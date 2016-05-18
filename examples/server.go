package main

import (
	"funstream/libs/socket.io"
	"funstream/libs/socket.io/transport"
	"log"
	"net/http"
	"time"
)

type Channel struct {
	Channel string `json:"channel"`
}

type Message struct {
	Id      int    `json:"id"`
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

func main() {
	server := gosocketio.NewServer(transport.GetDefaultWebsocketTransport())

	server.On(gosocketio.OnConnection, func(c *gosocketio.Channel, args interface{}) {
		log.Println("Connected")

		c.Emit("/message", Message{10, "main", "using emit"})

		c.Join("test")
		c.BroadcastTo("test", "/message", Message{10, "main", "using broadcast"})
	})
	server.On(gosocketio.OnDisconnection, func(c *gosocketio.Channel, args interface{}) {
		log.Println("Disconnected")
	})

	server.On("/join", func(c *gosocketio.Channel, channel Channel) string {
		time.Sleep(2 * time.Second)
		return "joined to " + channel.Channel
	})

	serveMux := http.NewServeMux()
	serveMux.Handle("/socket.io/", server)

	log.Println("Starting server...")
	log.Panic(http.ListenAndServe(":3811", serveMux))
}
