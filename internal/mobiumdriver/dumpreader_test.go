package mobiumdriver

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumdriver/reader"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// readerADB is an adb on a device whose accessibility settings are given, and
// whose app_process prints what the reader would: a vendor line first, then
// the hierarchy between the markers (or the error line, when fail is set).
func readerADB(t *testing.T, a11yEnabled, services string, fail bool) (*Android, string) {
	t.Helper()
	readerOut := `echo "Init wrapper sys mutex successful. Pid:1029"; echo MOBIUM-READER-BEGIN; echo '` + miniDump + `'; echo MOBIUM-READER-END`
	if fail {
		readerOut = `echo "MOBIUM-READER-ERROR: IllegalStateException: no active window from UiAutomation in 10s"`
	}
	adb, log := fakeADB(t, `case "$*" in
  *"settings get secure accessibility_enabled"*) echo `+a11yEnabled+`; echo "`+services+`" ;;
  *app_process*) `+readerOut+` ;;
  *"uiautomator dump"*) echo "UI hierchary dumped to: /data/local/tmp/mobium-dump.xml" ;;
  *"cat /data/local/tmp/mobium-dump.xml"*) echo '`+miniDump+`' ;;
esac`)
	return NewAndroid(adb), log
}

// With a screen reader enabled, the read goes through Mobium's reader, and the
// tree is the same one uiautomator's dump would have given.
func TestSnapshotUsesTheReaderWhenAScreenReaderIsOn(t *testing.T) {
	t.Setenv("MOBIUM_DUMP_READER", "")
	a, log := readerADB(t, "1", "com.amazon.logan/com.amazon.logan.LoganAccessibilityService", false)
	tree, err := a.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	tree.Walk(func(n *uitree.Node) bool {
		found = found || n.Text == "Hello"
		return true
	})
	if !found {
		t.Fatal("the reader's hierarchy did not parse into the tree")
	}
	got := calls(t, log)
	for _, want := range []string{"push", reader.Dir + "/reader.dex", "app_process /system/bin " + reader.Class, "rm -rf " + reader.Dir} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in the calls:\n%s", want, got)
		}
	}
	if strings.Contains(got, "uiautomator dump") {
		t.Errorf("uiautomator dump ran, which silences the screen reader:\n%s", got)
	}
}

// The positive control: with no accessibility service on, nothing changes.
func TestSnapshotUsesUiautomatorWithNoServiceOn(t *testing.T) {
	t.Setenv("MOBIUM_DUMP_READER", "")
	for _, c := range []struct{ enabled, services string }{{"0", "null"}, {"1", "null"}, {"1", ""}, {"0", "x/y"}} {
		a, log := readerADB(t, c.enabled, c.services, false)
		if _, err := a.Snapshot(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := calls(t, log)
		if strings.Contains(got, "app_process") || !strings.Contains(got, "uiautomator dump") {
			t.Errorf("enabled=%s services=%q: want uiautomator dump, got:\n%s", c.enabled, c.services, got)
		}
	}
}

// A failed read still deletes what it put on the device, and says why.
func TestReaderCleansUpAfterAFailedRead(t *testing.T) {
	t.Setenv("MOBIUM_DUMP_READER", "mobium")
	a, log := readerADB(t, "0", "null", true)
	_, err := a.Snapshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no active window") {
		t.Fatalf("want the reader's reason, got %v", err)
	}
	got := calls(t, log)
	if strings.Count(got, "app_process") != snapshotRetries || strings.Count(got, "rm -rf "+reader.Dir) != snapshotRetries {
		t.Errorf("want %d reads each followed by a cleanup:\n%s", snapshotRetries, got)
	}
}

func TestReaderOverrideIsChecked(t *testing.T) {
	t.Setenv("MOBIUM_DUMP_READER", "talkback")
	a, _ := readerADB(t, "0", "null", false)
	if _, err := a.Snapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "MOBIUM_DUMP_READER") {
		t.Fatalf("an unknown value was accepted: %v", err)
	}
}
