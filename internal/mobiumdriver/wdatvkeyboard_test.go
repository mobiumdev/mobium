package mobiumdriver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// fakeTVKeyboard serves an Apple TV with its keyboard up over Morpho
// Market's cart, editing the discount field, as captured on a tvOS 26.5
// simulator. The keyboard is the focused element; its text view, TV1, holds
// what has been typed. It counts what was typed into each element.
func fakeTVKeyboard(t *testing.T) (*WDA, map[string]int, func()) {
	t.Helper()
	xml, err := os.ReadFile("../uitree/testdata/ios-tvos26-morpho-keyboard-empty.xml")
	if err != nil {
		t.Fatal(err)
	}
	src, _ := json.Marshal(map[string]string{"value": string(xml)})
	var mu sync.Mutex
	typed := map[string]int{}
	held := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/source"):
			w.Write(src)
		case strings.HasSuffix(p, "/element/active"):
			w.Write([]byte(`{"value":{"ELEMENT":"KB"}}`))
		case strings.HasSuffix(p, "/KB/attribute/type"):
			w.Write([]byte(`{"value":"XCUIElementTypeKeyboard"}`))
		case strings.HasSuffix(p, "/element") && r.Method == http.MethodPost:
			var body struct{ Value string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if strings.Contains(body.Value, "XCUIElementTypeTextView") {
				w.Write([]byte(`{"value":{"ELEMENT":"TV1"}}`))
			} else {
				w.Write([]byte(`{"value":{"ELEMENT":"APPFIELD"}}`))
			}
		case strings.HasSuffix(p, "/wda/keys"):
			var body struct{ Value []string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			typed["KB"]++
			held += strings.Join(body.Value, "")
			w.Write([]byte(`{"value":null}`))
		case strings.HasSuffix(p, "/value") && r.Method == http.MethodPost:
			var body struct{ Value []string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			id := strings.Split(strings.TrimSuffix(p, "/value"), "/element/")[1]
			typed[id]++
			if id == "TV1" {
				held += strings.Join(body.Value, "")
			}
			w.Write([]byte(`{"value":null}`))
		case strings.HasSuffix(p, "/TV1/clear"):
			held = ""
			w.Write([]byte(`{"value":null}`))
		case strings.HasSuffix(p, "/TV1/attribute/value"):
			v, _ := json.Marshal(map[string]string{"value": held})
			w.Write(v)
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	return &WDA{w3c: c, scale: 1, sim: &device.Simctl{TV: true}}, typed, srv.Close
}

// Typed at the focus, the text lands in the keyboard's text view, and is
// read back there: the keyboard itself holds no text, and asked of it three
// keys that had landed read as dropped and were typed twice more
// (CHALLENGES 333).
func TestWDATypeIntoFocusOnAppleTVReadsTheKeyboardsField(t *testing.T) {
	d, typed, done := fakeTVKeyboard(t)
	defer done()
	f, err := d.TypeIntoFocus(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "codeField" || f.Value != "abc" {
		t.Errorf("reported %+v, want codeField holding abc", f)
	}
	if typed["KB"] != 1 || typed["TV1"] != 0 {
		t.Errorf("typed %v, want the keys sent once and no retry", typed)
	}
}

// app_fill on the field the keyboard edits types into the keyboard's text
// view, not the app's field behind it; on another field it is refused,
// naming the one the keyboard is up for.
func TestWDASetTextOnAppleTVGoesThroughTheKeyboard(t *testing.T) {
	d, typed, done := fakeTVKeyboard(t)
	defer done()
	code := &uitree.Node{TestID: "codeField", Class: "XCUIElementTypeTextField"}
	if err := d.SetText(context.Background(), code, "WELCOME10"); err != nil {
		t.Fatal(err)
	}
	if typed["TV1"] != 1 || typed["APPFIELD"] != 0 {
		t.Errorf("typed %v, want once into the keyboard's text view", typed)
	}
	name := &uitree.Node{TestID: "field_name", Class: "XCUIElementTypeTextField"}
	err := d.SetText(context.Background(), name, "Ada")
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "up for testid=codeField") {
		t.Errorf("another field: %v", err)
	}
}

// Asked whether the field has focus, an Apple TV with its keyboard up says
// the field it edits does, and no other.
func TestWDAHasFocusOnAppleTVIsTheFieldTheKeyboardEdits(t *testing.T) {
	d, _, done := fakeTVKeyboard(t)
	defer done()
	for id, want := range map[string]bool{"codeField": true, "field_name": false} {
		got, err := d.HasFocus(context.Background(), &uitree.Node{TestID: id, Class: "XCUIElementTypeTextField"})
		if err != nil || got != want {
			t.Errorf("%s: HasFocus = %v, %v; want %v", id, got, err, want)
		}
	}
}
