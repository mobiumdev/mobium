package webview

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Actionability is what a page says about one element just before a touch:
// whether it can be touched, and if so where.
//
// The checks are Vibium's, from clicker/internal/api/actionability.go: for a
// click, visible, enabled and receives events,
// with stability compared across two calls by the caller. Two things are
// Mobium's. The element is scrolled into view first — measured on MobiumApp's
// Actionability page, a target below the fold was tapped at y=6221 on a
// 2400-pixel screen and reached nothing — and a covered center is aimed
// around, at the clear point nearest it, as native targets are (CHALLENGES
// 115), because a page can say exactly what is over a point: unlike a native
// tree, it hit-tests.
type Actionability struct {
	// Status is "ok", "not_found" or "failed"; Check and Reason say which
	// check failed and why.
	Status string `json:"status"`
	Check  string `json:"check,omitempty"`
	Reason string `json:"reason,omitempty"`
	// The element's rectangle and the point to touch, in CSS pixels of the
	// viewport, after any scroll.
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	W  float64 `json:"w"`
	H  float64 `json:"h"`
	PX float64 `json:"px"`
	PY float64 `json:"py"`
	// Moved says the center was covered and PX, PY is a clear point instead;
	// Cover names what was over the center.
	Moved bool   `json:"moved,omitempty"`
	Cover string `json:"cover,omitempty"`
}

// actionableScript checks the element at index among the map's candidates.
const actionableScript = `(() => {
  ` + candidatesJS + `
  const el = __mobiumCandidates()[%d];
  if (!el) return JSON.stringify({status:'not_found'});
  const fail = (check, reason) => JSON.stringify({status:'failed', check, reason});

  let rect = __mobiumRect(el);
  const vw = window.innerWidth, vh = window.innerHeight;
  if (rect.top < 0 || rect.left < 0 || rect.bottom > vh || rect.right > vw) {
    el.scrollIntoView({block: 'center', inline: 'center', behavior: 'instant'});
    rect = __mobiumRect(el);
  }

  if (rect.width === 0 || rect.height === 0) return fail('visible', 'zero size');
  const style = el.ownerDocument.defaultView.getComputedStyle(el);
  if (style.visibility === 'hidden') return fail('visible', 'visibility: hidden');
  if (style.display === 'none') return fail('visible', 'display: none');

  if (el.disabled === true) return fail('enabled', 'disabled attribute');
  if (el.getAttribute('aria-disabled') === 'true') return fail('enabled', 'aria-disabled');
  const fs = el.closest('fieldset[disabled]');
  if (fs) {
    const legend = fs.querySelector('legend');
    if (!legend || !legend.contains(el)) return fail('enabled', 'inside a disabled fieldset');
  }

  // The part of the element inside the viewport, where a touch can land.
  const il = Math.max(0, rect.x), it = Math.max(0, rect.y);
  const ir = Math.min(vw, rect.x + rect.width), ib = Math.min(vh, rect.y + rect.height);
  if (ir <= il || ib <= it) return fail('visible', 'outside the viewport');

  // What is on top at a point, through shadow roots and frames.
  const topAt = __mobiumTopAt;
  const reaches = (x, y) => { const h = topAt(x, y); return !!h && (h === el || el.contains(h)); };
  const name = (n) => ((n.innerText || n.getAttribute('aria-label') || '').trim().replace(/\s+/g, ' ').slice(0, 40)) ||
    n.tagName.toLowerCase();

  let px = (il + ir) / 2, py = (it + ib) / 2, moved = false, cover = '';
  if (!reaches(px, py)) {
    const over = topAt(px, py);
    cover = over ? name(over) : 'nothing on the page';
    let best = null, bestD = -1;
    const cols = 9, rows = 5;
    for (let r = 0; r < rows; r++) {
      for (let c = 0; c < cols; c++) {
        const x = il + (2 * c + 1) * (ir - il) / (2 * cols), y = it + (2 * r + 1) * (ib - it) / (2 * rows);
        if (!reaches(x, y)) continue;
        const d = (x - px) * (x - px) + (y - py) * (y - py);
        if (bestD < 0 || d < bestD) { best = [x, y]; bestD = d; }
      }
    }
    if (!best) return fail('receivesEvents', 'covered by "' + cover + '"');
    [px, py] = best; moved = true;
  }
  return JSON.stringify({status: 'ok', x: rect.x, y: rect.y, w: rect.width, h: rect.height, px, py, moved, cover});
})()`

