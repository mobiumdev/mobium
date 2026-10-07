package mobiumdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// A screen too large to read with visible is read without it, and what is
// shown worked out from where it is. The fake is WebDriverAgent on
// Radiolab's page: a full read outlasts the client, a light one answers.
// The first read times out and falls back; the next goes light at once; a
// small screen after it is read in full again. CHALLENGES 258.
func TestAHugeScreenIsReadWithoutVisible(t *testing.T) {
	list := func(rows int) string {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><XCUIElementTypeApplication type="XCUIElementTypeApplication" ` +
			`name="x" label="x" enabled="true" accessible="false" x="0" y="0" width="430" height="932" bundleId="app">` +
			`<XCUIElementTypeTable type="XCUIElementTypeTable" enabled="true" accessible="false" x="0" y="0" width="430" height="932">`)
		for i := 0; i < rows; i++ {
			fmt.Fprintf(&b, `<XCUIElementTypeCell type="XCUIElementTypeCell" enabled="true" accessible="false" x="0" y="%d" width="430" height="70">`+
				`<XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="Episode %d" label="Episode %d" enabled="true" accessible="true" x="16" y="120" width="300" height="20"/>`+
				`</XCUIElementTypeCell>`, i*70, i, i)
		}
		b.WriteString(`</XCUIElementTypeTable></XCUIElementTypeApplication>`)
		return b.String()
	}
	var mu sync.Mutex
	rows, full := 700, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/source") {
			w.Write([]byte(`{"value":null}`))
			return
		}
		mu.Lock()
		n := rows
		light := r.URL.Query().Get("excluded_attributes") != ""
		if !light {
			full++
		}
		mu.Unlock()
		if !light && n >= 500 {
			time.Sleep(700 * time.Millisecond)
		}
		b, _ := json.Marshal(map[string]string{"value": list(n)})
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	c := newW3CClient(300 * time.Millisecond)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1}

	read := func(want bool) *uitree.Tree {
		t.Helper()
		tree, err := d.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if tree.VisibilityInferred != want {
			t.Errorf("visibility inferred %v, want %v", tree.VisibilityInferred, want)
		}
		return tree
	}
	tree := read(true)
	for _, e := range tree.Map() {
		if e.Node.Bounds.Y1 >= 932 {
			t.Errorf("map lists %q at %v, below the screen", e.Label, e.Node.Bounds)
		}
	}
	if len(tree.Map()) == 0 {
		t.Error("map lists nothing of the rows on screen")
	}
	read(true)
	mu.Lock()
	if full != 1 {
		t.Errorf("%d full reads, want the one that timed out", full)
	}
	rows = 5
	mu.Unlock()
	read(false)
	read(false)
	mu.Lock()
	defer mu.Unlock()
	if full != 3 {
		t.Errorf("%d full reads, want two more once the screen is small", full)
	}
}
