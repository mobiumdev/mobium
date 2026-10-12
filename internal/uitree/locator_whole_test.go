package uitree

import "testing"

// A quoted value is matched whole, in any case: CHALLENGES 233's wait for
// text=Internet was met by "Network & internet" on the screen being left,
// and there was no way to ask for the whole word.
func TestAQuotedValueParsesWhole(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Locator
	}{
		{`text="Internet"`, Locator{Kind: KindText, Value: "Internet", Whole: true}},
		{`"Save"`, Locator{Kind: KindText, Value: "Save", Whole: true}},
		{`label="Allow Paste",role=button`, Locator{Kind: KindLabel, Value: "Allow Paste", Whole: true, Role: "button"}},
		{`text="Reply, then archive"`, Locator{Kind: KindText, Value: "Reply, then archive", Whole: true}},
		{`text="Say \"hi\""`, Locator{Kind: KindText, Value: `Say "hi"`, Whole: true}},
		{`testid="loginBtn"`, Locator{Kind: KindTestID, Value: "loginBtn", Whole: true}},
		{`role="button"`, Locator{Kind: KindRole, Value: "button"}},
		// Unquoted is as it was.
		{`text=Save,role=button`, Locator{Kind: KindText, Value: "Save", Role: "button"}},
		{`Save,role=button`, Locator{Kind: KindText, Value: "Save", Role: "button"}},
		{`text=Don't "Save"`, Locator{Kind: KindText, Value: `Don't "Save"`}},
	} {
		got, err := ParseLocator(c.in)
		if err != nil || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{`text="Internet`, `text="Save"x`, `text="Save",role=nosuchrole`} {
		if _, err := ParseLocator(bad); err == nil {
			t.Errorf("%s parsed", bad)
		}
	}
}

// What a locator prints is what a caller writes to get it back: an exact or
// whole value is quoted, and parses back whole.
func TestALocatorPrintsAsItParses(t *testing.T) {
	for _, l := range []Locator{
		{Kind: KindText, Value: "Internet", Whole: true},
		{Kind: KindLabel, Value: "Card 8", Exact: true},
		{Kind: KindText, Value: `Say "hi", then go`, Whole: true, Role: "button"},
		{Kind: KindTestID, Value: "loginBtn"},
		{Kind: KindPath, Value: "0/1/2"},
	} {
		back, err := ParseLocator(l.String())
		if err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		if back.Value != l.Value || back.Kind != l.Kind || back.Role != l.Role || back.Whole != (l.Whole || l.Exact) {
			t.Errorf("%+v printed %s and parsed back as %+v", l, l, back)
		}
	}
}

func TestAWholeValueMatchesOnlyTheWhole(t *testing.T) {
	tree, err := ParseAndroid([]byte(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" class="android.widget.FrameLayout" text="" bounds="[0,0][1080,2400]" displayed="true" enabled="true">` +
		`<node index="0" class="android.widget.TextView" text="Network &amp; internet" bounds="[0,100][1080,200]" displayed="true" enabled="true"/>` +
		`<node index="1" class="android.widget.Button" text="DISCARD" clickable="true" bounds="[0,300][500,400]" displayed="true" enabled="true"/>` +
		`<node index="2" class="android.widget.TextView" text="  Wi-Fi " bounds="[0,500][1080,600]" displayed="true" enabled="true"/>` +
		`<node index="3" class="android.widget.Button" text="Don’t allow" clickable="true" bounds="[0,700][500,800]" displayed="true" enabled="true"/>` +
		`</node></hierarchy>`))
	if err != nil {
		t.Fatal(err)
	}
	count := func(s string) int {
		l, err := ParseLocator(s)
		if err != nil {
			t.Fatal(err)
		}
		return len(l.Resolve(tree))
	}
	for s, want := range map[string]int{
		`text=Internet`:             1, // a part, as before
		`text="Internet"`:           0, // never "Network & internet"
		`text="network & internet"`: 1,
		`text="Discard"`:            1, // Android draws the caption in capitals
		`text="Disc"`:               0,
		`text="Wi-Fi"`:              1, // the space around it is not the text
	} {
		if got := count(s); got != want {
			t.Errorf("%s matched %d, want %d", s, got, want)
		}
	}
}
