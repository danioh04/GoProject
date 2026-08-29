package wsutil_test

import (
	"context"
	"encoding/json"
	"geoduel/internal/wsutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func startRawServer(t *testing.T, afterAccept func(conn wsutil.Conn)) *websocket.Conn {
	t.Helper()
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsutil.Accept(w, r)
		if err != nil {
			return
		}
		close(ready)
		afterAccept(conn)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wsURL := "ws" + srv.URL[len("http"):]
	clientConn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { clientConn.CloseNow() })

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("server never accepted")
	}
	return clientConn
}

func TestSendOverflowBeforeAnyDrain(t *testing.T) {
	var session *wsutil.Session
	ready := make(chan struct{})
	client := startRawServer(t, func(conn wsutil.Conn) {
		session = wsutil.NewSession(conn, wsutil.SessionConfig{SendBuffer: 4})
		close(ready)
	})
	<-ready

	env, _ := wsutil.NewEnvelope(wsutil.TypeRoster, nil)
	for i := 0; i < 4; i++ {
		if !session.Send(env) {
			t.Fatalf("send %d into empty buffer rejected", i+1)
		}
	}
	if session.Send(env) {
		t.Error("send past full buffer accepted; backpressure broken")
	}

	session.Kick()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := client.Read(ctx); err == nil {
		t.Fatal("client connection still open after Kick")
	}
	if session.Send(env) {
		t.Error("Send accepted after teardown")
	}
}

func TestRunDeliversAndReportsClose(t *testing.T) {
	got := make(chan wsutil.Envelope, 1)
	closed := make(chan struct{})

	client := startRawServer(t, func(conn wsutil.Conn) {
		session := wsutil.NewSession(conn, wsutil.SessionConfig{})
		go func() {
			session.Run(func(env wsutil.Envelope) { got <- env }, func() { close(closed) })
		}()
	})

	env, _ := wsutil.NewEnvelope(wsutil.TypeStartGame, map[string]string{"x": "y"})
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case received := <-got:
		if received.Type != wsutil.TypeStartGame || string(received.Payload) != `{"x":"y"}` {
			t.Errorf("received = %+v", received)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("message never delivered")
	}

	if err := client.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Logf("client close: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("onClose never fired")
	}
}
