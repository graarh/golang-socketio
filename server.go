package gosocketio

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/verticalops/golang-socketio/protocol"
	"github.com/verticalops/golang-socketio/transport"
)

const (
	HeaderForward = "X-Forwarded-For"
)

var (
	ErrorServerNotSet       = errors.New("Server not set")
	ErrorConnectionNotFound = errors.New("Connection not found")

	ErrRateLimiting = errors.New("gosocketio: Rate limiting reached, a message was dropped")
)

/**
socket.io server instance
*/
type Server struct {
	methods
	http.Handler

	channels     map[string]map[*Channel]struct{}
	rooms        map[*Channel]map[string]struct{}
	channelsLock sync.RWMutex

	sids     map[string]*Channel
	sidsLock sync.RWMutex

	tr transport.Transport

	//an attempt to stop bad code from being bad
	rh RecoveryHandler
	eh ErrorHandler

	//for rate limiting
	limit int

	//for graceful shutdown
	done chan struct{}
	//counting the number of goroutines we've spawned
	count *int64
}

/**
Close current channel
*/
func (c *Channel) Close() {
	if c.server != nil {
		closeChannel(c, &c.server.methods)
	}
}

/**
Get ip of socket client
*/
func (c *Channel) Ip() string {
	forward := c.RequestHeader().Get(HeaderForward)
	if forward != "" {
		return forward
	}
	return c.ip
}

/**
Get request header of this connection
*/
func (c *Channel) RequestHeader() http.Header {
	return c.requestHeader
}

/**
Get channel by it's sid
*/
func (s *Server) GetChannel(sid string) (*Channel, error) {
	s.sidsLock.RLock()
	defer s.sidsLock.RUnlock()

	c, ok := s.sids[sid]
	if !ok {
		return nil, ErrorConnectionNotFound
	}

	return c, nil
}

/**
Join this channel to given room
*/
func (c *Channel) Join(room string) error {
	if c.server == nil {
		return ErrorServerNotSet
	}

	c.server.channelsLock.Lock()
	defer c.server.channelsLock.Unlock()

	cn := c.server.channels
	if _, ok := cn[room]; !ok {
		cn[room] = make(map[*Channel]struct{})
	}

	byRoom := c.server.rooms
	if _, ok := byRoom[c]; !ok {
		byRoom[c] = make(map[string]struct{})
	}

	cn[room][c] = struct{}{}
	byRoom[c][room] = struct{}{}

	return nil
}

/**
Remove this channel from given room
*/
func (c *Channel) Leave(room string) error {
	if c.server == nil {
		return ErrorServerNotSet
	}

	c.server.channelsLock.Lock()
	defer c.server.channelsLock.Unlock()

	cn := c.server.channels
	if _, ok := cn[room]; ok {
		delete(cn[room], c)
		if len(cn[room]) == 0 {
			delete(cn, room)
		}
	}

	byRoom := c.server.rooms
	if _, ok := byRoom[c]; ok {
		delete(byRoom[c], room)
	}

	return nil
}

/**
Get amount of channels, joined to given room, using channel
*/
func (c *Channel) Amount(room string) int {
	if c.server == nil {
		return 0
	}

	return c.server.Amount(room)
}

/**
Get amount of channels, joined to given room, using server
*/
func (s *Server) Amount(room string) int {
	s.channelsLock.RLock()
	defer s.channelsLock.RUnlock()

	roomChannels, _ := s.channels[room]
	return len(roomChannels)
}

/**
Get list of channels, joined to given room, using channel
*/
func (c *Channel) List(room string) []*Channel {
	if c.server == nil {
		return []*Channel{}
	}

	return c.server.List(room)
}

/**
Get list of channels, joined to given room, using server
*/
func (s *Server) List(room string) []*Channel {
	s.channelsLock.RLock()
	defer s.channelsLock.RUnlock()

	roomChannels, ok := s.channels[room]
	if !ok {
		return []*Channel{}
	}

	i := 0
	roomChannelsCopy := make([]*Channel, len(roomChannels))
	for channel := range roomChannels {
		roomChannelsCopy[i] = channel
		i++
	}

	return roomChannelsCopy

}

func (c *Channel) BroadcastTo(room, method string, args interface{}) {
	if c.server == nil {
		return
	}
	c.server.BroadcastTo(room, method, args)
}

/**
Broadcast message to all room channels
*/
func (s *Server) BroadcastTo(room, method string, args interface{}) {
	s.channelsLock.RLock()
	defer s.channelsLock.RUnlock()

	roomChannels, ok := s.channels[room]
	if !ok {
		return
	}

	for cn := range roomChannels {
		if cn.IsAlive() {
			go cn.Emit(method, args)
		}
	}
}

/**
Broadcast to all clients
*/
func (s *Server) BroadcastToAll(method string, args interface{}) {
	s.sidsLock.RLock()
	defer s.sidsLock.RUnlock()

	for _, cn := range s.sids {
		if cn.IsAlive() {
			go cn.Emit(method, args)
		}
	}
}

/**
Generate new id for socket.io connection
*/
func generateNewId(custom string) string {
	hash := fmt.Sprintf("%s %s %d %d", custom, time.Now(), rand.Uint32(), rand.Uint32())
	sum := md5.Sum([]byte(hash))
	return base64.URLEncoding.EncodeToString(sum[:])[:20]
}

