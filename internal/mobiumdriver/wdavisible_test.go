package mobiumdriver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// A visible element is shown visible by one find that asks for it, not a
// find and then a question, and the rectangle read after it finds it again
// for nothing; a hidden one is not found, and is not called visible.
// CHALLENGES 272.
func TestVisibilityIsAskedInTheFind(t *testing.T) {
	var mu sync.Mutex
	var finds []string
	visibleAsked := 0
	hidden := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/element") && r.Method == http.MethodPost:
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			finds = append(finds, body["value"])
			if hidden && strings.Contains(body["value"], "visible == 1") {
				w.Write([]byte(`{"value":{"error":"no such element","message":"none"}}`))
				return
			}
			w.Write([]byte(`{"value":{"ELEMENT":"E1"}}`))
		case strings.HasSuffix(r.URL.Path, "/attribute/visible"):
			visibleAsked++
			w.Write([]byte(`{"value":true}`))
		case strings.HasSuffix(r.URL.Path, "/rect"):
			w.Write([]byte(`{"value":{"x":10,"y":20,"width":30,"height":40}}`))
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1}
	n := &uitree.Node{TestID: "edgeTarget", Class: "XCUIElementTypeButton"}
	tree := &uitree.Tree{Root: &uitree.Node{Children: []*uitree.Node{n}}}

	visible, asked, err := d.ElementVisible(context.Background(), n, tree)
	if err != nil || !asked || !visible {
		t.Fatalf("visible %v asked %v: %v", visible, asked, err)
	}
	if _, _, err := d.ElementBounds(context.Background(), n, tree); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(finds) != 1 || !strings.HasSuffix(finds[0], " AND visible == 1") || visibleAsked != 0 {
		t.Errorf("finds %q and %d visibility requests, want one find asking visible and none", finds, visibleAsked)
	}
	hidden, finds = true, nil
	mu.Unlock()
	d.forgetFound()
	if visible, asked, err := d.ElementVisible(context.Background(), n, tree); err != nil || !asked || visible {
		t.Errorf("a hidden element: visible %v asked %v err %v", visible, asked, err)
	}
}
