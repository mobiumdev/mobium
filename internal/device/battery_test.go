package device

import "testing"

// As the Pixel 7 AVD printed it after `power capacity 37`, `power ac off`
// and `power status discharging`, abbreviated.
func TestParseBattery(t *testing.T) {
	dump := `Current Battery Service state:
  AC powered: false
  USB powered: false
  Wireless powered: false
  Dock powered: false
  status: 3
  health: 2
  present: true
  level: 37
  scale: 100
  voltage: 5000
`
	b, err := parseBattery(dump)
	if err != nil || b != (Battery{Present: true, Level: 37, State: "discharging", Plugged: "none"}) {
		t.Errorf("got %+v, %v", b, err)
	}
	b, err = parseBattery("  AC powered: true\n  status: 5\n  present: true\n  level: 100\n  scale: 100\n")
	if err != nil || b.State != "full" || b.Plugged != "ac" {
		t.Errorf("full on AC: %+v, %v", b, err)
	}
	if _, err := parseBattery("no battery service"); err == nil {
		t.Error("a dump with no level was read as a battery")
	}
}

func TestParseDeviceDate(t *testing.T) {
	got, err := parseDeviceDate("1790625653.233458676 -0700")
	if err != nil {
		t.Fatal(err)
	}
	if got.Unix() != 1790625653 || got.Nanosecond() != 233458676 {
		t.Errorf("instant %v", got)
	}
	if _, off := got.Zone(); off != -7*3600 {
		t.Errorf("offset %d", off)
	}
	if got.Format("2006-01-02T15:04:05Z07:00") != "2026-09-28T13:00:53-07:00" {
		t.Errorf("formatted %s", got.Format("2006-01-02T15:04:05Z07:00"))
	}
	if _, err := parseDeviceDate("Mon Sep 28"); err == nil {
		t.Error("a date with no epoch was read")
	}
}