/**
On connection system handler, store sid
*/
func onConnectStore(c *Channel) {
	c.server.sidsLock.Lock()
	c.server.sids[c.Id()] = c
	c.server.sidsLock.Unlock()
}

/**
On disconnection system handler, clean joins and sid
*/
func onDisconnectCleanup(c *Channel) {
	c.server.channelsLock.Lock()

	cn := c.server.channels
	byRoom, ok := c.server.rooms[c]
	if ok {
		for room := range byRoom {
			if curRoom, ok := cn[room]; ok {
				delete(curRoom, c)
				if len(curRoom) == 0 {
					delete(cn, room)
				}
			}
		}

		delete(c.server.rooms, c)
	}

	c.server.channelsLock.Unlock()

	c.server.sidsLock.Lock()
	delete(c.server.sids, c.Id())
	c.server.sidsLock.Unlock()
}

func (s *Server) SendOpenSequence(c *Channel) error {
	jsonHdr, err := json.Marshal(&c.header)
	if err != nil {
		return err
	}

	msg, err := protocol.Encode(&protocol.Message{
		Type: protocol.MessageTypeOpen,
		Args: string(jsonHdr),
	})
	if err != nil {
		return err
	}

	c.sendOut(msg)

	msg, err = protocol.Encode(&protocol.Message{Type: protocol.MessageTypeEmpty})
	if err != nil {
		return err
	}

	c.sendOut(msg)
	return nil
}

/**
Setup event loop for given connection
*/
func (s *Server) SetupEventLoop(conn transport.Connection, remoteAddr string,
	requestHeader http.Header) {

	interval, timeout := conn.PingParams()
	hdr := Header{
		Sid:          generateNewId(remoteAddr),
		Upgrades:     []string{},
		PingInterval: int(interval / time.Millisecond),
		PingTimeout:  int(timeout / time.Millisecond),
	}

	c := &Channel{}
	c.conn = conn
	c.ip = remoteAddr
	c.requestHeader = requestHeader
	c.initChannel()

	c.server = s
	c.header = hdr
	c.done = s.done
	c.rh = s.rh
	c.eh = s.eh
	c.rl = newRateLimiter(c, s.limit)

	c.startLoop(&s.methods, c.outLoop)

	if err := s.SendOpenSequence(c); err != nil {
		s.eh.call(err)
		closeChannel(c, &s.methods)
		return
	}

	c.startLoop(&s.methods, c.inLoop)

	s.callLoopEvent(c, OnConnection)
}

/**
implements ServeHTTP function from http.Handler
*/
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.tr.HandleConnection(w, r)
	if err != nil {
		s.eh.call(err)
		return
	}

	s.SetupEventLoop(conn, r.RemoteAddr, r.Header)
	s.tr.Serve(w, r)
}

/**
Get amount of current connected sids
*/
func (s *Server) AmountOfSids() int64 {
	s.sidsLock.RLock()
	defer s.sidsLock.RUnlock()

	return int64(len(s.sids))
}

/**
Get amount of rooms with at least one channel(or sid) joined
*/
func (s *Server) AmountOfRooms() int64 {
	s.channelsLock.RLock()
	defer s.channelsLock.RUnlock()

	return int64(len(s.channels))
}

/**
Create new socket.io server
*/
func NewServer(tr transport.Transport, opts ...ServerOption) *Server {
	s := Server{}
	s.initMethods()
	s.tr = tr
	s.channels = make(map[string]map[*Channel]struct{})
	s.rooms = make(map[*Channel]map[string]struct{})
	s.sids = make(map[string]*Channel)
	s.onConnection = onConnectStore
	s.onDisconnection = onDisconnectCleanup
	s.done = make(chan struct{})
	s.count = new(int64)

	for _, opt := range opts {
		opt(&s)
	}

	return &s
}

//Functions for atomic counter
func (s *Server) inc() { atomic.AddInt64(s.count, 1) }

func (s *Server) dec() { atomic.AddInt64(s.count, -1) }

//NumGoroutine returns the number of goroutines spawned by the server or its Channels.
func (s *Server) NumGoroutine() int64 { return atomic.LoadInt64(s.count) }

/*
Shutdown will shutdown the server gracefully by telling all interally spawned goroutines to exit.
It does not force goroutines spawned by callbacks to exit. Shutdown is not safe to call concurrently.
Shutdown may be called more than once from the same goroutine as it will only error if the passed context
is finished before shutdown is complete.
*/
func (s *Server) Shutdown(ctx context.Context) error {
	//This wont stop concurrent calls to shutdown from racing on channel close
	//but it's good enough for repeated calls from the same goroutine.
	select {
	case <-s.done:
	default:
		close(s.done)
	}

	done := ctx.Done()

	//Unfortunate but simple to add here without a greater rewrite, poll to check if goroutines are done.
	//Similar to http.Server.Shutdown.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return fmt.Errorf("gosocketio.Server.Shutdown: Server left with running goroutines: %d (%v)", s.NumGoroutine(), ctx.Err())
		case <-ticker.C:
			//Return error if we end up below zero?
			if s.NumGoroutine() <= 0 {
				return nil
			}
		}
	}
}
