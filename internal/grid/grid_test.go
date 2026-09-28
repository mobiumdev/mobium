package grid

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A lease says how long it has been held, and a renewal does not reset that:
// it moves only the file's time.
func TestALeaseKnowsWhenItStarted(t *testing.T) {
	t.Setenv("MOBIUM_HOME", t.TempDir())
	if a, _ := Take("emulator-5554", "g1"); !a.OK {
		t.Fatal("not leased")
	}
	f := filepath.Join(os.Getenv("MOBIUM_HOME"), "leases", Key("emulator-5554"))
	started := time.Now().Add(-30 * time.Second)
	os.WriteFile(f, []byte("g1\n"+started.UTC().Format(time.RFC3339)), 0o600)
	Take("emulator-5554", "g1") // a renewal
	l := mustLease(t, "emulator-5554")
	if l.Holder != "g1" || time.Since(l.Since) < 29*time.Second {
		t.Errorf("after a renewal the lease reads %+v, started ~30s ago", l)
	}
	if h, ok := Holder("emulator-5554"); !ok || h != "g1" {
		t.Errorf("Holder read %q, %v from a file with a start time in it", h, ok)
	}
}

func mustLease(t *testing.T, serial string) Lease {
	t.Helper()
	all, err := LiveLeases()
	if err != nil {
		t.Fatal(err)
	}
	l, ok := all[Key(serial)]
	if !ok {
		t.Fatalf("no live lease on %s", serial)
	}
	return l
}

// The queue is the runs that touched their note lately, oldest first; a
// note left by a run that stopped lapses by itself.
func TestTheQueue(t *testing.T) {
	t.Setenv("MOBIUM_HOME", t.TempDir())
	Wait("g1", "an android device")
	time.Sleep(1100 * time.Millisecond)
	Wait("g2", "an ios device")
	q, _ := Queue()
	if len(q) != 2 || q[0].Holder != "g1" || q[0].Want != "an android device" {
		t.Fatalf("queue %+v", q)
	}
	old := time.Now().Add(-2 * WaitTTL)
	os.Chtimes(filepath.Join(os.Getenv("MOBIUM_HOME"), "waiting", "g1"), old, old)
	if q, _ := Queue(); len(q) != 1 || q[0].Holder != "g2" {
		t.Errorf("a lapsed note is still queued: %+v", q)
	}
	Unwait("g2")
	if q, _ := Queue(); len(q) != 0 {
		t.Errorf("an unwaited run is still queued: %+v", q)
	}
}
