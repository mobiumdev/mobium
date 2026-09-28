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
// The checks are Vibium's, from clicker/internal/api/actionability.go, which
// are Playwright's matrix for a click: visible, enabled and receives events,
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

  let rect = el.getBoundingClientRect();
  const vw = window.innerWidth, vh = window.innerHeight;
  if (rect.top < 0 || rect.left < 0 || rect.bottom > vh || rect.right > vw) {
    el.scrollIntoView({block: 'center', inline: 'center', behavior: 'instant'});
    rect = el.getBoundingClientRect();
  }

  if (rect.width === 0 || rect.height === 0) return fail('visible', 'zero size');
  const style = getComputedStyle(el);
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

  // What is on top at a point, descending through shadow roots as Vibium
  // does: elementFromPoint stops at a shadow host.
  const topAt = (x, y) => {
    let hit = document.elementFromPoint(x, y);
    while (hit && hit.shadowRoot) {
      const inner = hit.shadowRoot.elementFromPoint(x, y);
      if (!inner || inner === hit) break;
      hit = inner;
    }
    return hit;
  };
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

// CheckActionable runs the checks against the index-th element of the map.
func CheckActionable(ctx context.Context, p Page, index int) (*Actionability, error) {
	raw, err := p.Evaluate(ctx, fmt.Sprintf(actionableScript, index))
	if err != nil {
		return nil, err
	}
	var a Actionability
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "could not read the page's actionability answer %q: %w", raw, err)
	}
	return &a, nil
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
  let rect = el.getBoundingClientRect();
  if (rect.top < 0 || rect.left < 0 || rect.bottom > window.innerHeight || rect.right > window.innerWidth) {
    el.scrollIntoView({block: 'center', inline: 'center', behavior: 'instant'});
    rect = el.getBoundingClientRect();
  }
  if (rect.width === 0 || rect.height === 0) return fail('visible', 'zero size');
  const style = getComputedStyle(el);
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
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
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
func Fill(ctx context.Context, p Page, index int, value string, appendText bool) (*Filled, error) {
	quoted, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	raw, err := p.Evaluate(ctx, fmt.Sprintf(fillScript, index, quoted, appendText))
	if err != nil {
		return nil, err
	}
	var f Filled
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "could not read the page's answer to a fill: %w", err)
	}
	return &f, nil
}
