package mobiumdriver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A one-time code's boxes move focus on as each digit arrives, so the field
// read back after typing at the focus is the next box, empty. That was taken
// for a dropped keystroke and the digit typed again into each box after it:
// "2" into the second of six left them holding 1, 2, 2, 2. The box typed
// into is read instead, and nothing is typed twice; a box that did not take
// the digit is named, not retried. CHALLENGES 332.
func TestWDATypeIntoFocusFollowsFocusThatMovedOn(t *testing.T) {
	for _, tc := range []struct {
		name, landed string
		ok           bool
	}{
		{"took it", "2", true},
		{"lost it", "", false},
	} {
		var mu sync.Mutex
		actives, keys := 0, 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			defer mu.Unlock()
			p := r.URL.Path
			switch {
			case strings.HasSuffix(p, "/element/active"):
				actives++
				if actives == 1 {
					w.Write([]byte(`{"value":{"ELEMENT":"BOX1"}}`))
				} else {
					w.Write([]byte(`{"value":{"ELEMENT":"BOX2"}}`))
				}
			case strings.HasSuffix(p, "/wda/keys"):
				keys++
				w.Write([]byte(`{"value":null}`))
			case strings.HasSuffix(p, "/value") && r.Method == http.MethodPost:
				keys++
				w.Write([]byte(`{"value":null}`))
			case strings.Contains(p, "/BOX1/attribute/name"):
				w.Write([]byte(`{"value":"otpBox1"}`))
			case strings.Contains(p, "/BOX2/attribute/name"):
				w.Write([]byte(`{"value":"otpBox2"}`))
			case strings.Contains(p, "/attribute/type"):
				w.Write([]byte(`{"value":"XCUIElementTypeTextField"}`))
			case strings.Contains(p, "/BOX1/attribute/value"):
				if keys == 0 {
					w.Write([]byte(`{"value":""}`))
				} else {
					w.Write([]byte(`{"value":"` + tc.landed + `"}`))
				}
			case strings.Contains(p, "/BOX2/attribute/value"):
				w.Write([]byte(`{"value":""}`))
			default:
				w.Write([]byte(`{"value":null}`))
			}
		}))
		c := newW3CClient(5 * time.Second)
		c.setBase(srv.URL)
		c.sessionID = "S1"
		d := &WDA{w3c: c, scale: 1}

		f, err := d.TypeIntoFocus(context.Background(), "2")
		srv.Close()
		if tc.ok {
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if f.ID != "otpBox1" || f.Value != "2" || f.MovedTo != "otpBox2" {
				t.Errorf("%s: reported %+v", tc.name, f)
			}
		} else {
			if mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed || !strings.Contains(err.Error(), "moved focus to otpBox2") ||
				strings.Contains(err.Error(), "dropped") {
				t.Errorf("%s: %v", tc.name, err)
			}
		}
		if keys != 1 {
			t.Errorf("%s: typed %d times, want once", tc.name, keys)
		}
	}
}

// On Android the read just after a digit found focus between the boxes,
// nowhere, and the type was reported unconfirmed with the digit in its box.
// The box typed into is read instead, and where focus landed is named.
// CHALLENGES 332.
func TestUIA2TypeIntoFocusFollowsFocusThatMovedOn(t *testing.T) {
	var mu sync.Mutex
	actives, keys := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/element/active"):
			actives++
			switch actives {
			case 1:
				w.Write([]byte(`{"value":{"ELEMENT":"BOX1"}}`))
			case 2:
				// In transit: the active element no longer says it is focused.
				w.Write([]byte(`{"value":{"ELEMENT":"BOX1"}}`))
			default:
				w.Write([]byte(`{"value":{"ELEMENT":"BOX2"}}`))
			}
		case strings.HasSuffix(p, "/keys"):
			keys++
			w.Write([]byte(`{"value":null}`))
		case strings.Contains(p, "/BOX1/attribute/focused"):
			if actives == 1 {
				w.Write([]byte(`{"value":"true"}`))
			} else {
				w.Write([]byte(`{"value":"false"}`))
			}
		case strings.Contains(p, "/attribute/focused"):
			w.Write([]byte(`{"value":"true"}`))
		case strings.Contains(p, "/BOX1/attribute/resource-id"):
			w.Write([]byte(`{"value":"otpBox1"}`))
		case strings.Contains(p, "/BOX2/attribute/resource-id"):
			w.Write([]byte(`{"value":"otpBox2"}`))
		case strings.Contains(p, "/BOX1/text"):
			if keys == 0 {
				w.Write([]byte(`{"value":""}`))
			} else {
				w.Write([]byte(`{"value":"2"}`))
			}
		case strings.HasSuffix(p, "/text"):
			w.Write([]byte(`{"value":""}`))
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	defer srv.Close()
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	u := &UIA2{w3c: c}

	f, err := u.TypeIntoFocus(context.Background(), "2")
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "otpBox1" || f.Value != "2" || f.MovedTo != "otpBox2" {
		t.Errorf("reported %+v", f)
	}
	if keys != 1 {
		t.Errorf("typed %d times, want once", keys)
	}
}
