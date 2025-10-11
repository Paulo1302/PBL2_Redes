package pubsub

import (
	"fmt"
	"strconv"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
)


func BrokerConnect(serverNumber int) *nats.Conn {
	url := "nats://127.0.0.1:" + strconv.Itoa(serverNumber + 4222)
	fmt.Println(url)
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