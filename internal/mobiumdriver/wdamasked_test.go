package mobiumdriver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// maskedField is a WebDriverAgent with one field that reads back as bullets,
// as Flutter's obscured field does once it holds text, though it was a plain
// TextField when resolved. drop loses that many characters from each type.
type maskedField struct {
	held string
	drop int
}

func (f *maskedField) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch p := r.URL.Path; {
	case strings.HasSuffix(p, "/element") && r.Method == http.MethodPost:
		w.Write([]byte(`{"value":{"element-6066-11e4-a52e-4f735466cecf":"EL1","ELEMENT":"EL1"}}`))
	case strings.HasSuffix(p, "/value") && r.Method == http.MethodPost:
		var body struct{ Text string }
		json.NewDecoder(r.Body).Decode(&body)
		t := []rune(body.Text)
		if f.drop > 0 && len(t) > f.drop {
			t = t[f.drop:]
		}
		f.held = string(t)
		w.Write([]byte(`{"value":null}`))
	case strings.HasSuffix(p, "/clear"):
		f.held = ""
		w.Write([]byte(`{"value":null}`))
	case strings.HasSuffix(p, "/attribute/value"):
		out, _ := json.Marshal(map[string]string{"value": strings.Repeat("•", len([]rune(f.held)))})
		w.Write(out)
	case strings.HasSuffix(p, "/source"):
		w.Write([]byte(`{"value":"<XCUIElementTypeApplication type=\"XCUIElementTypeApplication\" x=\"0\" y=\"0\" width=\"402\" height=\"874\"/>"}`))
	default:
		w.Write([]byte(`{"value":null}`))
	}
}

func typeIntoMasked(t *testing.T, f *maskedField, text string) (*uitree.Node, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	n := &uitree.Node{Class: "XCUIElementTypeTextField", TestID: "password", Displayed: true, Enabled: true,
		Bounds: uitree.Rect{X1: 16, Y1: 100, X2: 386, Y2: 150}}
	return n, (&WDA{w3c: c, scale: 1}).SetText(context.Background(), n, text)
}

// A field that turns out to be a password is confirmed by its length and
// marked, so the caller does not echo it; one that lost a keystroke is
// reported without the text. CHALLENGES 206.
func TestAFieldThatReadsBackMaskedIsAPassword(t *testing.T) {
	n, err := typeIntoMasked(t, &maskedField{}, "secret12")
	if err != nil {
		t.Fatalf("eight bullets for eight characters was not confirmed: %v", err)
	}
	if !n.Password {
		t.Error("the field was not marked as a password, so the caller would echo it")
	}
	_, err = typeIntoMasked(t, &maskedField{drop: 1}, "secret12")
	if err == nil {
		t.Fatal("a lost keystroke in a masked field was reported as typed")
	}
	if strings.Contains(err.Error(), "secret12") {
		t.Errorf("the error printed the password: %v", err)
	}
}
