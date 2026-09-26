package uitree

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The captured Aegis screen is the positive control: Android put a real
// typed value in the password field's text, and map printed it until
// CHALLENGES 43. The raw source is the one path that skips the parsers.
func TestRawSourceHidesAPassword(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "aegis-password-uia2.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("PASSWORD-MUST-NOT-APPEAR")) {
		t.Fatal("the fixture no longer holds the value this test hides")
	}
	out, n, err := RedactSource(raw)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("PASSWORD-MUST-NOT-APPEAR")) {
		t.Fatal("the password is in the redacted source")
	}
	// Two password fields, one typed into and one empty and showing its hint
	// (showing-hint="true"); only the typed one holds anything to hide.
	if n != 1 {
		t.Errorf("hid %d fields, want the one that was typed into", n)
	}
	if !bytes.Contains(out, []byte(`text="Please confirm the password"`)) {
		t.Error("the empty field's hint was masked, which says it holds something")
	}
	if !strings.Contains(string(out), `text="`+strings.Repeat("•", len("PASSWORD-MUST-NOT-APPEAR"))+`"`) {
		t.Error("the length was not kept")
	}
	// Everything else is the server's, byte for byte: the source still
	// parses to the same tree, and outside the two tags nothing moved.
	before, _ := ParseAndroid(raw)
	after, err := ParseAndroid(out)
	if err != nil {
		t.Fatalf("the redacted source does not parse: %v", err)
	}
	count := func(t *Tree) (n int) { t.Walk(func(*Node) bool { n++; return true }); return }
	if count(before) != count(after) {
		t.Errorf("%d nodes became %d", count(before), count(after))
	}
	if !bytes.HasPrefix(out, raw[:bytes.Index(raw, []byte("PASSWORD"))-200]) {
		t.Error("bytes before the first password field changed")
	}
}

// iOS names a password field by its element type and puts the value in
// `value`; WebDriverAgent already sends bullets, which are left alone.
func TestRawSourceIOS(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="App">
  <XCUIElementTypeSecureTextField type="XCUIElementTypeSecureTextField" value="hunter2" name="password"/>
  <XCUIElementTypeSecureTextField type="XCUIElementTypeSecureTextField" value="•••••••" name="bullets"/>
  <XCUIElementTypeTextField type="XCUIElementTypeTextField" value="mobium" name="username"/>
</XCUIElementTypeApplication>
`)
	out, n, err := RedactSource(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || bytes.Contains(out, []byte("hunter2")) {
		t.Errorf("hid %d, and the output is %s", n, out)
	}
	if !bytes.Contains(out, []byte(`value="mobium"`)) {
		t.Error("an ordinary field was redacted")
	}
	if !bytes.Contains(out, []byte(`value="•••••••" name="password" />`)) {
		t.Errorf("the tag was not rewritten in place: %s", out)
	}
	clean := []byte(`<a><b text="x"/></a>`)
	if got, n, _ := RedactSource(clean); n != 0 || !bytes.Equal(got, clean) {
		t.Error("a source with no password field changed")
	}
}

// An empty password field is not a hidden password. Aegis's confirm field is
// empty and showing its hint — UiAutomator2 says so with showing-hint — and
// was reported as a 27-character password (CHALLENGES 102). Its first field
// holds what was typed, and must still be counted and hidden.
func TestAnEmptyPasswordFieldIsEmpty(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "aegis-password-uia2.xml"))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := ParseAndroid(raw)
	if err != nil {
		t.Fatal(err)
	}
	var typed, empty *Node
	tree.Walk(func(n *Node) bool {
		switch n.ShortTestID() {
		case "text_password":
			typed = n
		case "text_password_confirm":
			empty = n
		}
		return true
	})
	if typed == nil || empty == nil {
		t.Fatal("the fixture's two password fields were not found")
	}
	if got, _ := Redact(empty); got != "(empty, showing its placeholder)" {
		t.Errorf("the empty field reads %q", got)
	}
	got, _ := Redact(typed)
	if strings.Contains(got, "PASSWORD-MUST-NOT-APPEAR") || !strings.Contains(got, "24 characters") {
		t.Errorf("the typed field reads %q", got)
	}
}

// WebDriverAgent reports an empty secure field's placeholder in plain text,
// and typed text as bullets. Measured with curl against WDA on an iPhone 17
// Pro simulator — not through mobium source, whose redaction had turned the
// placeholder into bullets and made iOS look as if it could not tell.
func TestIOSEmptySecureFieldShowsItsPlaceholder(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="App" x="0" y="0" width="400" height="800">
  <XCUIElementTypeTextField type="XCUIElementTypeTextField" value="username" name="username" label="username" placeholderValue="username" visible="true" x="0" y="0" width="300" height="40"/>
  <XCUIElementTypeSecureTextField type="XCUIElementTypeSecureTextField" value="password" name="password" label="password" placeholderValue="password" visible="true" x="0" y="50" width="300" height="40"/>
  <XCUIElementTypeSecureTextField type="XCUIElementTypeSecureTextField" value="•••••••" name="typed" label="typed" placeholderValue="password" visible="true" x="0" y="100" width="300" height="40"/>
</XCUIElementTypeApplication>`)
	tree, err := ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*Node{}
	tree.Walk(func(n *Node) bool { byID[n.TestID] = n; return true })
	if !byID["username"].ShowingHint {
		t.Error("an ordinary field whose value is its placeholder was not seen as empty")
	}
	if got, _ := Redact(byID["password"]); got != "(empty, showing its placeholder)" {
		t.Errorf("the empty secure field reads %q", got)
	}
	if got, _ := Redact(byID["typed"]); !strings.Contains(got, "7 characters") {
		t.Errorf("seven typed characters read %q", got)
	}
	// And the raw source leaves the placeholder as it is: not a secret, and
	// bullets would say the field holds something.
	out, n, err := RedactSource(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || !bytes.Contains(out, []byte(`value="password" name="password"`)) {
		t.Errorf("the placeholder was masked (%d): %s", n, out)
	}
}
