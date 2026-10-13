package webview

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// cssScript says, for each candidate of the map in this context, whether it
// matches a selector — or "bad" for a selector the page refuses.
const cssScript = `(() => {
  ` + candidatesJS + `
  const sel = %s;
  try { document.querySelector(sel); } catch (e) { return 'bad'; }
  return JSON.stringify(__mobiumCandidates().map((el) => el.matches(sel)));
})()`

// MatchCSS returns the indexes of the map's elements a CSS selector names.
// The selector may be preceded by a frame's path, as map prints one —
// "iframe#f >> button.save" — and then names only elements in that frame.
// Only the map's own elements can match: every web action picks its element
// from that list, so a selector naming anything else names nothing to act
// on. Each context the elements come from — the page, and each cross-origin
// frame (CHALLENGES 229) — is asked once.
//
// When nothing in the frame named matches, elsewhere is what the selector
// matches in the other frames, so a refusal can name the path that reaches
// it: a selector written for a frame's own document, without the frame,
// names nothing on the page.
func MatchCSS(ctx context.Context, p Page, els []Element, selector string) (out, elsewhere []int, err error) {
	path, sel := "", strings.TrimSpace(selector)
	if i := strings.LastIndex(sel, " >> "); i >= 0 {
		path, sel = strings.TrimSpace(sel[:i]), strings.TrimSpace(sel[i+len(" >> "):])
	}
	if sel == "" {
		return nil, nil, mobiumerr.New(mobiumerr.InvalidArgument, "css= needs a selector")
	}
	script := fmt.Sprintf(cssScript, jsString(sel))
	read := func(id int) ([]bool, error) {
		var raw string
		var err error
		if id == 0 {
			raw, err = p.Evaluate(ctx, script)
		} else if c, ok := p.(contextual); ok {
			raw, err = c.evaluateIn(ctx, id, script)
		} else {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if raw == "bad" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%q is not a CSS selector the page accepts", sel)
		}
		var m []bool
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "could not read the page's selector answer %q: %w", raw, err)
		}
		return m, nil
	}
	answers := map[int][]bool{}
	matches := func(e Element) (bool, error) {
		m, ok := answers[e.context]
		if !ok {
			var err error
			if m, err = read(e.context); err != nil {
				return false, err
			}
			answers[e.context] = m
		}
		return e.local < len(m) && m[e.local], nil
	}
	for i, e := range els {
		if e.Frame != path {
			continue
		}
		ok, err := matches(e)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			out = append(out, i)
		}
	}
	if len(out) > 0 {
		return out, nil, nil
	}
	for i, e := range els {
		if e.Frame == path {
			continue
		}
		ok, err := matches(e)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			elsewhere = append(elsewhere, i)
		}
	}
	return nil, elsewhere, nil
}
