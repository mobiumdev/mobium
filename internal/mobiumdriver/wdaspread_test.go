package mobiumdriver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// boxes builds an iOS hierarchy of six one-digit fields, as MobiumApp's OTP
// Demo has them, holding the given values; an empty value has no value
// attribute, as WebDriverAgent reports an empty field.
func boxes(t *testing.T, values ...string) (*uitree.Tree, *uitree.Node) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="MobiumApp" bundleId="dev.mobium.mobiumapp" x="0" y="0" width="402" height="874" visible="true" enabled="true">`)
	for i, v := range values {
		val := ""
		if v != "" {
			val = fmt.Sprintf(` value="%s"`, v)
		}
		fmt.Fprintf(&b, `<XCUIElementTypeTextField type="XCUIElementTypeTextField" name="otpBox%d"%s x="%d" y="400" width="46" height="54" visible="true" enabled="true" accessible="true"/>`, i, val, 16+i*60)
	}
	b.WriteString(`</XCUIElementTypeApplication>`)
	tree, err := uitree.ParseIOS([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	var first *uitree.Node
	tree.Walk(func(n *uitree.Node) bool {
		if n.TestID == "otpBox0" {
			first = n
			return false
		}
		return true
	})
	return tree, first
}

func TestFindSpread(t *testing.T) {
	for _, c := range []struct {
		name   string
		values []string
		spread bool
		lost   string
	}{
		// Measured on the iPhone 17 Pro simulator: one attempt at "123456"
		// into the first box left 1, 3, 4, 5, 6 — the 2 lost as focus moved.
		{"measured, one lost", []string{"1", "3", "4", "5", "6", ""}, true, "2"},
		{"every digit arrived", []string{"1", "2", "3", "4", "5", "6"}, true, ""},
		// A single field that dropped a keystroke has nothing after it.
		{"dropped, nothing after", []string{"1235", "", "", "", "", ""}, false, ""},
		// Something unrelated in the next field is not this text.
		{"next field holds other text", []string{"1", "9", "", "", "", ""}, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			tree, target := boxes(t, c.values...)
			sp, ok := findSpread(tree, target, "123456")
			if ok != c.spread {
				t.Fatalf("spread = %v, want %v (%+v)", ok, c.spread, sp)
			}
			if ok && sp.Lost != c.lost {
				t.Errorf("lost %q, want %q", sp.Lost, c.lost)
			}
		})
	}
}

func TestSpreadErrorSaysWhereEachPartWent(t *testing.T) {
	err := spreadError("123456", spread{Parts: []string{"1", "3", "4", "5", "6"}, Lost: "2"})
	for _, want := range []string{`the field took "1"`, `"3", "4", "5", "6"`, `"2" never arrived`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not say %s", err, want)
		}
	}
}
