package uitree

import (
	"os"
	"testing"
)

func loadFixture(t *testing.T, name string) *Tree {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree, err := ParseAndroid(data)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return tree
}

func TestParseBounds(t *testing.T) {
	tests := []struct {
		in      string
		want    Rect
		wantErr bool
	}{
		{"[0,96][1080,264]", Rect{0, 96, 1080, 264}, false},
		{"[-10,-20][30,40]", Rect{-10, -20, 30, 40}, false},
		{"[0,0][0,0]", Rect{0, 0, 0, 0}, false},
		{"garbage", Rect{}, true},
		{"", Rect{}, true},
	}
	for _, tc := range tests {
		got, err := ParseBounds(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseBounds(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("ParseBounds(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRectGeometry(t *testing.T) {
	r := Rect{48, 860, 1032, 1000}
	if r.Width() != 984 || r.Height() != 140 {
		t.Errorf("size = %dx%d, want 984x140", r.Width(), r.Height())
	}
	x, y := r.Center()
	if x != 540 || y != 930 {
		t.Errorf("center = (%d,%d), want (540,930)", x, y)
	}
	if r.Empty() {
		t.Error("rect with area reported empty")
	}
	if !(Rect{0, 0, 0, 0}).Empty() {
		t.Error("zero rect not reported empty")
	}
}

func TestParseAndroidStructure(t *testing.T) {
	tree := loadFixture(t, "login.xml")

	all := tree.All()
	if len(all) != 15 {
		t.Fatalf("parsed %d nodes, want 15", len(all))
	}

	var submit *Node
	for _, n := range all {
		if n.TestID == "com.example.shop:id/submit" {
			submit = n
		}
	}
	if submit == nil {
		t.Fatal("submit button not found in parsed tree")
	}
	if submit.Text != "Sign In" {
		t.Errorf("submit text = %q, want %q", submit.Text, "Sign In")
	}
	if !submit.Clickable || !submit.Enabled {
		t.Errorf("submit clickable=%v enabled=%v, want both true", submit.Clickable, submit.Enabled)
	}
	if submit.Bounds != (Rect{48, 860, 1032, 1000}) {
		t.Errorf("submit bounds = %v", submit.Bounds)
	}
	if got := submit.ShortClass(); got != "AppCompatButton" {
		t.Errorf("ShortClass = %q", got)
	}
	if got := submit.ShortTestID(); got != "submit" {
		t.Errorf("ShortTestID = %q", got)
	}
	if submit.Path != "0/1/3/0" {
		t.Errorf("path = %q, want 0/1/3/0", submit.Path)
	}
	if submit.Depth != 3 {
		t.Errorf("depth = %d, want 3", submit.Depth)
	}
	if submit.Parent == nil || submit.Parent.TestID != "com.example.shop:id/submit_wrapper" {
		t.Error("submit parent is not the wrapper")
	}
}

func TestParseAndroidBooleanAttributes(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	var password, remember *Node
	for _, n := range tree.All() {
		switch n.TestID {
		case "com.example.shop:id/password":
			password = n
		case "com.example.shop:id/remember":
			remember = n
		}
	}
	if password == nil || !password.Password {
		t.Error("password field did not carry password=true")
	}
	if remember == nil || !remember.Checkable || remember.Checked {
		t.Error("checkbox attributes not parsed")
	}
}

func TestParseAndroidToleratesLeadingJunk(t *testing.T) {
	data, err := os.ReadFile("testdata/login.xml")
	if err != nil {
		t.Fatal(err)
	}
	// Some API levels prefix the dump with a status line.
	tree, err := ParseAndroid(append([]byte("UI hierchary dumped to: /dev/tty\n"), data...))
	if err != nil {
		t.Fatalf("parse with leading junk: %v", err)
	}
	if len(tree.All()) != 15 {
		t.Errorf("parsed %d nodes after junk prefix", len(tree.All()))
	}
}

func TestParseAndroidRejectsGarbage(t *testing.T) {
	if _, err := ParseAndroid([]byte("not xml at all")); err == nil {
		t.Error("expected an error for non-XML input")
	}
}

func TestWalkIsDocumentOrder(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	var texts []string
	tree.Walk(func(n *Node) bool {
		if n.Text != "" {
			texts = append(texts, n.Text)
		}
		return true
	})
	want := []string{"Sign in", "Remember me", "Sign In", "Forgot password?", "Continue", "Continue"}
	if len(texts) != len(want) {
		t.Fatalf("got %v, want %v", texts, want)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Errorf("text[%d] = %q, want %q", i, texts[i], want[i])
		}
	}
}

func TestWalkStopsEarly(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	count := 0
	tree.Walk(func(n *Node) bool {
		count++
		return count < 3
	})
	if count != 3 {
		t.Errorf("walk visited %d nodes after requesting a stop at 3", count)
	}
}
