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

func ReplyPing(nc *nats.Conn) {
	var payload map[string]int64
	nc.Subscribe("topic.ping", func (m *nats.Msg) {
		json.Unmarshal(m.Data, &payload)
		payload["response_time"] = time.Now().UnixMilli()
		data,_:=json.Marshal(payload)
		nc.Publish(m.Reply, data)
		fmt.Println("atraso:", time.Now().UnixMilli()-payload["send_time"], "ms")
	})	
}