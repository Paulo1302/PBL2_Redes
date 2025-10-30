package pubsub

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
)

type matchStruct struct {
    SelfId string `json:"self_id"`
    P1     int    `json:"p1"`
    P2     int    `json:"p2"`
    Card1  int    `json:"card1"`
    Card2  int    `json:"card2"`
}


type NatsMessage struct {
    ClientID int         `json:"client_id"`
    Err      any		 `json:"err"`
    Match    matchStruct `json:"match"`     
}

type TradeCard struct {
    ClientID int         `json:"client_id"`
    Err      any		 `json:"err"`
    Card	 int		 `json:"return_card"`     
}


func BrokerConnect(serverNumber int) *nats.Conn {
	url := "nats://10.200.54.149:" + strconv.Itoa(serverNumber+4222)
	//fmt.Println(url)
	nc, _ := nats.Connect(url)

	return nc
}

func RequestPing(nc *nats.Conn) int64 {
	msg := map[string]any{
		"send_time": time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.ping", data, time.Second)
	if err != nil {
		return -1
	}
	json.Unmarshal(response.Data, &msg)
	return int64(msg["server_ping"].(float64)) - int64(msg["send_time"].(float64))
}

func RequestCreateAccount(nc *nats.Conn) int {
	response, err := nc.Request("topic.createAccount", nil, time.Second)
	if err != nil {
		fmt.Println(err.Error())
		return 0 //se id == 0, não conseguiu criar usuário
	}
	msg := make(map[string]int)
	fmt.Println("Enviado:", msg)
	json.Unmarshal(response.Data, &msg)
	return msg["player_id"]
}

func RequestLogin(nc *nats.Conn, id int) (bool, error) {
	msg := map[string]any{
		"client_id": id,
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.login", data, time.Second)
	if err != nil {
		fmt.Println(err.Error())
		return false, err
	}
	fmt.Println("Enviado:", msg)
	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		return false, errors.New(msg["err"].(string))
	}

	return msg["result"].(bool), err
}

func RequestOpenPack(nc *nats.Conn, id int) ([]int, error) {
	msg := map[string]any{
		"client_id": id,
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.openPack", data, time.Second)

	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}

	fmt.Println("Enviado:", msg)
	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		return nil, errors.New(msg["err"].(string))
	}

	resultSlice := msg["result"].([]any)
	cards := make([]int, 0, len(resultSlice))

	for _, item := range resultSlice {
		cards = append(cards, int(item.(float64)))
	}

	return cards, nil
}

func RequestSeeCards(nc *nats.Conn, id int) ([]int, error) {
	msg := map[string]any{
		"client_id": id,
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.seeCards", data, time.Second)

	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}

	fmt.Println("Enviado:", msg)
	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		return []int{}, errors.New(msg["err"].(string))
	}

	if msg["result"] == nil {
		return []int{}, nil
	}
	resultSlice := msg["result"].([]any)

	cards := make([]int, 0, len(resultSlice))

	for _, item := range resultSlice {
		cards = append(cards, int(item.(float64)))
	}

	return cards, nil
}

func RequestFindMatch(nc *nats.Conn, id int) (string, error) {

	matchValue := ""
	match := &matchValue

	onQueue := make(chan (int))

	sub, _ := nc.Subscribe("topic.matchmaking", func(msg *nats.Msg) {
		var natsPayload NatsMessage
		json.Unmarshal(msg.Data, &natsPayload)
		if natsPayload.ClientID != id {
			fmt.Println("NOTT")
			return
		}
		if natsPayload.Err != nil {
			*match = ""
			onQueue <- -1
		}

		nc.Publish(msg.Reply, msg.Data)
		*match = natsPayload.Match.SelfId
		onQueue <- 0
	})

	msg := map[string]any{
		"client_id": id,
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.findMatch", data, time.Second)

	if err != nil {
		sub.Unsubscribe()
		fmt.Println(err.Error())
		return "", err
	}

	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		sub.Unsubscribe()
		return "", errors.New(msg["err"].(string))
	}

	if (<-onQueue) == -1 {
		sub.Unsubscribe()
		return "", errors.New("MATCHMAKING QUEUE ERROR")
	}
	sub.Unsubscribe()
	return *match, nil
}


func RequestTradeCards(nc *nats.Conn, id int, cardToSend int) (int, error) {

	cardValue := 0
	card := &cardValue

	onQueue := make(chan (int))

	sub, _ := nc.Subscribe("topic.listenTrade", func(msg *nats.Msg) {
		var natsPayload TradeCard
		json.Unmarshal(msg.Data, &natsPayload)
		if natsPayload.ClientID != id {
			fmt.Println("NOTT")
			return
		}
		if natsPayload.Err != nil {
			*card = 0
			onQueue <- -1
		}

		nc.Publish(msg.Reply, msg.Data)
		*card = natsPayload.Card
		onQueue <- 0
	})

	msg := map[string]any{
		"client_id": id,
		"card":cardToSend,
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.sendTrade", data, time.Second)

	if err != nil {
		sub.Unsubscribe()
		fmt.Println(err.Error())
		return 0, err
	}

	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		sub.Unsubscribe()
		return 0, errors.New(msg["err"].(string))
	}

	if (<-onQueue) == -1 {
		sub.Unsubscribe()
		return 0, errors.New("MATCHMAKING QUEUE ERROR")
	}
	sub.Unsubscribe()
	return *card, nil
}

