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

	//is closed when no longer alive, useful when
	//blocking in select and checking a bool isn't viable
	aliveC chan struct{}

	alive     bool
	aliveLock sync.Mutex

	ack ackProcessor

	server        *Server
	ip            string
	requestHeader http.Header

	//an attempt to stop bad code from being bad
	rh RecoveryHandler
	eh ErrorHandler

	//for rate limiting
	rl rateLimiter

	//closed when Server is shutting down
	done <-chan struct{}
}

/**
create channel, map, and set active
*/
func (c *Channel) initChannel() {
	c.out = make(chan string, QueueBufferSize)
	c.ack.resultWaiters = make(map[int](chan string))
	c.alive = true
	c.aliveC = make(chan struct{})
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

//safely sends a msg into 'out' chan so we don't block forever
//if we're trying to exit
func (c *Channel) sendOut(msg string) {
	select {
	case c.out <- msg:
	case <-c.done:
	case <-c.aliveC:
	}
}

/**
Close channel
*/
func closeChannel(c *Channel, m *methods) {
	c.aliveLock.Lock()
	if !c.alive {
		//already closed
		c.aliveLock.Unlock()
		return
	}
	c.conn.Close()
	c.alive = false
	close(c.aliveC)
	c.aliveLock.Unlock()

	m.callLoopEvent(c, OnDisconnection)
}

//start server side channel loops, not client side ones
func (c *Channel) startLoop(m *methods, loop func(*methods) error) {
	c.server.inc()

	go func() {
		defer c.server.dec()

		if err := loop(m); err != nil {
			c.eh.call(err)
		}
	}()
}

//incoming messages loop, puts incoming messages to In channel
func (c *Channel) inLoop(m *methods) error {
	defer c.rh.call(c)

	for {
		pkg, err := c.conn.GetMessage()
		if err != nil {
			select {
			case <-c.done:
			case <-c.aliveC:
			default:
				closeChannel(c, m)
				return err
			}
			return nil
		}
		msg, err := protocol.Decode(pkg)
		if err != nil {
			closeChannel(c, m)
			return err
		}

		switch msg.Type {
		case protocol.MessageTypeOpen:
			if err := json.Unmarshal([]byte(msg.Source[1:]), &c.header); err != nil {
				closeChannel(c, m)
				return err
			}
			m.callLoopEvent(c, OnConnection)
		case protocol.MessageTypePing:
			c.sendOut(protocol.PongMessage)
		case protocol.MessageTypePong:
		default:
			c.rl(func() { m.processIncomingMessage(c, msg) })
		}
	}
}

/**
outgoing messages loop, sends messages from channel to socket
*/
func (c *Channel) outLoop(m *methods) error {
	defer c.rh.call(c)

	for {
		if len(c.out) >= QueueBufferSize-1 {
			closeChannel(c, m)
			return ErrorSocketOverflood
		}

		select {
		case <-c.done:
			closeChannel(c, m)
			return nil
		case <-c.aliveC:
			return nil
		case msg := <-c.out:

			if err := c.conn.WriteMessage(msg); err != nil {
				closeChannel(c, m)
				return err
			}
		}
	}
}

/**
Pinger sends ping messages for keeping connection alive
*/
func pinger(c *Channel) {
	interval, _ := c.conn.PingParams()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-c.aliveC:
			return
		case <-ticker.C:
			if !c.IsAlive() {
				return
			}

			c.sendOut(protocol.PingMessage)
		}
	}
}
