package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// dialogRule is a declared answer to a dialog: when one is up whose text
// contains When, press the button captioned Press.
//
// A button, not accept or dismiss. Which button each verb presses depends on
// the platform and the kind of dialog — accept on a three-button iOS alert
// pressed Cancel (CHALLENGES 106) — so a rule that meant "save" could only be
// written by naming it. And a caption is compared with both a node's text and
// its label, ignoring case: it is the text on Android, in capitals, and the
// label on iOS, and map prints it the same on both.
type dialogRule struct {
	When  string `json:"when"`
	Press string `json:"press"`
	Hits  int    `json:"hits"`
}

// HandledDialog is one dialog a rule answered during a call.
type HandledDialog struct {
	Dialog string `json:"dialog"`
	Press  string `json:"press"`
}

// maxDialogsPerCall bounds how many dialogs one action answers on its way.
const maxDialogsPerCall = 3

// DialogsView is the result of app_dialogs.
type DialogsView struct {
	Rules  []dialogRule `json:"rules"`
	Device string       `json:"device"`
}

func (h *Handlers) dialogs(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.dialogsOn(s, args)
}

// dialogsOn is app_dialogs once the device is resolved: add a rule, clear
// them, or with neither list them.
func (h *Handlers) dialogsOn(s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	serial := s.dev.Serial
	when, press := stringArg(args, "when"), stringArg(args, "press")
	switch {
	case boolArg(args, "clear"):
		if when != "" || press != "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "clear removes every rule; it takes no when or press")
		}
		delete(h.dialogRules, serial)
		return Result("no dialog rules", DialogsView{Rules: []dialogRule{}, Device: serial}), nil
	case when != "" || press != "":
		if when == "" || press == "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "a dialog rule needs both when (text the dialog "+
				"contains) and press (the caption of the button to press)")
		}
		rules := h.dialogRules[serial]
		replaced := false
		for i := range rules {
			if strings.EqualFold(rules[i].When, when) {
				rules[i].Press, replaced = press, true
			}
		}
		if !replaced {
			rules = append(rules, dialogRule{When: when, Press: press})
		}
		h.dialogRules[serial] = rules
	}
	rules := h.dialogRules[serial]
	if rules == nil {
		rules = []dialogRule{}
	}
	lines := []string{fmt.Sprintf("%d dialog rule(s)", len(rules))}
	for _, r := range rules {
		lines = append(lines, fmt.Sprintf("  when %q: press %q  (answered %d)", r.When, r.Press, r.Hits))
	}
	return Result(strings.Join(lines, "\n"), DialogsView{Rules: rules, Device: serial}), nil
}

// dialogInTheWay reports whether an error is the refusal for a dialog over
// the target, which a rule might answer.
func dialogInTheWay(err error) bool {
	e, ok := mobiumerr.As(err)
	return ok && e.Code == mobiumerr.DeviceNotReady && e.Details["dialog"] != nil
}

// answerByRule answers the dialog on screen if a rule covers it, and reports
// whether one did. The button is found by its caption among the dialog's
// own, and pressed; then the dialog must be gone — a press the dialog
// ignored is reported, not assumed.
func (h *Handlers) answerByRule(ctx context.Context, s *session) (bool, error) {
	rules := h.dialogRules[s.dev.Serial]
	if len(rules) == 0 {
		return false, nil
	}
	text, err := h.dialogText(ctx, s)
	if err != nil || text == "" {
		return false, nil
	}
	var rule *dialogRule
	for i := range rules {
		if strings.Contains(strings.ToLower(text), strings.ToLower(rules[i].When)) {
			rule = &rules[i]
			break
		}
	}
	if rule == nil {
		return false, nil
	}
	title := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])

	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return false, err
	}
	button, captions := buttonByCaption(tree, rule.Press)
	if button == nil {
		return false, mobiumerr.New(mobiumerr.NoSuchElement, "a rule says to press %q on %q, and the dialog's "+
			"buttons are %s", rule.Press, title, quoteAll(captions)).
			WithRemedy("change the rule's press to one of the dialog's buttons (app_dialogs)").
			WithDetail("dialog", title)
	}
	x, y := button.Bounds.Center()
	if err := s.driver.Tap(ctx, x, y); err != nil {
		return false, err
	}
	// Gone, or at least replaced: a button may raise the next dialog.
	deadline := time.Now().Add(3 * time.Second)
	for {
		now, err := h.dialogText(ctx, s)
		if err == nil && now != text {
			break
		}
		if time.Now().After(deadline) {
			return false, mobiumerr.New(mobiumerr.NotConfirmed, "pressed %q on %q, as a rule says, and the "+
				"dialog is still there", rule.Press, title)
		}
		time.Sleep(200 * time.Millisecond)
	}
	rule.Hits++
	h.handled = append(h.handled, HandledDialog{Dialog: title, Press: rule.Press})
	delete(h.refs, s.dev.Serial)
	return true, nil
}

