package webview

import (
	"encoding/binary"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"net"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/plist"
)

// Remote Web Inspector: how WebKit exposes a page for debugging, and the only
// way into a WKWebView. It is a third protocol after WebDriver BiDi and CDP,
// which is why iOS WebViews sat unstarted for so long — see
// docs/decisions/0002-ios-webviews-are-reachable.md for what turned out to be
// wrong about that.
//
// Three things make it unlike CDP, and each one is a trap:
//
//  1. **The transport is a Unix socket carrying binary property lists**, four
//     bytes of big-endian length then a plist. Inside `WIRSocketDataKey` the
//     payload is ordinary JSON, so everything above this file is shared with
//     Android.
//  2. **Getting a page takes a conversation, not a request.** You announce
//     yourself, are told about applications, ask one for a listing, are sent
//     it, then set up a socket to a page. Nothing is a request/response pair;
//     replies arrive as separate messages whenever WebKit feels like it.
//  3. **WebKit is multi-target.** An unwrapped `Runtime.evaluate` answers
//     "'Runtime' domain was not found", which reads like a missing domain and
//     is not: since iOS 12.2 commands go inside `Target.sendMessageToTarget`
//     and replies come back inside `Target.dispatchMessageFromTarget`.

// rwiTimeout bounds one round trip once a page is attached.
const rwiTimeout = 30 * time.Second

// rwiSetupTimeout bounds the whole announce-list-attach conversation. It is
// generous because a cold WebView can take seconds to publish a listing.
const rwiSetupTimeout = 25 * time.Second

// maxRWIMessage caps one framed message. A page's innerText can be large, so
// the limit is generous; unbounded would let a confused peer exhaust memory in
// the daemon.
const maxRWIMessage = 64 << 20

// rwiMessage is one decoded message from webinspectord.
type rwiMessage struct {
	Selector string
	Argument map[string]any
}

// inspector is a connection to a simulator's Remote Web Inspector.
//
// One reader goroutine owns the socket and fans messages out to whoever is
// waiting. An earlier design read on the calling goroutine, which cannot work
// here: the answer to a request is not the next message on the wire, and a
// caller that consumed somebody else's reply would hang them both.
type inspector struct {
	conn net.Conn
	id   string // our connection identifier, echoed in every message

	writeMu sync.Mutex

	mu       sync.Mutex
	handlers []chan rwiMessage

	closeOnce sync.Once
	closed    chan struct{}
	readErr   error
}

func dialInspector(socket, id string) (*inspector, error) {
	conn, err := net.DialTimeout("unix", socket, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("could not reach the simulator's web inspector at %s: %w", socket, err)
	}
	return newInspector(conn, id), nil
}

// newInspector speaks the protocol over any stream: a simulator's Unix
// socket, or a real iPhone's service reached through lockdown. The protocol
// is the same on both — measured, message for message — so only how the
// stream is opened differs.
func newInspector(conn net.Conn, id string) *inspector {
	in := &inspector{conn: conn, id: id, closed: make(chan struct{})}
	go in.readLoop()
	return in
}

// subscribe returns a channel of every message that arrives from now on, and a
// function to stop listening. Subscribing before sending is what makes this
// safe: the answer may arrive before the send call returns.
func (in *inspector) subscribe() (<-chan rwiMessage, func()) {
	ch := make(chan rwiMessage, 64)
	in.mu.Lock()
	in.handlers = append(in.handlers, ch)
	in.mu.Unlock()
	return ch, func() {
		in.mu.Lock()
		for i, h := range in.handlers {
			if h == ch {
				in.handlers = append(in.handlers[:i], in.handlers[i+1:]...)
				break
			}
		}
		in.mu.Unlock()
	}
}

func (in *inspector) readLoop() {
	defer func() {
		in.mu.Lock()
		for _, h := range in.handlers {
			close(h)
		}
		in.handlers = nil
		in.mu.Unlock()
	}()

	for {
		head := make([]byte, 4)
		if _, err := io.ReadFull(in.conn, head); err != nil {
			in.readErr = err
			return
		}
		n := binary.BigEndian.Uint32(head)
		if n > maxRWIMessage {
			in.readErr = mobiumerr.New(mobiumerr.DeviceServer, "the web inspector announced a %d byte message", n)
			return
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(in.conn, body); err != nil {
			in.readErr = err
			return
		}
		v, err := plist.Unmarshal(body)
		if err != nil {
			// Not fatal: one unreadable message should not end the session.
			// It is recorded so a later failure can name it.
			in.readErr = fmt.Errorf("could not read a message from the web inspector: %w", err)
			continue
		}
		m, _ := v.(map[string]any)
		sel, _ := m["__selector"].(string)
		arg, _ := m["__argument"].(map[string]any)
		msg := rwiMessage{Selector: sel, Argument: arg}

		in.mu.Lock()
		for _, h := range in.handlers {
			select {
			case h <- msg:
			default: // a slow listener must not stall the socket
			}
		}
		in.mu.Unlock()
	}
}

// send writes one selector call.
func (in *inspector) send(selector string, arg map[string]any) error {
	body, err := plist.Marshal(map[string]any{"__selector": selector, "__argument": arg})
	if err != nil {
		return fmt.Errorf("could not encode %s: %w", selector, err)
	}
	head := make([]byte, 4)
	binary.BigEndian.PutUint32(head, uint32(len(body)))

	in.writeMu.Lock()
	defer in.writeMu.Unlock()
	if _, err := in.conn.Write(append(head, body...)); err != nil {
		return fmt.Errorf("could not send %s to the web inspector: %w", selector, err)
	}
	return nil
}

func (in *inspector) Close() error {
	in.closeOnce.Do(func() {
		close(in.closed)
		in.conn.Close()
	})
	return nil
}

// announce introduces us to webinspectord, which answers with the current
// state and the list of applications that have inspectable pages.
func (in *inspector) announce() error {
	return in.send("_rpc_reportIdentifier:", map[string]any{
		"WIRConnectionIdentifierKey": in.id,
	})
}
