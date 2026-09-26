package webview

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
	"sync"
	"time"
)

// IOSSession is an attached Remote Web Inspector connection to one page.
//
// Everything above it is shared with Android: Map and Text are the same
// scripts run through Evaluate, and the coordinate mapping is the same Frame.
// Only the transport and the wrapping differ, which is the whole argument for
// where the seam was put.
type IOSSession struct {
	in   *inspector
	app  string
	page any

	mu       sync.Mutex
	nextID   int
	targetID string
}

// AttachIOS opens a page for evaluation.
//
// The `Socket` field of the context carries the application and page
// identifiers learned during discovery, because they are meaningless outside
// the connection that learned them and there is nowhere better to put them.
// AttachIOS opens a page for evaluation, over an inspector connection that is
// already open.
//
// The connection is passed in rather than dialled here because webinspectord
// answers only the first connection promptly — every later one waits a
// measured 10.2 seconds before its first byte. One connection per device
// session, reused, is the difference between an instant context switch and a
// ten-second one.
//
// The `Socket` field of the context carries the application and page
// identifiers learned during discovery, because they are meaningless outside
// the connection that learned them and there is nowhere better to put them.
func AttachIOS(ctx context.Context, insp *Inspector, c Context) (*IOSSession, error) {
	app, pageRef, ok := strings.Cut(c.Socket, "/")
	if !ok {
		return nil, mobiumerr.New(mobiumerr.NoSuchContext, "context %s carries no page identifier; run app_contexts again", c.ID)
	}

	// The page identifier is a number on the wire. It arrived as one and has
	// to go back as one: sending the string form is accepted and then simply
	// never answers, which is a long way to find a type error. So the live
	// value is looked up rather than parsed out of the context id.
	pages, err := insp.Pages(ctx)
	if err != nil {
		return nil, err
	}
	var page any
	for _, p := range pages {
		if p.App == app && fmt.Sprintf("%v", p.Page) == pageRef {
			page = p.Page
			break
		}
	}
	if page == nil {
		return nil, mobiumerr.New(mobiumerr.NoSuchContext, "page %s is no longer open — run app_contexts again", c.ID)
	}

	s := &IOSSession{in: insp.in, app: app, page: page}
	if err := s.setup(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// setup opens the data channel and waits for the target WebKit will route
// messages through.
func (s *IOSSession) setup(ctx context.Context) error {
	msgs, unsubscribe := s.in.subscribe()
	defer unsubscribe()

	if err := s.in.send("_rpc_forwardSocketSetup:", map[string]any{
		"WIRConnectionIdentifierKey":  s.in.id,
		"WIRApplicationIdentifierKey": s.app,
		"WIRPageIdentifierKey":        s.page,
		"WIRSenderKey":                s.in.id,
		"WIRAutomaticallyPause":       false,
	}); err != nil {
		return err
	}

	deadline := time.After(rwiSetupTimeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return mobiumerr.New(mobiumerr.NoSuchContext, "the page never announced a target to talk to. WebKit is "+
				"multi-target and every command goes through one, so without it nothing can "+
				"be evaluated.\n\nA page's target is announced to one debugger at a time, and "+
				"only once per connection. The usual causes, in order of likelihood: another "+
				"debugger already has this page — Safari's own Web Inspector, or a mobium "+
				"daemon still switched into it (`mobium daemon stop`); or the page was "+
				"attached to earlier on this connection and detached uncleanly.")
		case msg, ok := <-msgs:
			if !ok {
				return s.in.failure("opening the page")
			}
			body := socketData(msg)
			if body == nil {
				continue
			}
			if body["method"] != "Target.targetCreated" {
				continue
			}
			params, _ := body["params"].(map[string]any)
			info, _ := params["targetInfo"].(map[string]any)
			id, _ := info["targetId"].(string)
			if id == "" {
				continue
			}
			s.mu.Lock()
			s.targetID = id
			s.mu.Unlock()
			return nil
		}
	}
}

// socketData pulls the JSON payload out of an _rpc_applicationSentData:
// message, which is where everything CDP-shaped lives.
func socketData(msg rwiMessage) map[string]any {
	if msg.Selector != "_rpc_applicationSentData:" {
		return nil
	}
	raw, _ := msg.Argument["WIRMessageDataKey"].([]byte)
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// call sends one inspector command and waits for its reply.
//
// Both halves are wrapped. The command goes inside Target.sendMessageToTarget
// and the reply comes back inside Target.dispatchMessageFromTarget — sending
// it unwrapped answers "'Runtime' domain was not found", which reads like a
// missing feature rather than a missing envelope and is the single most
// misleading error in this protocol.
func (s *IOSSession) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	s.mu.Lock()
	s.nextID++
	inner := s.nextID
	outer := s.nextID + 100000
	target := s.targetID
	s.mu.Unlock()

	if target == "" {
		return nil, mobiumerr.New(mobiumerr.NoSuchContext, "this page has no target; it was closed or never finished opening")
	}

	// Subscribe before sending: the reply can arrive before Write returns.
	msgs, unsubscribe := s.in.subscribe()
	defer unsubscribe()

	command, err := json.Marshal(map[string]any{"id": inner, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	envelope, err := json.Marshal(map[string]any{
		"id": outer, "method": "Target.sendMessageToTarget",
		"params": map[string]any{"targetId": target, "message": string(command)},
	})
	if err != nil {
		return nil, err
	}
	if err := s.in.send("_rpc_forwardSocketData:", map[string]any{
		"WIRConnectionIdentifierKey":  s.in.id,
		"WIRApplicationIdentifierKey": s.app,
		"WIRPageIdentifierKey":        s.page,
		"WIRSenderKey":                s.in.id,
		"WIRSocketDataKey":            envelope,
	}); err != nil {
		return nil, err
	}

	timeout := time.After(rwiTimeout)
	for {
		select {
		case <-ctx.Done():
			return nil, mobiumerr.New(mobiumerr.Timeout, "%s did not answer in time: %w", method, ctx.Err())
		case <-timeout:
			return nil, mobiumerr.New(mobiumerr.Timeout, "%s did not answer within %s", method, rwiTimeout)
		case msg, ok := <-msgs:
			if !ok {
				return nil, s.in.failure("waiting for " + method)
			}
			body := socketData(msg)
			if body == nil || body["method"] != "Target.dispatchMessageFromTarget" {
				continue
			}
			params, _ := body["params"].(map[string]any)
			text, _ := params["message"].(string)
			var reply struct {
				ID     float64         `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(text), &reply) != nil || int(reply.ID) != inner {
				continue
			}
			if reply.Error != nil {
				return nil, mobiumerr.New(mobiumerr.DeviceServer, "%s: %s", method, reply.Error.Message)
			}
			return reply.Result, nil
		}
	}
}

// Evaluate runs an expression in the page and returns its value as a string.
func (s *IOSSession) Evaluate(ctx context.Context, expression string) (string, error) {
	raw, err := s.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Result struct {
			Type        string          `json:"type"`
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
		} `json:"result"`
		// WebKit reports a thrown exception inside a successful reply, the
		// same trap CDP has: no error field is not evidence the script ran.
		WasThrown bool `json:"wasThrown"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.WasThrown {
		msg := result.Result.Description
		if msg == "" {
			msg = string(result.Result.Value)
		}
		return "", mobiumerr.New(mobiumerr.DeviceServer, "page script failed: %s", firstLine(msg))
	}
	if result.Result.Type == "string" {
		var str string
		if json.Unmarshal(result.Result.Value, &str) == nil {
			return str, nil
		}
	}
	return string(result.Result.Value), nil
}

// LayoutMetrics reads the page's visual viewport.
//
// iOS has no Page.getLayoutMetrics, and it does not need one: `innerWidth` and
// `innerHeight` *are* the visual viewport, so the distinction that produced
// defect 6 on Android cannot be got wrong here. Asking the page directly is
// both simpler and more correct.
func (s *IOSSession) LayoutMetrics(ctx context.Context) (*Metrics, error) {
	raw, err := s.Evaluate(ctx, `JSON.stringify([window.innerWidth, window.innerHeight])`)
	if err != nil {
		return nil, err
	}
	var wh []float64
	if err := json.Unmarshal([]byte(raw), &wh); err != nil || len(wh) != 2 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the page reported its viewport as %q", raw)
	}
	if wh[0] <= 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the page reported a zero-width viewport")
	}
	return &Metrics{CSSWidth: wh[0], CSSHeight: wh[1]}, nil
}

// Map returns the page's actionable elements.
func (s *IOSSession) Map(ctx context.Context) ([]Element, error) { return mapPage(ctx, s) }

// Text returns the page's visible text.
func (s *IOSSession) Text(ctx context.Context) (string, error) { return pageText(ctx, s) }

// Healthy reports whether the page still answers.
func (s *IOSSession) Healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.Evaluate(ctx, "1")
	return err == nil
}

// Close detaches from the page.
//
// `_rpc_forwardDidClose:` matters and is easy to skip, because nothing
// complains if you do — until the next attach. Target.targetCreated is
// announced once per page per connection, so a page left open on a held
// connection cannot be attached to again: the second setup is accepted and
// simply never announces a target, and the failure reads as "the page never
// announced a target" on a page that is plainly there. Saying goodbye is what
// makes the next hello work.
//
// The inspector connection itself is deliberately left open — it is shared
// with the session that owns it, and dropping it would cost ten seconds the
// next time anything needed it. The session closes that.
func (s *IOSSession) Close() error {
	err := s.in.send("_rpc_forwardDidClose:", map[string]any{
		"WIRConnectionIdentifierKey":  s.in.id,
		"WIRApplicationIdentifierKey": s.app,
		"WIRPageIdentifierKey":        s.page,
		"WIRSenderKey":                s.in.id,
	})
	s.mu.Lock()
	s.targetID = ""
	s.mu.Unlock()
	return err
}
