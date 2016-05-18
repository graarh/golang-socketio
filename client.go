package gosocketio

import (
	"github.com/graarh/golang-socketio/transport"
)

const (
	webSocketProtocol = "ws://"
	socketioUrl       = "/socket.io/?EIO=3&transport=websocket"
)

/**
Socket.io client representation
*/
type Client struct {
	methods
	Channel
}

/**
connect to host and initialise socket.io protocol
*/
func Dial(host string, tr transport.Transport) (*Client, error) {
	c := &Client{}
	c.initChannel()
	c.initMethods()

	var err error
	c.conn, err = tr.Connect(host)
	if err != nil {
		return nil, err
	}

	go inLoop(&c.Channel, &c.methods)
	go outLoop(&c.Channel, &c.methods)
	go pinger(&c.Channel)

	return c, nil
}

/**
Close client connection
*/
func (c *Client) Close() {
	CloseChannel(&c.Channel, &c.methods)
}
