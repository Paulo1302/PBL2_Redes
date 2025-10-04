package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
)

func pubSub(serverNumber int) {
	url := "nats://127.0.0.1:" + strconv.Itoa(serverNumber + 4222)
	fmt.Println(url)
	nc,_ := nats.Connect(nats.DefaultURL)
	defer nc.Close()


	for range 10 {
		msg := map[string]int64{
			"send_time": time.Now().UnixMilli(),
		}
		data,_ := json.Marshal(msg);
		nc.Publish("topic.ping", data)
		fmt.Println("Enviado:", msg)
		time.Sleep(2 * time.Second)
	}
}

func main() {
	
	pubSub(0)
	
}