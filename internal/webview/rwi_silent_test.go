package webview

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/plist"
)

// fakeListings plays webinspectord for listings only: PID:1 answers each
// _rpc_forwardGetListing: with one page at once, and PID:2 answers only once
// speak is set — the shape of com.apple.AppStore.Widgets after an
// accessibility setting changed on an iPhone, which answered none.
func fakeListings(t *testing.T, conn net.Conn, speak *atomic.Bool) {
	t.Helper()
	write := func(sel string, arg map[string]any) {
		body, err := plist.Marshal(map[string]any{"__selector": sel, "__argument": arg})
		if err != nil {
			t.Error(err)
			return
		}
		head := make([]byte, 4)
		binary.BigEndian.PutUint32(head, uint32(len(body)))
		conn.Write(append(head, body...))
	}
	go func() {
		for {
			head := make([]byte, 4)
			if _, err := io.ReadFull(conn, head); err != nil {
				return
			}
			body := make([]byte, binary.BigEndian.Uint32(head))
			if _, err := io.ReadFull(conn, body); err != nil {
				return
			}
			v, err := plist.Unmarshal(body)
			if err != nil {
				continue
			}
			m, _ := v.(map[string]any)
			if m["__selector"] != "_rpc_forwardGetListing:" {
				continue
			}
			arg, _ := m["__argument"].(map[string]any)
			app, _ := arg["WIRApplicationIdentifierKey"].(string)
			if app == "PID:2" && !speak.Load() {
				continue
			}
			listing := map[string]any{}
			if app == "PID:1" {
				listing["1"] = map[string]any{"WIRPageIdentifierKey": int64(1), "WIRTitleKey": "Motion"}
			}
			write("_rpc_applicationSentListing:", map[string]any{
				"WIRApplicationIdentifierKey": app,
				"WIRListingKey":               listing,
			})
		}
	}()
}

// One application that never answers a listing held every app_contexts to
// the whole setup timeout — 25s, and an attach twice that — on an iPhone
// after an accessibility setting changed (CHALLENGES 116). Now the rest get
// listingGrace once one has answered, the silent one is remembered so the
// next listing does not wait for it at all, and it is forgotten the moment
// it answers.
func TestAListingDoesNotWaitForAnApplicationThatNeverAnswers(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	var speak atomic.Bool
	fakeListings(t, server, &speak)

	in := newInspector(client, "test")
	i := &Inspector{
		in:     in,
		apps:   map[string]string{"PID:1": "dev.mobium.mobiumapp", "PID:2": "com.apple.AppStore.Widgets"},
		silent: map[string]bool{},
	}
	// The watch, as openWith starts it: it is what hears a late answer.
	msgs, unsubscribe := in.subscribe()
	go i.watch(msgs, unsubscribe)
	ctx := context.Background()

	start := time.Now()
	ctxs, err := i.Contexts(ctx)
	first := time.Since(start)
	if err != nil || len(ctxs) != 1 {
		t.Fatalf("contexts = %v, %v; want the one page", ctxs, err)
	}
	if first >= rwiSetupTimeout/2 {
		t.Fatalf("the first listing took %s, waiting on the silent application", first)
	}
	i.mu.Lock()
	silent := map[string]bool{"PID:1": i.silent["PID:1"], "PID:2": i.silent["PID:2"]}
	i.mu.Unlock()
	if !silent["PID:2"] || silent["PID:1"] {
		t.Fatalf("silent = %v, want only PID:2", silent)
	}

	start = time.Now()
	if _, err := i.Contexts(ctx); err != nil {
		t.Fatal(err)
	}
	if second := time.Since(start); second >= listingGrace {
		t.Errorf("the second listing took %s; an application known to be silent was waited for", second)
	}

	// It starts answering. The listing that asks may already have stopped
	// waiting for it, so its reply arrives late — and the watch hears it, so
	// the next listing waits for it again.
	speak.Store(true)
	if _, err := i.Contexts(ctx); err != nil {
		t.Fatal(err)
	}
	// The watch hears it on its own goroutine, so poll to a deadline rather
	// than pause. This failed one run in five until list stopped marking
	// silent an application whose reply the watch had just heard.
	var still bool
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		i.mu.Lock()
		still = i.silent["PID:2"]
		i.mu.Unlock()
		if !still || time.Now().After(deadline) {
			break
		}
	}
	if still {
		t.Fatalf("PID:2 answered and is still marked silent")
	}
	if _, err := i.Contexts(ctx); err != nil {
		t.Fatal(err)
	}
	i.mu.Lock()
	still = i.silent["PID:2"]
	i.mu.Unlock()
	if still {
		t.Errorf("PID:2 was waited for, answered, and was marked silent again")
	}
}
