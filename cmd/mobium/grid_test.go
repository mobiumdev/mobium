package main

import (
	"os"
	"testing"
	"time"
)

// A device belongs to one run at a time, and the lease on the node is what
// says so: exclusive, renewable by its holder, free again once its holder
// stops renewing, and released only by its holder.
func TestLeases(t *testing.T) {
	t.Setenv("MOBIUM_HOME", t.TempDir())

	if got, err := takeLease("emulator-5554", "a"); err != nil || !got.OK {
		t.Fatalf("a free device was not leased: %+v, %v", got, err)
	}
	if got, _ := takeLease("emulator-5554", "b"); got.OK || got.Holder != "a" {
		t.Errorf("a second holder was given a leased device: %+v", got)
	}
	if got, _ := takeLease("emulator-5554", "a"); !got.OK {
		t.Error("the holder could not renew its own lease")
	}
	if live, _ := liveLeases(); live["emulator-5554"] != "a" {
		t.Errorf("live leases %v", live)
	}

	// Lapsed: nobody renewed it for longer than the TTL.
	f, _ := leaseFile("emulator-5554")
	old := time.Now().Add(-2 * leaseTTL)
	os.Chtimes(f, old, old)
	if live, _ := liveLeases(); len(live) != 0 {
		t.Errorf("a lapsed lease is still live: %v", live)
	}
	if got, _ := takeLease("emulator-5554", "b"); !got.OK || got.Holder != "b" {
		t.Errorf("a lapsed lease was not taken over: %+v", got)
	}

	// Only its holder releases it.
	dropLease("emulator-5554", "a")
	if live, _ := liveLeases(); live["emulator-5554"] != "b" {
		t.Error("someone else's release freed the device")
	}
	dropLease("emulator-5554", "b")
	if live, _ := liveLeases(); len(live) != 0 {
		t.Errorf("the holder's release left %v", live)
	}

	// A serial never reaches the filesystem as a path.
	if f, _ := leaseFile("../../etc/x"); f == "" || !startsWith(f, os.Getenv("MOBIUM_HOME")) {
		t.Errorf("a hostile serial became %q", f)
	}
}

func startsWith(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }
