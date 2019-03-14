package gosocketio

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/verticalops/golang-socketio/protocol"
	"github.com/verticalops/golang-socketio/transport"
)

var (
	//QueueBufferSize is the global buffer size for all internal channels and messages.
	//It should only be changed before using anything from this package and cannot be changed concurrently.
	QueueBufferSize = 50

	ErrorWrongHeader = errors.New("Wrong header")
)

/**
engine.io header to send or receive
*/
type Header struct {
	Sid          string   `json:"sid"`
	Upgrades     []string `json:"upgrades"`
	PingInterval int      `json:"pingInterval"`
	PingTimeout  int      `json:"pingTimeout"`
}

/**
socket.io connection handler

use IsAlive to check that handler is still working
use Dial to connect to websocket
use In and Out channels for message exchange
Close message means channel is closed
ping is automatic
*/
type Channel struct {
	conn transport.Connection

	out    chan string
	header Header

	alive     bool
	aliveLock sync.Mutex

	ack ackProcessor

	server        *Server
	ip            string
	requestHeader http.Header
}

/**
create channel, map, and set active
*/
func (c *Channel) initChannel() {
	c.out = make(chan string, QueueBufferSize)
	c.ack.resultWaiters = make(map[int](chan string))
	c.alive = true
}

/**
Get id of current socket connection
*/
func (c *Channel) Id() string {
	return c.header.Sid
}

/**
Checks that Channel is still alive
*/
func (c *Channel) IsAlive() (alive bool) {
	c.aliveLock.Lock()
	alive = c.alive
	c.aliveLock.Unlock()
	return
}

/**
Close channel
*/
func closeChannel(c *Channel, m *methods, args ...interface{}) error {
	c.aliveLock.Lock()
	if !c.alive {
		//already closed
		c.aliveLock.Unlock()
		return nil
	}
	c.conn.Close()
	c.alive = false
	c.aliveLock.Unlock()

	//clean outloop
	for len(c.out) > 0 {
		<-c.out
	}
	c.out <- protocol.CloseMessage

	m.callLoopEvent(c, OnDisconnection)

	return nil
}

//incoming messages loop, puts incoming messages to In channel
func inLoop(c *Channel, m *methods) error {
	for {
		pkg, err := c.conn.GetMessage()
		if err != nil {
			return closeChannel(c, m, err)
		}
		msg, err := protocol.Decode(pkg)
		if err != nil {
			closeChannel(c, m, protocol.ErrorWrongPacket)
			return err
		}

		switch msg.Type {
		case protocol.MessageTypeOpen:
			if err := json.Unmarshal([]byte(msg.Source[1:]), &c.header); err != nil {
				closeChannel(c, m, ErrorWrongHeader)
			}
			m.callLoopEvent(c, OnConnection)
		case protocol.MessageTypePing:
			c.out <- protocol.PongMessage
		case protocol.MessageTypePong:
		default:
			go m.processIncomingMessage(c, msg)
		}
	}
}

/**
outgoing messages loop, sends messages from channel to socket
*/
func outLoop(c *Channel, m *methods) error {
	for {
		if len(c.out) >= QueueBufferSize-1 {
			return closeChannel(c, m, ErrorSocketOverflood)
		}

		msg := <-c.out
		if msg == protocol.CloseMessage {
			return nil
		}

		err := c.conn.WriteMessage(msg)
		if err != nil {
			return closeChannel(c, m, err)
		}
	}
}

/**
Pinger sends ping messages for keeping connection alive
*/
func pinger(c *Channel) {
	for {
		interval, _ := c.conn.PingParams()
		time.Sleep(interval)
		if !c.IsAlive() {
			return
		}

		c.out <- protocol.PingMessage
	}
}
