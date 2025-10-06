package main

import (
	"time"

	"client/API"
)

func pubSub(serverNumber int) {
	nc := pubsub.BrokerConnect(serverNumber)
	defer nc.Close()

	for range 10 {
		pubsub.RequestPing(nc)
		time.Sleep(2 * time.Second)
	}
}

func main() {
	
	pubSub(0)
	
}