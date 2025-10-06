package main

import (
	"net/http"
	
	"github.com/gin-gonic/gin"
	"server/API"
)


func clientCommunication(serverNumber int){
	nc := pubsub.BrokerConnect(serverNumber)
	defer nc.Close()
	
	pubsub.ReplyPing(nc)

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

	go clientCommunication(0)
	select {}
}