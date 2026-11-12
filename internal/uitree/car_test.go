package uitree

import (
	"os"
	"strings"
	"testing"
)

// The car-*.xml fixtures are UiAutomator2's /source with enableMultiWindows
// on, from the Android Automotive 15 emulator (1024x768), 2026-11-11: every
// window, top first.

const (
	carBars     = "com.android.systemui"
	carKeyboard = "com.google.android.apps.automotive.inputmethod"
)

func carFixture(t *testing.T, file string) *Tree {
	t.Helper()
	tree := windowedFixture(t, file)
	tree.SystemBars, tree.KeyboardPackage = []string{carBars}, carKeyboard
	return tree
}

// The app grid, under the top bar and over nothing but the bottom bar. Read
// as Quest's windows, the bars were the app in front (CHALLENGES 394).
func TestCarAppGridIsTheAppInFront(t *testing.T) {
	plain := windowedFixture(t, "car-app-grid.xml")
	if pkg := plain.Package(); pkg != carBars {
		t.Fatalf("control: without bars the app in front should be the bars, as on the device; got %q", pkg)
	}
	tree := carFixture(t, "car-app-grid.xml")
	if pkg := tree.Package(); pkg != "com.android.car.carlauncher" {
		t.Errorf("the app in front is %q, want the launcher", pkg)
	}
	got := mapLabels(tree)
	for _, want := range []string{"Settings", "Calendar", "Raise temperature", "Applications"} {
		if _, ok := got[want]; !ok {
			t.Errorf("map has no %q", want)
		}
	}
	// Each tile once: its icon is part of it (CHALLENGES 401).
	if _, ok := got["app_icon"]; ok {
		t.Error("map lists a tile's icon as a control of its own")
	}
	if d := tree.Dialog(); d != nil {
		t.Errorf("the app grid read as a dialog: %s", d.Path)
	}
	if tree.Root.Bounds != tree.Screen || tree.Screen.Empty() {
		t.Errorf("the root's bounds are %v, want the screen %v (CHALLENGES 396)", tree.Root.Bounds, tree.Screen)
	}
}

// Morpho Market's dialog: its window, the bars, and not the app's window
// behind it (CHALLENGES 395).
func TestCarAppDialog(t *testing.T) {
	plain := windowedFixture(t, "car-shop-dialog.xml")
	if plain.Dialog() != nil {
		t.Fatal("control: without bars the dialog should go unseen, as it did on the device")
	}
	tree := carFixture(t, "car-shop-dialog.xml")
	d := tree.Dialog()
	if d == nil {
		t.Fatal("the dialog is not a dialog")
	}
	if words := DialogWords(d); !strings.Contains(words, "Remove Wait For It Mug?") {
		t.Errorf("the dialog says %q", words)
	}
}

// MobiumApp is portrait-locked, and the car letterboxes it to 576x768. It
// keeps the area's height whole, which a dialog does not.
func TestCarLetterboxedAppIsNoDialog(t *testing.T) {
	tree := carFixture(t, "car-mobiumapp-letterboxed.xml")
	if d := tree.Dialog(); d != nil {
		t.Errorf("letterboxed MobiumApp read as a dialog: %s", d.Path)
	}
	if pkg := tree.Package(); pkg != "dev.mobium.mobiumapp" {
		t.Errorf("the app in front is %q", pkg)
	}
}

// The keyboard is a window of its own over the app. It is no dialog, the app
// is still in front, and its keys are not mapped.
func TestCarKeyboard(t *testing.T) {
	unmarked := carFixture(t, "car-shop-keyboard.xml")
	unmarked.KeyboardPackage = ""
	if pkg := unmarked.Package(); pkg != carKeyboard {
		t.Fatalf("control: with the keyboard taken for an app, it should be the app in front; got %q", pkg)
	}
	if _, ok := mapLabels(unmarked)["q"]; !ok {
		t.Fatal("control: with the keyboard taken for an app, its keys should be mapped")
	}
	tree := carFixture(t, "car-shop-keyboard.xml")
	if d := tree.Dialog(); d != nil {
		t.Errorf("the keyboard read as a dialog: %s", d.Path)
	}
	if pkg := tree.Package(); pkg != "dev.mobium.shop" {
		t.Errorf("the app in front is %q", pkg)
	}
	got := mapLabels(tree)
	for _, key := range []string{"q", "w", "Space", "Shift"} {
		if _, ok := got[key]; ok {
			t.Errorf("map lists the key %q", key)
		}
	}
	if _, ok := got["Search"]; !ok {
		t.Errorf("map has no search field: %v", got)
	}
}

// MobiumApp's late alert, quoted from its own nodes (CHALLENGES 398).
func TestCarDialogWords(t *testing.T) {
	tree := carFixture(t, "car-mobiumapp-late-alert.xml")
	d := tree.Dialog()
	if d == nil {
		t.Fatal("the late alert is not a dialog")
	}
	if words := DialogWords(d); words != "Session expiring\nStay signed in?" {
		t.Errorf("the dialog says %q", words)
	}
}

// The car's permission prompt: each button a nameless interceptor over a
// sibling holding its caption (CHALLENGES 399).
func TestCarPermissionPrompt(t *testing.T) {
	tree := carFixture(t, "car-permission-prompt.xml")
	got := mapLabels(tree)
	for _, want := range []string{"Allow", "Don’t allow"} {
		if _, ok := got[want]; !ok {
			t.Errorf("map has no %q: %v", want, got)
		}
	}
	d := tree.Dialog()
	if d == nil {
		t.Fatal("the prompt is not a dialog")
	}
	if words := DialogWords(d); words != "Allow MobiumApp to send you notifications?" {
		t.Errorf("the prompt says %q", words)
	}
}

// car-ui marks a list for the rotary controller with an id in its
// content-desc, which is no name (CHALLENGES 402).
func TestCarUIMarkerIsNoLabel(t *testing.T) {
	raw, err := os.ReadFile("testdata/car-settings.xml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "com.android.car.ui.utils.ROTARY_CONTAINER") {
		t.Fatal("control: the capture should hold the marker")
	}
	tree := carFixture(t, "car-settings.xml")
	tree.Walk(func(n *Node) bool {
		if strings.Contains(n.Label, "ROTARY") {
			t.Errorf("%s keeps the marker as its label", n.Path)
		}
		return true
	})
	for _, e := range tree.Map() {
		if strings.Contains(e.Label, "ROTARY") {
			t.Errorf("map lists %q", e.Label)
		}
	}
}

// The car's back button says it is not clickable, and a tap on it goes back
// (CHALLENGES 404).
func TestCarBackButtonIsMapped(t *testing.T) {
	tree := carFixture(t, "car-mobiumapp-letterboxed.xml")
	e, ok := mapLabels(tree)["Back Button"]
	if !ok {
		t.Fatal("map has no Back Button")
	}
	if e.Node.Clickable {
		t.Fatal("control: the capture should hold a back button that says it is not clickable")
	}
}
