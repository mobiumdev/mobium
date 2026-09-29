package mobiumdriver

import (
	"fmt"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// spread is what a type into one field left across it and the fields after
// it, when the app moved focus on as the text arrived — a one-time-code box
// that advances on every digit.
type spread struct {
	// Parts are the target's value and then each following field's, up to
	// the first that took nothing.
	Parts []string
	// Lost are the characters of the text found in none of them.
	Lost string
}

// Complete says every character arrived, in order, across the fields.
func (s spread) Complete() bool { return s.Lost == "" }

// findSpread reads, in a tree taken after typing, whether the text went on
// past the target. It did when the field right after the target holds
// something, and the target's value followed by the next fields' values is
// the text in order with at most some characters missing. A single field
// that dropped a keystroke has nothing after it holding the rest, and is not
// a spread.
func findSpread(tree *uitree.Tree, target *uitree.Node, text string) (spread, bool) {
	var fields []*uitree.Node
	at := -1
	tree.Walk(func(n *uitree.Node) bool {
		if !isTextEntry(n) {
			return true
		}
		if at < 0 && sameNode(n, target) {
			at = len(fields)
		}
		fields = append(fields, n)
		return true
	})
	if at < 0 || at+1 >= len(fields) {
		return spread{}, false
	}
	parts := []string{fieldValue(fields[at])}
	for _, f := range fields[at+1:] {
		v := fieldValue(f)
		if v == "" {
			break
		}
		parts = append(parts, v)
	}
	if len(parts) < 2 {
		return spread{}, false
	}
	lost, ok := missing(text, strings.Join(parts, ""))
	if !ok {
		return spread{}, false
	}
	return spread{Parts: parts, Lost: lost}, true
}

// missing returns the characters of want absent from got, when got is want
// with some characters left out and nothing added or reordered.
func missing(want, got string) (string, bool) {
	g := []rune(got)
	var lost []rune
	j := 0
	for _, r := range want {
		if j < len(g) && g[j] == r {
			j++
			continue
		}
		lost = append(lost, r)
	}
	return string(lost), j == len(g)
}

func isTextEntry(n *uitree.Node) bool {
	switch n.Class {
	case "XCUIElementTypeTextField", "XCUIElementTypeSecureTextField", "XCUIElementTypeTextView":
		return true
	}
	return false
}

func sameNode(a, b *uitree.Node) bool {
	if b.TestID != "" {
		return a.TestID == b.TestID
	}
	return a.Path == b.Path
}

// fieldValue is what a field holds, not its placeholder.
func fieldValue(n *uitree.Node) string {
	if n.ShowingHint {
		return ""
	}
	return n.Text
}

// spreadError says where a spread text went, and what never arrived.
func spreadError(text string, s spread) error {
	return mobiumerr.New(mobiumerr.NotConfirmed,
		"typed %q, and the app moved focus on as it arrived: the field took %q and the fields after it %s — "+
			"%q never arrived, lost as focus moved", text, s.Parts[0], quoteAll(s.Parts[1:]), s.Lost).
		WithRemedy("a field that advances by itself takes one character at a time: type into each field in turn")
}

func quoteAll(parts []string) string {
	q := make([]string, len(parts))
	for i, p := range parts {
		q[i] = fmt.Sprintf("%q", p)
	}
	return strings.Join(q, ", ")
}