// CheckActionable runs the checks against an element of the map.
//
// One in a cross-origin frame is checked in its frame's own context, after
// the page has brought the frame into view, and its rectangle and point come
// back in the frame's viewport, so the frame's place in the page is added.
// The frame's script cannot see what the page draws over the frame, so the
// page is asked too: the point must land on the frame. CHALLENGES 229.
func CheckActionable(ctx context.Context, p Page, e Element) (*Actionability, error) {
	if e.context == 0 {
		return checkIn(ctx, p, e.local)
	}
	c, ok := p.(contextual)
	if !ok {
		return &Actionability{Status: "not_found"}, nil
	}
	if _, err := frameBox(ctx, p, e.box, true); err != nil {
		return nil, err
	}
	a, err := checkIn(ctx, evalIn{c, e.context}, e.local)
	if err != nil || a.Status != "ok" {
		return a, err
	}
	off, err := frameBox(ctx, p, e.box, false)
	if err != nil {
		return nil, err
	}
	if off == nil {
		return &Actionability{Status: "not_found"}, nil
	}
	a.X, a.Y, a.PX, a.PY = a.X+off.X, a.Y+off.Y, a.PX+off.X, a.PY+off.Y
	over, err := p.Evaluate(ctx, fmt.Sprintf(frameHitScript, jsString(e.box), a.PX, a.PY))
	if err != nil {
		return nil, err
	}
	if over != "frame" {
		return &Actionability{Status: "failed", Check: "receivesEvents",
			Reason: "the page draws " + over + " over the frame it is in, at that point"}, nil
	}
	return a, nil
}

func checkIn(ctx context.Context, e evaluator, index int) (*Actionability, error) {
	raw, err := e.Evaluate(ctx, fmt.Sprintf(actionableScript, index))
	if err != nil {
		return nil, err
	}
	var a Actionability
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "could not read the page's actionability answer %q: %w", raw, err)
	}
	return &a, nil
}

// evalIn is an evaluator bound to one frame's context.
type evalIn struct {
	c  contextual
	id int
}

func (e evalIn) Evaluate(ctx context.Context, expression string) (string, error) {
	return e.c.evaluateIn(ctx, e.id, expression)
}

// frameBoxScript is the top-left of a frame's content in the page's
// viewport, after scrolling the frame into view when asked to.
const frameBoxScript = `(() => {
  ` + candidatesJS + `
  ` + frameAtJS + `
  const f = __mobiumFrameAt(%s);
  if (!f) return 'null';
  let r = __mobiumRect(f);
  if (%t && (r.top < 0 || r.left < 0 || r.bottom > innerHeight || r.right > innerWidth)) {
    f.scrollIntoView({block: 'center', inline: 'center', behavior: 'instant'});
    r = __mobiumRect(f);
  }
  return JSON.stringify({x: r.x + f.clientLeft, y: r.y + f.clientTop});
})()`

type offset struct{ X, Y float64 }

func frameBox(ctx context.Context, p evaluator, path string, scroll bool) (*offset, error) {
	raw, err := p.Evaluate(ctx, fmt.Sprintf(frameBoxScript, jsString(path), scroll))
	if err != nil || raw == "null" {
		return nil, err
	}
	var o offset
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return nil, fmt.Errorf("parse frame position %q: %w", raw, err)
	}
	return &o, nil
}

