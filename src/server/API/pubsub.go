package API

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/hashicorp/raft"
	"github.com/nats-io/nats.go"
)

var myConnection *nats.Conn

func SetupPS(s *Store) {

	SetupGameState()

	nc, _ := BrokerConnect(0)
	ReplyPing(nc)

	myConnection = nc
	
	go func() {
		for {
			htb := map[string]int64{"server_ping": time.Now().UnixMilli()}
			PublishMessage(nc, "topic.heartbeat", htb)
		}
	}()

	CreateAccount(nc, s)
	ClientLogin(nc, s)
}

// BrokerConnect conecta ao broker NATS com tratamento de erro robusto
// O parâmetro serverNumber se torna redundante, mas mantemos por compatibilidade
func BrokerConnect(serverNumber int) (*nats.Conn, error) {
	url := "nats://192.168.0.21:" + strconv.Itoa(serverNumber+4222)

	// Configuração com timeout e reconexão para robustez
	opts := []nats.Option{
		nats.Name("CardGame-Server"),
		nats.Timeout(10 * time.Second),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(5),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			log.Printf("NATS disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("NATS reconnected to %v", nc.ConnectedUrl())
		}),
	}

	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %v", err)
	}

	fmt.Printf("Successfully connected to NATS at %s", url)
	return nc, nil
}

func PublishMessage(nc *nats.Conn, subject string, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %v", err)
	}

	if err := nc.Publish(subject, jsonData); err != nil {
		return fmt.Errorf("failed to publish message: %v", err)
	}

	return nil
}

func RequestMessage(nc *nats.Conn, subject string, data any, timeout time.Duration) (*nats.Msg, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	response, err := nc.Request(subject, jsonData, timeout)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}

	return response, nil
}

func ReplyPing(nc *nats.Conn) error {
	_, err := nc.Subscribe("topic.ping", func(m *nats.Msg) {
		var payload map[string]any

		if err := json.Unmarshal(m.Data, &payload); err != nil {
			log.Printf("Error unmarshaling ping payload: %v", err)
			return
		}

		payload["server_ping"] = time.Now().UnixMilli()

		data, err := json.Marshal(payload)
		if err != nil {
			log.Printf("Error marshaling ping response: %v", err)
			return
		}

		// Tratamento de erro no publish
		if err := nc.Publish(m.Reply, data); err != nil {
			log.Printf("Error publishing ping response: %v", err)
			return
		}

		latency := payload["server_ping"].(int64) - int64(payload["send_time"].(float64))
		fmt.Printf("Ping processed - latency: %d ms\n", latency)
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to ping topic: %v", err)
	}

	log.Printf("NATS ping reply handler registered successfully")
	return nil
}

func CreateAccount(nc *nats.Conn, s *Store) {
	nc.Subscribe("topic.createAccount", func(m *nats.Msg) {
		fmt.Println("REQUEST CREATE ACCOUNT")
		if s.RaftLog.State() == raft.Leader {
			fmt.Println("IM LEADER")
			playerID, err := s.CreatePlayer()
			fmt.Println("CREATED PLAYER")
			if err != nil {
				nc.Publish(m.Reply, []byte(`{"error":"RAFT_APPLY_ERROR"}`))
				fmt.Println("SHIT")
				return
			}

			payload := map[string]any{
				"status":    "player created",
				"player_id": playerID,
				"node":      s.NodeID,
				"is_leader": true,
			}
			fmt.Println("YEAH")
			data, _ := json.Marshal(payload)
			nc.Publish(m.Reply, data)
			return
		}
		// se for follower, redireciona pro líder
		req := StandardRequest{
			RequestID:     fmt.Sprintf("%d", time.Now().UnixNano()),
			OperationType: "create_player",
			Payload:       nil,
		}
		resp := s.forwardToLeaderViaREST(req)
		data, _ := json.Marshal(resp.Payload)
		nc.Publish(m.Reply, data)
	})
}


func isLogged(id int) bool {
	payload := map[string]int{
		"client_id": id,
	}
	_, err := RequestMessage(myConnection, "topic.loggedIn", payload, 200 * time.Millisecond)
	
	if err != nil {
		return false
	}else {
		return true
	}
}


func ClientLogin(nc *nats.Conn, s *Store) {
	nc.Subscribe("topic.login", func(msg *nats.Msg) {
		fmt.Println(s.count)
		var payload map[string]any
		json.Unmarshal(msg.Data, &payload)
		
		if int(payload["client_id"].(float64)) > s.count {
			payload["err"] = "user not found"
			data,_ := json.Marshal(payload)
			nc.Publish(msg.Reply, data)
			return
		}

		// Só o líder coordena o login
		if s.RaftLog.State() != raft.Leader {
			leaderAddr := string(s.RaftLog.Leader())
			if leaderAddr == "" {
				nc.Publish(msg.Reply, []byte(`{"err":"NO_LEADER"}`))
				return
			}
			req := StandardRequest{
				RequestID:     fmt.Sprintf("%d", time.Now().UnixNano()),
				OperationType: "login",
				Payload:       msg.Data,
			}
			x := s.forwardToLeaderViaREST(req)
			json.Unmarshal(x.Payload, &payload)
			rsp := map[string]any{
				"err" : nil,
				"result" : payload["logged"],
			}
			data,_:=json.Marshal(rsp)
			nc.Publish(msg.Reply, data)
			return
		}
		// LÍDER: consulta todos os nós (inclusive ele mesmo)
		fmt.Println("SOU LIDER")
		clientID := int(payload["client_id"].(float64))
		logged := s.checkIfAnyNodeLogged(clientID)

		resp := map[string]any{
			"node":       s.NodeID,
			"is_leader":  true,
			"client_id":  clientID,
			"result": logged,
		}
		data, _ := json.Marshal(resp)
		nc.Publish(msg.Reply, data)
	})
}

func ClientOpenPack(nc *nats.Conn, s *Store) {
	nc.Subscribe("topic.openPack", func(m *nats.Msg) {
		fmt.Println("REQUEST OPENPACK")
		if s.RaftLog.State() == raft.Leader {
			fmt.Println("IM LEADER")
			playerID, err := s.CreatePlayer()
			fmt.Println("CREATED PLAYER")
			if err != nil {
				nc.Publish(m.Reply, []byte(`{"error":"RAFT_APPLY_ERROR"}`))
				fmt.Println("SHIT")
				return
			}

			payload := map[string]any{
				"status":    "player created",
				"player_id": playerID,
				"node":      s.NodeID,
				"is_leader": true,
			}
			fmt.Println("YEAH")
			data, _ := json.Marshal(payload)
			nc.Publish(m.Reply, data)
			return
		}

		// se for follower, redireciona pro líder
		req := StandardRequest{
			RequestID:     fmt.Sprintf("%d", time.Now().UnixNano()),
			OperationType: "create_player",
			Payload:       nil,
		}
		resp := s.forwardToLeaderViaREST(req)
		data, _ := json.Marshal(resp.Payload)
		nc.Publish(m.Reply, data)
	})
}


// func getSmth() map[string]any {
// 	resp, _ := http.Get("http://localhost:8080/status")
// 	bod,_:=io.ReadAll(resp.Body)
// 	var mymap map[string]any
// 	json.Unmarshal(bod, &mymap)

// 	return mymap
// }
