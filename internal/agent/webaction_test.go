package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/webview"
)

// A web target that stays untouchable is refused in Vibium's shape — which
// check, and why — with a code that says what kind of failure it is.
func TestWebCheckRefusals(t *testing.T) {
	for _, c := range []struct {
		check, reason string
		code          mobiumerr.Code
	}{
		{"enabled", "disabled attribute", mobiumerr.Timeout},
		{"stable", "the element is moving or resizing", mobiumerr.Timeout},
		{"receivesEvents", `covered by "full cover"`, mobiumerr.ElementNotReachable},
		{"visible", "outside the viewport", mobiumerr.ElementNotReachable},
	} {
		err := webCheckFailed("@e7", &webview.Actionability{Status: "failed", Check: c.check, Reason: c.reason}, 2*time.Second)
		if mobiumerr.CodeOf(err) != c.code {
			t.Errorf("%s: code %s, want %s", c.check, mobiumerr.CodeOf(err), c.code)
		}
		if !strings.Contains(err.Error(), "@e7 failed check "+c.check+": "+c.reason) {
			t.Errorf("%s: message %q", c.check, err.Error())
		}
		var me *mobiumerr.Error
		if !errorsAs(err, &me) || me.Details["check"] != c.check {
			t.Errorf("%s: details do not carry the check", c.check)
		}
	}
}
