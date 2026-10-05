package mobiumdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// boxes builds an iOS hierarchy of six one-digit fields, as MobiumApp's OTP
// Demo has them, holding the given values; an empty value has no value
// attribute, as WebDriverAgent reports an empty field.
func boxes(t *testing.T, values ...string) (*uitree.Tree, *uitree.Node) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="MobiumApp" bundleId="dev.mobium.mobiumapp" x="0" y="0" width="402" height="874" visible="true" enabled="true">`)
	for i, v := range values {
		val := ""
		if v != "" {
			val = fmt.Sprintf(` value="%s"`, v)
		}
		fmt.Fprintf(&b, `<XCUIElementTypeTextField type="XCUIElementTypeTextField" name="otpBox%d"%s x="%d" y="400" width="46" height="54" visible="true" enabled="true" accessible="true"/>`, i, val, 16+i*60)
	}
	b.WriteString(`</XCUIElementTypeApplication>`)
	tree, err := uitree.ParseIOS([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	var first *uitree.Node
	tree.Walk(func(n *uitree.Node) bool {
		if n.TestID == "otpBox0" {
			first = n
			return false
		}
		return true
	})
	return tree, first
}

func TestFindSpread(t *testing.T) {
	for _, c := range []struct {
		name   string
		values []string
		spread bool
		lost   string
	}{
		// Measured on the iPhone 17 Pro simulator: one attempt at "123456"
		// into the first box left 1, 3, 4, 5, 6 — the 2 lost as focus moved.
		{"measured, one lost", []string{"1", "3", "4", "5", "6", ""}, true, "2"},
		{"every digit arrived", []string{"1", "2", "3", "4", "5", "6"}, true, ""},
		// A single field that dropped a keystroke has nothing after it.
		{"dropped, nothing after", []string{"1235", "", "", "", "", ""}, false, ""},
		// Something unrelated in the next field is not this text.
		{"next field holds other text", []string{"1", "9", "", "", "", ""}, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			tree, target := boxes(t, c.values...)
			sp, ok := findSpread(tree, target, "123456")
			if ok != c.spread {
				t.Fatalf("spread = %v, want %v (%+v)", ok, c.spread, sp)
			}
			if ok && sp.Lost != c.lost {
				t.Errorf("lost %q, want %q", sp.Lost, c.lost)
			}
		})
	}
}

func TestSpreadErrorSaysWhereEachPartWent(t *testing.T) {
	err := spreadError("123456", spread{Parts: []string{"1", "3", "4", "5", "6"}, Lost: "2"})
	for _, want := range []string{`the field took "1"`, `"3", "4", "5", "6"`, `"2" never arrived`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not say %s", err, want)
		}
	}
}

// fakeBoxes is WebDriverAgent in front of six one-digit boxes that move focus
// on by themselves: text typed into a box fills it and the ones after, one
// character each, and the first whole-code type loses the character at
// loseAt as focus moves (-1 for none). A box in stuck takes nothing.
type fakeBoxes struct {
	mu     sync.Mutex
	vals   [6]string
	loseAt int
	stuck  int
	posts  []string
}

func (f *fakeBoxes) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/element") && r.Method == http.MethodPost:
		var body struct{ Value string }
		json.NewDecoder(r.Body).Decode(&body)
		// A lookup names a box by its name — `name == "otpBox3" AND type ==
		// ...` — and the box's number is its element id.
		name := body.Value
		if i := strings.Index(name, `name == "`); i >= 0 {
			name = name[i+len(`name == "`):]
			name = name[:strings.Index(name, `"`)]
		}
		id := "EL" + strings.TrimPrefix(name, "otpBox")
		fmt.Fprintf(w, `{"value":{"element-6066-11e4-a52e-4f735466cecf":%q,"ELEMENT":%q}}`, id, id)
	case strings.HasSuffix(path, "/value") && r.Method == http.MethodPost:
		var body struct{ Text string }
		json.NewDecoder(r.Body).Decode(&body)
		box := f.box(path)
		f.posts = append(f.posts, fmt.Sprintf("%d:%s", box, body.Text))
		for i, ch := range []rune(body.Text) {
			if i == f.loseAt {
				f.loseAt = -1
				continue
			}
			for box < 6 && box == f.stuck {
				box++
			}
			if box < 6 {
				f.vals[box] = string(ch)
				box++
			}
		}
		w.Write([]byte(`{"value":null}`))
	case strings.HasSuffix(path, "/clear"):
		f.vals[f.box(path)] = ""
		w.Write([]byte(`{"value":null}`))
	case strings.HasSuffix(path, "/attribute/value"):
		out, _ := json.Marshal(map[string]string{"value": f.vals[f.box(path)]})
		w.Write(out)
	case strings.HasSuffix(path, "/source"):
		var b strings.Builder
		b.WriteString(`<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="MobiumApp" bundleId="dev.mobium.mobiumapp" x="0" y="0" width="402" height="874" visible="true" enabled="true">`)
		for i, v := range f.vals {
			val := ""
			if v != "" {
				val = fmt.Sprintf(` value="%s"`, v)
			}
			fmt.Fprintf(&b, `<XCUIElementTypeTextField type="XCUIElementTypeTextField" name="otpBox%d"%s x="%d" y="400" width="46" height="54" visible="true" enabled="true" accessible="true"/>`, i, val, 16+i*60)
		}
		b.WriteString(`</XCUIElementTypeApplication>`)
		out, _ := json.Marshal(map[string]string{"value": b.String()})
		w.Write(out)
	default:
		w.Write([]byte(`{"value":null}`))
	}
}