// dialogText is what the dialog on screen says, or "" with none.
// An app's own dialog, which the platform's alert endpoint does not know, is
// read from the hierarchy, so a rule answers a Jetpack Compose dialog too.
func (h *Handlers) dialogText(ctx context.Context, s *session) (string, error) {
	a, ok := mobiumdriver.AsAlerts(s.driver)
	if !ok {
		return "", nil
	}
	text, err := a.AlertText(ctx)
	if errors.Is(err, mobiumdriver.ErrNoAlert) || mobiumerr.CodeOf(err) == mobiumerr.NoSuchAlert {
		tree, serr := s.driver.Snapshot(ctx)
		if serr != nil {
			return "", nil
		}
		return appDialogText(tree), nil
	}
	return text, err
}

// appDialogText is what an app's own dialog says, from the hierarchy — its
// texts in order, title first — or "" when no window floats over the app.
// Only Android's floating window: an iOS alert is the platform's, and its
// endpoint already reads it.
func appDialogText(t *uitree.Tree) string {
	if t == nil || t.Screen.Empty() {
		return ""
	}
	d := t.Dialog()
	if d == nil {
		return ""
	}
	if words := dialogWords(d); words != "" {
		return words
	}
	// Only buttons: still a dialog in the way, with nothing to quote.
	return "an untitled dialog"
}

// dialogWords is what a dialog or popover says — its texts in order, title
// first, one to a line — leaving out its buttons' captions, or "" when it
// has only buttons.
func dialogWords(d *uitree.Node) string {
	var parts []string
	var walk func(n *uitree.Node)
	walk = func(n *uitree.Node) {
		// A button's words are its caption, not what the dialog says: a
		// platform alert's text is its title and message alone.
		if n.Clickable {
			return
		}
		text := strings.TrimSpace(n.Text)
		if masked, ok := uitree.Redact(n); ok {
			text = masked
		}
		if text == "" {
			text = strings.TrimSpace(n.Label)
		}
		if text != "" {
			parts = append(parts, text)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(d)
	return strings.Join(parts, "\n")
}

// popoverNote says that a popover is in front, what it says, and how it
// closes, or "" when none is. A popover is no dialog: the platform's alert
// endpoint does not know it, and Pocket Casts' first-run tip, "Add
// bookmark", has no button to answer it with. But while one is up iOS
// reports everything behind it hidden, so map listed nothing at all on the
// player and app_alert said no dialog was on screen — while the tip was the
// one thing on it. It closes when touched outside it, at a point found as
// the refusal behind it finds one (screenOver). CHALLENGES 235.
//
// Only where there is such a point. Safari's share sheet is a popover too,
// with nothing outside it to touch and its own controls in map; saying it
// closes when touched outside would be a claim with nothing to try.
func popoverNote(t *uitree.Tree) string {
	p := t.Popover()
	if p == nil {
		return ""
	}
	x, y, ok := t.OutsidePoint()
	if !ok {
		return ""
	}
	note := "a popover is in front"
	if words := strings.ReplaceAll(dialogWords(p), "\n", " — "); words != "" {
		note += fmt.Sprintf(": %q", words)
	}
	return note + fmt.Sprintf(" — iOS reports what is behind it hidden until it closes, and it closes when "+
		"touched outside it: app_tap at x %d, y %d is outside it", x, y)
}

// buttonByCaption finds the dialog's button captioned caption, and failing
// that returns every caption it has. The dialog is the tree's Alert or Sheet
// on iOS, where the app beneath stays in the tree; elsewhere the tree is the
// dialog's window alone.
func buttonByCaption(tree *uitree.Tree, caption string) (*uitree.Node, []string) {
	scope := tree.Dialog()
	if scope == nil {
		scope = tree.Root
	}
	want := strings.TrimSpace(caption)
	var captions []string
	// A caption is a button's own text or label, or, where it has neither,
	// the label map gives it from inside: a Jetpack Compose button is a
	// clickable node whose words are on a child (CHALLENGES 177).
	for _, e := range tree.Map() {
		n := e.Node
		if !n.Within(scope) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(n.Text), want) || strings.EqualFold(strings.TrimSpace(n.Label), want) ||
			strings.EqualFold(strings.TrimSpace(e.Label), want) {
			return n, nil
		}
		if e.Label != "" {
			captions = append(captions, e.Label)
		}
	}
	return nil, captions
}

func quoteAll(ss []string) string {
	if len(ss) == 0 {
		return "none that can be pressed"
	}
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(q, ", ")
}

// reportHandled adds the dialogs rules answered during a call to its
// result: a line in the text, and dialogs_handled in the structured half. An
// answer nobody hears about is a click nobody made.
func (h *Handlers) reportHandled(res *ToolsCallResult, err error) (*ToolsCallResult, error) {
	if len(h.handled) == 0 {
		return res, err
	}
	var parts []string
	for _, d := range h.handled {
		parts = append(parts, fmt.Sprintf("%q — pressed %q", d.Dialog, d.Press))
	}
	line := "answered by a dialog rule on the way: " + strings.Join(parts, "; ")
	if err != nil {
		return res, fmt.Errorf("%w (%s)", err, line)
	}
	if res == nil {
		return res, nil
	}
	if len(res.Content) > 0 && res.Content[0].Type == "text" {
		res.Content[0].Text += "\n" + line
	}
	if res.StructuredContent != nil {
		if raw, merr := json.Marshal(res.StructuredContent); merr == nil {
			var m map[string]interface{}
			if json.Unmarshal(raw, &m) == nil {
				m["dialogs_handled"] = h.handled
				res.StructuredContent = m
			}
		}
	}
	return res, nil
}
