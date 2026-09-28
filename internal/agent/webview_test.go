package agent

import (
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/webview"
)

// A tab opening renumbered every page after it, so a name read from one
// listing attached another page in the next — seen with Safari. Names now
// belong to pages. CHALLENGES 137.
func TestAPageKeepsItsContextNameWhileItIsListed(t *testing.T) {
	page := func(socket, target, url string) webview.Context {
		return webview.Context{ID: "positional", Base: "WEBVIEW_com.android.chrome", Socket: socket, TargetID: target, URL: url}
	}
	names := func(cs []webview.Context) string {
		var out []string
		for _, c := range cs {
			out = append(out, c.URL+"="+c.ID)
		}
		return strings.Join(out, " ")
	}
	s := &session{}
	first := []webview.Context{page("chrome", "A", "a"), page("chrome", "B", "b")}
	s.nameContexts(first)
	if got := names(first); got != "a=WEBVIEW_com.android.chrome b=WEBVIEW_com.android.chrome_1" {
		t.Fatalf("first listing: %s", got)
	}
	// A new tab listed first, as Chrome lists the newest: the others keep
	// their names, and it takes the next free one.
	second := []webview.Context{page("chrome", "C", "c"), page("chrome", "A", "a"), page("chrome", "B", "b")}
	s.nameContexts(second)
	if got := names(second); got != "c=WEBVIEW_com.android.chrome_2 a=WEBVIEW_com.android.chrome b=WEBVIEW_com.android.chrome_1" {
		t.Errorf("after a tab opened: %s", got)
	}
	// A relaunched app is a new socket: its page comes back under the name
	// the gone page had, not as the next number.
	third := []webview.Context{page("chrome-2", "D", "d")}
	s.nameContexts(third)
	if got := names(third); got != "d=WEBVIEW_com.android.chrome" {
		t.Errorf("after a relaunch: %s", got)
	}
}