// frameHitScript says whether a point of the page lands on a frame — the
// frame itself, which is what the page's hit test reaches for a frame it
// cannot see into — or names what is there instead.
const frameHitScript = `(() => {
  ` + candidatesJS + `
  ` + frameAtJS + `
  const f = __mobiumFrameAt(%s);
  const h = __mobiumTopAt(%f, %f);
  if (h && h === f) return 'frame';
  if (!h) return 'nothing';
  return '"' + ((h.innerText || h.getAttribute('aria-label') || '').trim().replace(/\s+/g, ' ').slice(0, 40) ||
    h.tagName.toLowerCase()) + '"';
})()`

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Filled is what a page says after text was put into one of its fields.
type Filled struct {
	Status string `json:"status"`
	Check  string `json:"check,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Matches says the field now holds exactly the text; Password says it is
	// a password field, whose value is never sent back — only whether it
	// matches.
	Matches  bool `json:"matches"`
	Password bool `json:"password"`
}

// fillScript is Vibium's fill: the checks a fill needs — visible, enabled,
// editable — then the value set through the element type's own native setter,
// which is what a framework's controlled input listens to, and the input and
// change events a person's typing would have caused. Then read back.
const fillScript = `(() => {
  ` + candidatesJS + `
  const el = __mobiumCandidates()[%d];
  const text = %s;
  const append = %t;
  if (!el) return JSON.stringify({status:'not_found'});
  const fail = (check, reason) => JSON.stringify({status:'failed', check, reason});
  let rect = __mobiumRect(el);
  if (rect.top < 0 || rect.left < 0 || rect.bottom > window.innerHeight || rect.right > window.innerWidth) {
    el.scrollIntoView({block: 'center', inline: 'center', behavior: 'instant'});
    rect = __mobiumRect(el);
  }
  if (rect.width === 0 || rect.height === 0) return fail('visible', 'zero size');
  const win = el.ownerDocument.defaultView;
  const style = win.getComputedStyle(el);
  if (style.visibility === 'hidden' || style.display === 'none') return fail('visible', 'hidden');
  if (el.disabled === true) return fail('enabled', 'disabled attribute');
  if (el.getAttribute('aria-disabled') === 'true') return fail('enabled', 'aria-disabled');
  if (el.readOnly === true) return fail('editable', 'readonly attribute');
  if (el.getAttribute('aria-readonly') === 'true') return fail('editable', 'aria-readonly');
  const tag = el.tagName.toLowerCase();
  if (tag === 'input') {
    const t = (el.type || 'text').toLowerCase();
    if (!` + fillableInputTypesJS + `.includes(t)) return fail('editable', 'input type ' + t + ' is not a text field');
  } else if (tag !== 'textarea' && !el.isContentEditable) {
    return fail('editable', 'not a text field');
  }
  el.focus();
  const editable = el.isContentEditable && tag !== 'input' && tag !== 'textarea';
  const value = append ? (editable ? el.textContent : el.value) + text : text;
  if (editable) {
    el.textContent = value;
  } else {
    // The element's own window: a frame's elements are not instances of the
    // top page's HTMLTextAreaElement, and its setter would not apply.
    const proto = el instanceof win.HTMLTextAreaElement ? win.HTMLTextAreaElement.prototype : win.HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
    setter.call(el, value);
  }
  el.dispatchEvent(new Event('input', {bubbles: true}));
  el.dispatchEvent(new Event('change', {bubbles: true}));
  const now = editable ? el.textContent : el.value;
  return JSON.stringify({status: 'ok', matches: now === value, password: tag === 'input' && el.type === 'password'});
})()`

// fillableInputTypesJS is Vibium's list of the input types a fill can set:
// text-like inputs and the value-bearing pickers, not checkboxes, radios,
// files or buttons.
const fillableInputTypesJS = `['text','password','email','number','search','tel','url',` +
	`'range','color','date','time','datetime-local','month','week']`

// Fill puts text into the index-th element of the map, after the page says it
// can take it, and reports whether it now holds exactly that — the text alone,
// or with appendText what it held followed by the text, which is app_type's
// meaning where Fill without it is app_fill's.
//
// An element in a cross-origin frame is filled in its frame's context.
func Fill(ctx context.Context, p Page, el Element, value string, appendText bool) (*Filled, error) {
	quoted, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var in evaluator = p
	if el.context != 0 {
		c, ok := p.(contextual)
		if !ok {
			return &Filled{Status: "not_found"}, nil
		}
		in = evalIn{c, el.context}
	}
	raw, err := in.Evaluate(ctx, fmt.Sprintf(fillScript, el.local, quoted, appendText))
	if err != nil {
		return nil, err
	}
	var f Filled
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "could not read the page's answer to a fill: %w", err)
	}
	return &f, nil
}
