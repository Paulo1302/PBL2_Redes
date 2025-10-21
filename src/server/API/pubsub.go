package API

import (
    "encoding/json"
    "fmt"
    "log"
    "strconv"
    "time"

    "github.com/nats-io/nats.go"
)


func SetupPS(){
	nc,_ := BrokerConnect(0)
	ReplyPing(nc)
	
	go func() {
		for {
			htb := map[string]int64{"server_ping":time.Now().UnixMilli()}
			PublishMessage(nc, "topic.heartbeat", htb)
		}
	}()
	
}

// BrokerConnect conecta ao broker NATS com tratamento de erro robusto
// O parâmetro serverNumber se torna redundante, mas mantemos por compatibilidade
func BrokerConnect(serverNumber int) (*nats.Conn, error) {    
	url := "nats://host.docker.internal:" + strconv.Itoa(serverNumber+4222)

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
