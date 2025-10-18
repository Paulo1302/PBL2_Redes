package main

import (
	"time"

	"client/API"

)

//PLACEHOLDER


func pubSub(serverNumber int, htb *int64) {
	nc := pubsub.BrokerConnect(serverNumber)
	defer nc.Close()

	go pubsub.Heartbeat(nc, htb)
	
	select {}
}

func main() {

	var value = time.Now().UnixMilli()
	pubSub(0, &value)

	for time.Now().UnixMilli() - value < 1000{};
	
}