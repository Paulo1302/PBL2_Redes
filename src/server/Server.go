package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
)


func pubSub(serverNumber int){
	url := "nats://127.0.0.1:" + strconv.Itoa(serverNumber + 4222)
	fmt.Println(url)
	nc,_ := nats.Connect(nats.DefaultURL)
	defer nc.Close()
	var payload map[string]int64
	
	nc.Subscribe("topic.ping", func(m *nats.Msg) {
		json.Unmarshal(m.Data, &payload)
		fmt.Println("atraso:", time.Now().UnixMilli()-payload["send_time"], "ms")
	})

	select {}
}


func main(){

	// Create a Gin router with default middleware (logger and recovery)
	r := gin.Default()

	// Define a simple GET endpoint
	r.GET("/hello", func(c *gin.Context) {
		// Return JSON response
		c.JSON(http.StatusOK, gin.H{
			"message": "hello world",
		})
	})


	// Start server on port 8080 (default)
	// Server will listen on 0.0.0.0:8080 (localhost:8080 on Windows)
	go r.Run()

	go pubSub(0)
	select {}
}