// box is the box an element path names: /session/S1/element/EL3/... is 3.
func (f *fakeBoxes) box(path string) int {
	i := strings.Index(path, "/EL")
	n, _ := strconv.Atoi(strings.SplitN(path[i+3:], "/", 2)[0])
	return n
}

func typeIntoBoxes(t *testing.T, f *fakeBoxes, text string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1}
	_, first := boxes(t, "", "", "", "", "", "")
	return d.SetText(context.Background(), first, text)
}

// The measured loss: typed whole into the first box, a digit is lost as focus
// moves. It is then typed one character to a box, and every box holds its
// digit — and the whole code is never typed again into boxes already filled.
func TestSetTextRetypesALostDigitOneToABox(t *testing.T) {
	f := &fakeBoxes{loseAt: 1, stuck: -1}
	if err := typeIntoBoxes(t, f, "123456"); err != nil {
		t.Fatalf("a recoverable loss failed: %v", err)
	}
	if got := strings.Join(f.vals[:], ""); got != "123456" {
		t.Errorf("the boxes hold %q", got)
	}
	whole := 0
	for _, p := range f.posts {
		if strings.HasSuffix(p, ":123456") {
			whole++
		}
	}
	if whole != 1 {
		t.Errorf("the whole code was typed %d times, want once: %v", whole, f.posts)
	}
	if len(f.posts) != 7 {
		t.Errorf("posts = %v, want the whole code once and then one digit to each box", f.posts)
	}
}

// Nothing lost: nothing typed again.
func TestSetTextLeavesACompleteSpreadAlone(t *testing.T) {
	f := &fakeBoxes{loseAt: -1, stuck: -1}
	if err := typeIntoBoxes(t, f, "123456"); err != nil {
		t.Fatal(err)
	}
	if len(f.posts) != 1 {
		t.Errorf("posts = %v, want the one type", f.posts)
	}
}

// A box that takes nothing even when typed into directly is named, not
// reported typed.
func TestSetTextNamesWhatStillNeverArrived(t *testing.T) {
	f := &fakeBoxes{loseAt: 1, stuck: 3}
	err := typeIntoBoxes(t, f, "123456")
	if err == nil || !strings.Contains(err.Error(), "never arrived") {
		t.Fatalf("a box that never takes its digit was reported as %v", err)
	}
}
