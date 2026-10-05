package webview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mobiumdev/mobium/internal/device"
)

// evalTimeout bounds one Runtime.evaluate round trip.
var evalTimeout = 30 * time.Second

// Session is an attached CDP connection to one web target.
type Session struct {
	adb    *device.ADB
	conn   *websocket.Conn
	port   int
	nextID int
	mu     sync.Mutex
}

// Attach forwards the context's socket and opens its CDP WebSocket.
func Attach(ctx context.Context, adb *device.ADB, c Context) (*Session, error) {
	port, err := adb.ForwardAbstract(ctx, c.Socket)
	if err != nil {
		return nil, err
	}

	// The debugger URL was minted against an earlier, now-closed forward, so
	// only its path is reusable; the host must point at the port just opened.
	target, err := url.Parse(c.WSURL)
	if err != nil {
		adb.RemoveForward(ctx, port)
		return nil, fmt.Errorf("parse debugger url %q: %w", c.WSURL, err)
	}
	target.Host = "127.0.0.1:" + strconv.Itoa(port)

	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, target.String(), nil)
	if err != nil {
		// On a failed handshake gorilla hands back the HTTP response, whose
		// body is ours to close. On success it is nil.
		if resp != nil {
			resp.Body.Close()
		}
		adb.RemoveForward(ctx, port)
		return nil, fmt.Errorf("attach to %s: %w", c.ID, err)
	}
	return &Session{adb: adb, conn: conn, port: port}, nil
}

// Close drops the connection and releases the forwarded port.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	if s.port != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.adb.RemoveForward(ctx, s.port)
		s.port = 0
	}
	return nil
}

// Healthy reports whether the connection still answers.
func (s *Session) Healthy(ctx context.Context) bool {
	_, err := s.Evaluate(ctx, "1")
	return err == nil
}

// call sends one CDP command and waits for the matching reply, skipping the
// events that arrive on the same socket.
func (s *Session) call(ctx context.Context, method string, params map[string]interface{}) (json.RawMessage, error) {
	return s.callCollect(ctx, method, params, "", nil)
}

// callCollect is call, handing every event named event that arrives before
// the reply to collect: Runtime.enable announces each frame's context as an
// event ahead of its own reply.
func (s *Session) callCollect(ctx context.Context, method string, params map[string]interface{},
	event string, collect func(json.RawMessage)) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil, mobiumerr.New(mobiumerr.NoSuchContext, "the webview session is closed")
	}

	s.nextID++
	id := s.nextID
	if params == nil {
		params = map[string]interface{}{}
	}
	msg, err := json.Marshal(map[string]interface{}{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}

	// Each round trip is bounded by evalTimeout even inside a call with a
	// later deadline of its own, as WebKit's is by rwiTimeout. It was used
	// only when the call had none, and every tool call has one — four
	// minutes — so a page whose renderer had gone held a context switch for
	// all of it. CHALLENGES 203.
	deadline := time.Now().Add(evalTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	s.conn.SetWriteDeadline(deadline)
	if err := s.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		return nil, cdpErr(method, err)
	}

	s.conn.SetReadDeadline(deadline)
	for {
		_, data, err := s.conn.ReadMessage()
		if err != nil {
			return nil, cdpErr(method, err)
		}
		var resp struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &resp) != nil {
			continue
		}
		if collect != nil && resp.ID == 0 && resp.Method == event {
			collect(resp.Params)
			continue
		}
		// id 0 means an event (Page.loadEventFired and friends), not our reply.
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "%s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// cdpErr names a round trip that ran out of time as a timeout, as the iOS
// transport does, rather than as the socket's own i/o error.
func cdpErr(method string, err error) error {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return mobiumerr.New(mobiumerr.Timeout, "%s did not answer in time: %w", method, err)
	}
	return fmt.Errorf("%s: %w", method, err)
}

// Evaluate runs an expression in the page and returns its value as a string.
func (s *Session) Evaluate(ctx context.Context, expression string) (string, error) {
	return s.evaluateIn(ctx, 0, expression)
}

// evaluateIn is Evaluate in one frame's execution context; 0 is the page's.
func (s *Session) evaluateIn(ctx context.Context, contextID int, expression string) (string, error) {
	params := map[string]interface{}{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	}
	if contextID != 0 {
		params["contextId"] = contextID
	}
	raw, err := s.call(ctx, "Runtime.evaluate", params)
	if err != nil {
		return "", err
	}

	var result struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	// A thrown exception comes back inside a successful response, so the
	// absence of a CDP error is not evidence the script ran.
	if d := result.ExceptionDetails; d != nil {
		msg := d.Text
		if d.Exception != nil && d.Exception.Description != "" {
			msg = d.Exception.Description
		}
		return "", mobiumerr.New(mobiumerr.DeviceServer, "page script failed: %s", firstLine(msg))
	}

	if result.Result.Type == "string" {
		var s string
		if err := json.Unmarshal(result.Result.Value, &s); err == nil {
			return s, nil
		}
	}
	return string(result.Result.Value), nil
}

// Metrics is what the page reports about its own geometry.
type Metrics struct {
	// CSSWidth and CSSHeight are the viewport in CSS pixels, which is the
	// coordinate space getBoundingClientRect returns.
	CSSWidth  float64
	CSSHeight float64
}

// LayoutMetrics reads the page's viewport size.
//
// The *visual* viewport is the one that matters, not the layout viewport.
// They are equal on an ordinary page, which is exactly why this is easy to get
// wrong: on a page whose content is wider than the screen the layout viewport
// expands to the content, while the visual viewport stays the size of what is
// actually displayed. Android's third-party licenses page reports a layout
// viewport of 1648 CSS px against a visual viewport of 412 — scaling by the
// former puts every tap at roughly a quarter of the right offset.
func (s *Session) LayoutMetrics(ctx context.Context) (*Metrics, error) {
	raw, err := s.call(ctx, "Page.getLayoutMetrics", nil)
	if err != nil {
		return nil, err
	}
	var m struct {
		CSSVisualViewport struct {
			ClientWidth  float64 `json:"clientWidth"`
			ClientHeight float64 `json:"clientHeight"`
		} `json:"cssVisualViewport"`
		CSSLayoutViewport struct {
			ClientWidth  float64 `json:"clientWidth"`
			ClientHeight float64 `json:"clientHeight"`
		} `json:"cssLayoutViewport"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}

	w, h := m.CSSVisualViewport.ClientWidth, m.CSSVisualViewport.ClientHeight
	if w <= 0 {
		// Older WebViews omit the visual viewport; the layout one is then the
		// best available answer and is correct for an unzoomed page.
		w, h = m.CSSLayoutViewport.ClientWidth, m.CSSLayoutViewport.ClientHeight
	}
	if w <= 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the page reported a zero-width viewport")
	}
	return &Metrics{CSSWidth: w, CSSHeight: h}, nil
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}
