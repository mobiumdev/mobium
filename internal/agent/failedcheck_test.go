package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
	"github.com/mobiumdev/mobium/internal/webview"
)

// Every refusal from a check an action makes has one shape, native or web:
// "X failed check C: reason", with C and the reason in details, and C spelled
// as the page spells it — the cover's was receives_events natively and
// receivesEvents in a WebView until they shared this.
func TestCheckRefusalsShareOneShape(t *testing.T) {
	loc := uitree.Locator{Kind: uitree.KindTestID, Value: "submit"}
	cases := []struct {
		check string
		err   error
	}{
		{checkEnabled, notEnabled(loc, 2*time.Second)},
		{checkReceivesEvents, keyboardOver(loc, true)},
		{checkReceivesEvents, dialogOver("Allow?", loc, true)},
		{checkReceivesEvents, webCheckFailed("#go", &webview.Actionability{Status: "failed", Check: "receivesEvents", Reason: "covered"}, time.Second)},
	}
	for _, c := range cases {
		e, ok := mobiumerr.As(c.err)
		if !ok {
			t.Fatalf("%v is not a coded error", c.err)
		}
		if !strings.Contains(e.Message, " failed check "+c.check+": ") {
			t.Errorf("message %q is not in the failed-check shape for %s", e.Message, c.check)
		}
		if e.Details["check"] != c.check {
			t.Errorf("details check = %v, want %s (%q)", e.Details["check"], c.check, e.Message)
		}
		if r, _ := e.Details["reason"].(string); r == "" || !strings.Contains(e.Message, r) {
			t.Errorf("details reason %q is missing or not what the message says (%q)", r, e.Message)
		}
	}
}

// What is not a failed check keeps its own shape: nothing matched at all, and
// the keyboard is only the likely reason.
func TestAMissIsNotAFailedCheck(t *testing.T) {
	loc := uitree.Locator{Kind: uitree.KindTestID, Value: "submit"}
	for _, err := range []error{keyboardOver(loc, false), dialogOver("Allow?", loc, false)} {
		if strings.Contains(err.Error(), "failed check") {
			t.Errorf("a miss read as a failed check: %v", err)
		}
	}
}
