package gosocketio

import (
	"github.com/graarh/golang-socketio/transport"
	"strconv"
)

const (
	webSocketProtocol = "ws://"
	webSocketSecureProtocol = "wss://"
	socketioUrl       = "/socket.io/?EIO=3&transport=websocket"
)

/**
Socket.io client representation
*/
type Client struct {
	methods
	Channel
	url string
}

/**
Get ws/wss url by host and port
 */
func GetUrl(host string, port int, secure bool) string {
	var prefix string
	if secure {
		prefix = webSocketSecureProtocol
	} else {
		prefix = webSocketProtocol
	}
	return prefix + host + ":" + strconv.Itoa(port) + socketioUrl
}

/**
connect to host and initialise socket.io protocol

The correct ws protocol url example:
ws://myserver.com/socket.io/?EIO=3&transport=websocket

You can use GetUrlByHost for generating correct url
*/
func Dial(url string, tr transport.Transport,reconnect bool) (*Client, error) {
	c := &Client{}
	c.initChannel()
	c.initMethods()

	var err error
	c.conn, err = tr.Connect(url)
	if err != nil {
		return nil, err
	}

	go inLoop(&c.Channel, &c.methods)
	go outLoop(&c.Channel, &c.methods)
	go pinger(&c.Channel)
	if reconnect {
		c.On(OnDisconnection, func(channel *Channel, msg interface{}) {
			Redial(c)
		})
	}
	return c, nil
}

func Redial(c *Client) {
	var err error
	tr := transport.GetDefaultWebsocketTransport()
	c.initChannel()
	for {
		c.conn, err = tr.Connect(c.url)
		if err == nil {
			break
		} else {
			time.Sleep(time.Second)
		}
	}
	go inLoop(&c.Channel, &c.methods)
	go outLoop(&c.Channel, &c.methods)
	go pinger(&c.Channel)
	
}

/**
Close client connection
*/
func (c *Client) Close() {
	closeChannel(&c.Channel, &c.methods)
}
