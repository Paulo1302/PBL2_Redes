package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
)


func BrokerConnect(serverNumber int) *nats.Conn {
	url := "nats://127.0.0.1:" + strconv.Itoa(serverNumber + 4222)
	//fmt.Println(url)
	nc,_ := nats.Connect(url)
	
	return nc
}

func RequestPing(nc *nats.Conn) {
	msg := map[string]int64{
			"send_time": time.Now().UnixMilli(),
		}
	data,_ := json.Marshal(msg)
	response,err := nc.Request("topic.ping", data, time.Second)
	if err != nil{
		fmt.Println(err.Error())
		return
	}
	fmt.Println("Enviado:", msg)
	json.Unmarshal(response.Data, &msg)
	fmt.Println("Resposta:",msg["response_time"] - msg["send_time"])
}

func RequestCreateAccount(nc *nats.Conn) int {
	response,err := nc.Request("topic.createAccount", nil, time.Second)
	if err != nil{
		fmt.Println(err.Error())
		return 0
	}
	msg := make(map[string]int)
	fmt.Println("Enviado:", msg)
	json.Unmarshal(response.Data, &msg)
	return msg["client_ID"]
}


func RequestLogin(nc *nats.Conn, id int) (int,error) {
	msg := map[string]any{
			"client_ID": id,
		}
	data,_ := json.Marshal(msg)
	response,err := nc.Request("topic.login", data, time.Second)
	if err != nil{
		fmt.Println(err.Error())
		return 0, err
	}
	fmt.Println("Enviado:", msg)
	json.Unmarshal(response.Data, &msg)
	
	if msg["err"] != nil {
		err = errors.New(msg["err"].(string))
	}else {
		err = nil
	}
	
	return msg["result"].(int), err
}


func RequestOpenPack(nc *nats.Conn, id int) ([]int,error) {
	msg := map[string]any{
			"client_ID": id,
		}
	data,_ := json.Marshal(msg)
	response,err := nc.Request("topic.openPack", data, time.Second)

	if err != nil{
		fmt.Println(err.Error())
		return nil, err
	}

	
	fmt.Println("Enviado:", msg)
	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		err = errors.New(msg["err"].(string))
	}else {
		err = nil
	}

	return msg["result"].([]int), err
}


func RequestSeeCards(nc *nats.Conn, id int) ([]int,error) {
	msg := map[string]any{
			"client_ID": id,
		}
	data,_ := json.Marshal(msg)
	response,err := nc.Request("topic.seeCards", data, time.Second)

	if err != nil{
		fmt.Println(err.Error())
		return nil, err
	}

	
	fmt.Println("Enviado:", msg)
	json.Unmarshal(response.Data, &msg)
	
	if msg["err"] != nil {
		err = errors.New(msg["err"].(string))
	}else {
		err = nil
	}

	return msg["result"].([]int), err
}


func RequestFindMatch(nc *nats.Conn, id int) (int,error) {

	payload := make(map[string]any)

	var enemyId *int
	
	onQueue := make(chan(int))

	nc.Subscribe("topic.matchmaking", func(msg *nats.Msg) {

		json.Unmarshal(msg.Data, &payload)
		if payload["client_ID"].(int) != id{
			return
		}
		if payload["err"] != nil{
			*enemyId = 0
			onQueue <- -1
			select {}
		}
		
		*enemyId = payload["enemy_ID"].(int)
		nc.Publish(msg.Reply, msg.Data)
		onQueue <- 0
		select {}
	})

	msg := map[string]any{
			"client_ID": id,
		}
	data,_ := json.Marshal(msg)
	response,err := nc.Request("topic.findMatch", data, time.Second)

	if err != nil{
		fmt.Println(err.Error())
		return 0, err
	}

	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		return 0, errors.New(msg["err"].(string))
	}

	if ((<- onQueue) == -1){
		return 0, errors.New("MATCHMAKING QUEUE ERROR")
	}

	return *enemyId, nil
}


func RequestTradeCards(nc *nats.Conn, id int, card int) (int,error) {

	payload := make(map[string]any)

	var tradedCard *int
	
	onQueue := make(chan(int))

	nc.Subscribe("topic.listenTrade", func(msg *nats.Msg) {

		json.Unmarshal(msg.Data, &payload)
		if payload["client_ID"].(int) != id{
			return
		}
		if payload["err"] != nil{
			*tradedCard = 0
			onQueue <- -1
			select {}
		}
		
		*tradedCard = payload["new_card"].(int)
		nc.Publish(msg.Reply, msg.Data)
		onQueue <- 0
		select {}
	})

	msg := map[string]any{
			"client_ID": id,
			"card": card,
		}
	data,_ := json.Marshal(msg)
	response,err := nc.Request("topic.sendTrade", data, time.Second)

	if err != nil{
		fmt.Println(err.Error())
		return 0, err
	}

	json.Unmarshal(response.Data, &msg)

	if msg["err"] != nil {
		return 0, errors.New(msg["err"].(string))
	}

	if ((<- onQueue) == -1){
		return 0, errors.New("TRADE QUEUE ERROR")
	}

	return *tradedCard, nil
}

func sendCards(nc *nats.Conn, id int, card int){
	msg := map[string]any{
			"client_ID": id,
			"card": card,
		}
	data, _ := json.Marshal(msg)

	nc.Publish("game.client", data)
}

func ManageGame(nc *nats.Conn, id int, card chan(int)) string{
	gameResult := make(chan(string))
	ctx, cancel := context.WithCancel(context.Background())
	
	go imAlive(nc, int64(id), ctx)

	payload := make(map[string]any)

	nc.Subscribe("game.server", func(msg *nats.Msg) {
		
		json.Unmarshal(msg.Data, &payload)
		
		if payload["err"] != nil {
			card <- 0
			gameResult <- "error" //erro, sai da fila
			cancel()
			return
		}
		if payload["client_ID"].(int) != id {
			return
		}
		if payload["result"].(string) == "win"{
			card <- payload["card"].(int)
			gameResult <- "win" //vitoria
			cancel()
			return
		}
		if payload["result"].(string) == "lose"{
			card <- payload["card"].(int)
			gameResult <- "lose" //derrota
			cancel()
		}
	})

	return <- gameResult

}

func imAlive(nc *nats.Conn, id int64, ctx context.Context){
	
	for{
		select {
		case <-ctx.Done():
			return
		default:
			msg := map[string]int64{
				"client_id" : id,
				"client_ping": time.Now().UnixMilli(),
			}
			data,_ := json.Marshal(msg)
			nc.Publish("game.heartbeat", data)	
		}
	}

}


func Heartbeat(nc *nats.Conn, value *int64) {
	ping := make(map[string]int64)
	nc.Subscribe("topic.heartbeat", func(msg *nats.Msg) {
		json.Unmarshal(msg.Data, &ping)
		*value = ping["server_ping"]
	})
}