func RequestTradeCards2(nc *nats.Conn, id int, card int) (int, error) {

	payload := make(map[string]any)

	var tradedCard *int

	onQueue := make(chan (int))

	sub, _ := nc.Subscribe("topic.listenTrade", func(msg *nats.Msg) {

		json.Unmarshal(msg.Data, &payload)
		if payload["client_ID"].(int) != id {
			return
		}
		if payload["err"] != nil {
			*tradedCard = 0
			onQueue <- -1
		}

		*tradedCard = payload["new_card"].(int)
		nc.Publish(msg.Reply, msg.Data)
		onQueue <- 0
	})

	msg := map[string]any{
		"client_ID": id,
		"card":      card,
	}
	data, _ := json.Marshal(msg)
	response, err := nc.Request("topic.sendTrade", data, time.Second)

	if err != nil {
		sub.Unsubscribe()
		fmt.Println(err.Error())
		return 0, err
	}

	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		sub.Unsubscribe()
		return 0, errors.New(msg["err"].(string))
	}

	if (<-onQueue) == -1 {
		sub.Unsubscribe()
		return 0, errors.New("TRADE QUEUE ERROR")
	}

	sub.Unsubscribe()
	return *tradedCard, nil
}

func SendCards(nc *nats.Conn, id int, card int, game string) {
	msg := map[string]any{
		"client_id": id,
		"card":      card,
		"game":      game,
	}
	data, _ := json.Marshal(msg)

	nc.Publish("game.client", data)
}

// func ManageGame(nc *nats.Conn, id int, card chan(int), ready chan(struct{}), roundResult chan(string)){
// 	gameResult := make(chan(string))
// 	ctx, cancel := context.WithCancel(context.Background())

// 	go imAlive(nc, int64(id), ctx)

// 	payload := make(map[string]any)

// 	sub,_:=nc.Subscribe("game.server", func(msg *nats.Msg) {

// 		json.Unmarshal(msg.Data, &payload)

// 		if payload["err"] != nil {
// 			card <- 0
// 			gameResult <- "error" //erro, sai da fila
// 			cancel()
// 			return
// 		}
// 		if int(payload["client_id"].(float64)) != id {
// 			return
// 		}
// 		if payload["result"].(string) == "win"{
// 			card <- int(payload["card"].(float64))
// 			gameResult <- "win" //vitoria
// 			cancel()
// 			return
// 		}
// 		if payload["result"].(string) == "lose"{
// 			card <- int(payload["card"].(float64))
// 			gameResult <- "lose" //derrota
// 			cancel()
// 		}
// 	})
// 	ready <- struct{}{}
// 	roundResult <- <- gameResult
// 	sub.Unsubscribe()
// }

func ManageGame2(nc *nats.Conn, id *int, card chan (int), roundResult chan (string)) {

	payload := make(map[string]any)

	nc.Subscribe("game.server", func(msg *nats.Msg) {
		json.Unmarshal(msg.Data, &payload)
		currId := *id
		if currId == 0 {
			return
		}
		if payload["err"] != nil {
			card <- 0
			roundResult <- "error" //erro, sai da fila
			return
		}
		if int(payload["client_id"].(float64)) != currId {
			return
		}
		if payload["result"].(string) == "win" {
			card <- int(payload["card"].(float64))
			roundResult <- "win" //vitoria
			return
		}
		if payload["result"].(string) == "lose" {
			card <- int(payload["card"].(float64))
			roundResult <- "lose" //derrota
		}
	})
}

func ImAlive(nc *nats.Conn, id int) *nats.Subscription {
	sub, _ := nc.Subscribe("game.heartbeat", func(m *nats.Msg) {
		var payload map[string]int
		json.Unmarshal(m.Data, &payload)
		if int(payload["client_id"]) != id {
			return
		}
		nc.Publish(m.Reply, m.Data)
	})
	return sub

}

func LoggedIn(nc *nats.Conn, id int) *nats.Subscription {
	sub, _ := nc.Subscribe("topic.loggedIn", func(m *nats.Msg) {
		var payload map[string]int
		json.Unmarshal(m.Data, &payload)
		if int(payload["client_id"]) != id {
			return
		}
		nc.Publish(m.Reply, m.Data)
	})
	return sub
}

func Heartbeat(nc *nats.Conn, value *int64) {
	ping := make(map[string]int64)
	nc.Subscribe("topic.heartbeat", func(msg *nats.Msg) {
		json.Unmarshal(msg.Data, &ping)
		*value = ping["server_ping"]
	})
}
