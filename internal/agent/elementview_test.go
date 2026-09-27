package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

func TestElementViewCarriesTheCheckedState(t *testing.T) {
	// map's text said "(checkbox, checked)" and its structured result did
	// not, so no client could read whether a box was ticked.
	on, off := true, false
	for _, c := range []struct {
		checked *bool
		want    string
	}{{&on, `"checked":true`}, {&off, `"checked":false`}, {nil, ""}} {
		b, _ := json.Marshal(elementView(uitree.Entry{Ref: "@e1", Label: "Accept terms", Role: "checkbox", Checked: c.checked}))
		if c.want == "" {
			if strings.Contains(string(b), "checked") {
				t.Errorf("an element with no state carries one: %s", b)
			}
			continue
		}
		if !strings.Contains(string(b), c.want) {
			t.Errorf("want %s in %s", c.want, b)
		}
	}
}
