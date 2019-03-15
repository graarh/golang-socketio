package transport

import (
	"errors"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	upgradeFailed = "Upgrade failed: "

	WsDefaultPingInterval     = 30 * time.Second
	WsDefaultPingTimeout      = 60 * time.Second
	WsDefaultReceiveTimeout   = 60 * time.Second
	WsDefaultSendTimeout      = 60 * time.Second
	WsDefaultHandshakeTimeout = 4 * time.Second

	//WsDefaultBufferSize of 0 means the underlying websocket connection will reuse buffers
	//provided by net/http. It does not limit the size of socketio messages.
	WsDefaultBufferSize = 0
)

var (
	ErrorBinaryMessage     = errors.New("Binary messages are not supported")
	ErrorBadBuffer         = errors.New("Buffer error")
	ErrorPacketWrong       = errors.New("Wrong packet type error")
	ErrorMethodNotAllowed  = errors.New("Method not allowed")
	ErrorHttpUpgradeFailed = errors.New("Http upgrade failed")
)

type WebsocketConnection struct {
	socket    *websocket.Conn
	transport *WebsocketTransport
}

func (wsc *WebsocketConnection) GetMessage() (message string, err error) {
	wsc.socket.SetReadDeadline(time.Now().Add(wsc.transport.ReceiveTimeout))
	msgType, reader, err := wsc.socket.NextReader()
	if err != nil {
		return "", err
	}

	//support only text messages exchange
	if msgType != websocket.TextMessage {
		return "", ErrorBinaryMessage
	}

	data, err := ioutil.ReadAll(reader)
	if err != nil {
		return "", err
	}
	text := string(data)

	//empty messages are not allowed
	if len(text) == 0 {
		return "", ErrorPacketWrong
	}

	return text, nil
}

func (wsc *WebsocketConnection) WriteMessage(message string) error {
	wsc.socket.SetWriteDeadline(time.Now().Add(wsc.transport.SendTimeout))

	return wsc.socket.WriteMessage(websocket.TextMessage, []byte(message))
}

func (wsc *WebsocketConnection) Close() {
	wsc.socket.Close()
}

func (wsc *WebsocketConnection) PingParams() (interval, timeout time.Duration) {
	return wsc.transport.PingInterval, wsc.transport.PingTimeout
}

type WebsocketTransport struct {
	//Upgrader used to upgrade all connections, must be non-nil
	Upgrader *websocket.Upgrader

	PingInterval   time.Duration
	PingTimeout    time.Duration
	ReceiveTimeout time.Duration
	SendTimeout    time.Duration

	RequestHeader http.Header
}

func (wst *WebsocketTransport) Connect(url string) (conn Connection, err error) {
	dialer := websocket.Dialer{}
	socket, _, err := dialer.Dial(url, wst.RequestHeader)
	if err != nil {
		return nil, err
	}

	return &WebsocketConnection{socket, wst}, nil
}

func (wst *WebsocketTransport) HandleConnection(
	w http.ResponseWriter, r *http.Request) (conn Connection, err error) {

	if r.Method != "GET" {
		http.Error(w, upgradeFailed+ErrorMethodNotAllowed.Error(), 503)
		return nil, ErrorMethodNotAllowed
	}

	socket, err := wst.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, upgradeFailed+err.Error(), 503)
		return nil, ErrorHttpUpgradeFailed
	}

	return &WebsocketConnection{socket, wst}, nil
}

/**
Websocket connection do not require any additional processing
*/
func (wst *WebsocketTransport) Serve(w http.ResponseWriter, r *http.Request) {}

/**
Returns websocket connection with default params
*/
func GetDefaultWebsocketTransport() *WebsocketTransport {
	return &WebsocketTransport{
		PingInterval:   WsDefaultPingInterval,
		PingTimeout:    WsDefaultPingTimeout,
		ReceiveTimeout: WsDefaultReceiveTimeout,
		SendTimeout:    WsDefaultSendTimeout,
		Upgrader: &websocket.Upgrader{
			HandshakeTimeout: WsDefaultHandshakeTimeout,
		},
	}
}
