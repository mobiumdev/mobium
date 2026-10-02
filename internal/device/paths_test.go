package device

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// A device path that climbs out is refused, and one without app must be the
// shell's, from the root.
func TestCheckDevicePath(t *testing.T) {
	for _, c := range []struct {
		p    string
		abs  bool
		okay bool
	}{
		{"/sdcard/x", true, true}, {"sdcard/x", true, false}, {"/sdcard/../data", true, false},
		{"files/x.txt", false, true}, {"files/../../x", false, false}, {"", false, false},
	} {
		if err := CheckDevicePath(c.p, c.abs); (err == nil) != c.okay {
			t.Errorf("CheckDevicePath(%q, %v) = %v", c.p, c.abs, err)
		}
	}
}

// With app, a path is relative to the app's data folder, where run-as
// starts, or absolute inside it; anything else is refused.
func TestAppPath(t *testing.T) {
	for in, want := range map[string]string{
		"files/x": "files/x", "./files/x": "files/x",
		"/data/user/0/com.x/files/x": "files/x", "/data/data/com.x/databases/d.db": "databases/d.db",
	} {
		if got, err := appPath("com.x", in); err != nil || got != want {
			t.Errorf("appPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := appPath("com.x", "/data/user/0/com.other/files/x"); err == nil {
		t.Error("a path in another app's data was accepted")
	}
}

// What arrived is held to what was sent, file by file.
func TestTreeDiff(t *testing.T) {
	sent := Tree{"a": 3, "b/c": 5}
	if d := sent.diff(Tree{"a": 3, "b/c": 5}); d != "" {
		t.Errorf("identical trees differ: %s", d)
	}
	if d := sent.diff(Tree{"a": 3}); d == "" {
		t.Error("a missing file was not noticed")
	}
	if d := sent.diff(Tree{"a": 3, "b/c": 4}); d == "" {
		t.Error("a short file was not noticed")
	}
	if d := sent.diff(Tree{"a": 3, "b/c": 5, "extra": 1}); d == "" {
		t.Error("an extra file was not noticed")
	}
}

// A folder goes out as tar and comes back the same; an entry naming a place
// outside the folder is refused, not written.
func TestTarRoundTripAndEscape(t *testing.T) {
	src := t.TempDir()
	_ = os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "a.txt"), []byte("one"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("two22"), 0o644)
	var buf bytes.Buffer
	if err := tarFolder(src, &buf); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := untarInto(&buf, dst); err != nil {
		t.Fatal(err)
	}
	want, _, _ := LocalTree(src)
	got, _, _ := LocalTree(dst)
	if d := want.diff(got); d != "" {
		t.Errorf("the round trip changed the folder: %s", d)
	}

	var evil bytes.Buffer
	tw := tar.NewWriter(&evil)
	_ = tw.WriteHeader(&tar.Header{Name: "../escaped.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	out := filepath.Join(t.TempDir(), "in")
	if err := untarInto(&evil, out); err == nil {
		t.Error("an entry outside the folder was unpacked")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(out), "escaped.txt")); err == nil {
		t.Error("the escaping entry was written")
	}
}
