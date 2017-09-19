package gosocketio

import (
	"fmt"
	"github.com/n0needt0/golang-socketio/transport"
	"net/url"
	"strconv"
	"strings"
)

const (
	webSocketProtocol       = "ws://"
	webSocketSecureProtocol = "wss://"
	socketioUrl             = "/socket.io/?EIO=3&transport=websocket"
)

/**
Socket.io client representation
*/
type Client struct {
	methods
	Channel
}

/**
Get ws/wss url by host and port
*/
func GetUrl(host string, port int, params []string, secure bool) string {
	var prefix string
	if secure {
		prefix = webSocketSecureProtocol
	} else {
		prefix = webSocketProtocol
	}

	_url, err := url.Parse(prefix + host + ":" + strconv.Itoa(port) + socketioUrl)
	if err != nil {
		fmt.Println("We unable to parse given url: ", _url)
	}

	if len(params) > 0 {
		_uval := _url.Query()
		for _, element := range params {
			s := strings.Split(element, "=")
			_uval.Add(s[0], s[1])
		}
		_url.RawQuery = _uval.Encode()
	}

	return _url.String()
}

/**
connect to host and initialise socket.io protocol

The correct ws protocol url example:
ws://myserver.com/socket.io/?EIO=3&transport=websocket

You can use GetUrlByHost for generating correct url
*/
func Dial(url string, tr transport.Transport) (*Client, error) {
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

	return c, nil
}

/**
Close client connection
*/
func (c *Client) Close() {
	closeChannel(&c.Channel, &c.methods)
}
