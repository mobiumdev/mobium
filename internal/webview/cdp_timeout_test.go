package webview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A page whose renderer has gone accepts the debugger's connection and never
// answers. Each round trip is bounded by evalTimeout even inside a call with
// a much later deadline — every tool call has one — and it ends as a timeout,
// not the socket's i/o error. CHALLENGES 203.
func TestASilentPageTimesOutPerCall(t *testing.T) {
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	old := evalTimeout
	evalTimeout = 300 * time.Millisecond
	defer func() { evalTimeout = old }()

	s := &Session{conn: conn}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	start := time.Now()
	_, err = s.Evaluate(ctx, "1")
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("a silent page held the call %s, inside a one-minute deadline", took)
	}
	if mobiumerr.CodeOf(err) != mobiumerr.Timeout {
		t.Errorf("a silent page answered %v, want a timeout", err)
	}
	_ = s.Close()
}
