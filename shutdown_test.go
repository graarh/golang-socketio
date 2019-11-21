package gosocketio_test

import (
	"context"
	"net"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	gosocketio "github.com/VerticalOps/golang-socketio"
	"github.com/VerticalOps/golang-socketio/transport"
)

func TestStartupShutdown(t *testing.T) {
	transp := transport.GetDefaultWebsocketTransport()
	socketSrv := gosocketio.NewServer(transp, gosocketio.WithErrorHandler(func(err error) {
		//We just want to log this, the test is for starting and shutdown
		//the server with a client connected
		t.Logf("Error from ErrorHandler: %v", err)
	}))

	socketSrv.On(gosocketio.OnConnection, func(c *gosocketio.Channel) {
		t.Logf("Client (%s) connected", c.Id())
	})

	socketSrv.On(gosocketio.OnDisconnection, func(c *gosocketio.Channel) {
		t.Logf("Client (%s) disconnected", c.Id())
	})

	socketSrv.On("MyMethod", func(c *gosocketio.Channel, msg string) {
		t.Logf("Received (%s) from (%s)", msg, c.Id())
		if err := c.Emit("MyMethod", "Goodbye"); err != nil {
			t.Errorf("Error when emitting to client: %v", err)
		}
	})

	httpSrv := httptest.NewServer(socketSrv)
	defer httpSrv.Close()

	////silly we have to do this
	u, err := url.Parse(httpSrv.URL)
	if err != nil {
		t.Fatalf("Unable to parse URL: %v", err)
	}

	host, portS, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("Unable to split url: %v", err)
	}

	port, err := strconv.Atoi(portS)
	if err != nil {
		t.Fatalf("Unable to get http port: %v", err)
	}
	////

	//client doesn't even let us register methods before connecting...
	client, err := gosocketio.Dial(gosocketio.GetUrl(host, port, false), transp)
	if err != nil {
		t.Fatalf("Unable to create socketio client: %v", err)
	}

	//Do note the client is still connected when Shutdown is called below
	defer client.Close()

	client.On("MyMethod", func(c *gosocketio.Channel, msg string) {
		t.Logf("Received (%s) from (%s)", msg, c.Id())
	})

	if err := client.Emit("MyMethod", "Hello"); err != nil {
		t.Errorf("Error when emitting to server: %v", err)
	}

	//sleep just a moment since we're running the server and client in the same process
	//not actually needed for the test to pass but it lets the callbacks run so we see output
	time.Sleep(time.Millisecond * 200)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := socketSrv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failure: %v", err)
	}
}
