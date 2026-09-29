package device

import "testing"

// What `tc qdisc show` printed on the Pixel 7 AVD with 300ms, 1000 kbit/s
// down and 500 up in place — Android's own qdiscs around Mobium's.
const shapedRules = `qdisc noqueue 0: dev lo root refcnt 2
qdisc noqueue 0: dev dummy0 root refcnt 2
qdisc tbf 8007: dev ifb0 root refcnt 2 rate 1Mbit burst 16Kb lat 1.0s
qdisc pfifo_fast 0: dev eth0 root refcnt 2 bands 3 priomap  1 2 2 2 1 2 0 0 1 1 1 1 1 1 1 1
qdisc clsact ffff: dev eth0 parent ffff:fff1
qdisc netem 1: dev wlan0 root refcnt 2 limit 1000 delay 300.0ms
qdisc tbf 10: dev wlan0 parent 1:1 rate 500Kbit burst 16Kb lat 1.0s
qdisc clsact ffff: dev wlan0 parent ffff:fff1
`

// And with nothing of Mobium's: ifb0's default is pfifo_fast, and the clsact
// hooks are Android's traffic accounting.
const bareRules = `qdisc noqueue 0: dev lo root refcnt 2
qdisc pfifo_fast 0: dev ifb0 root refcnt 2 bands 3 priomap  1 2 2 2 1 2 0 0 1 1 1 1 1 1 1 1
qdisc clsact ffff: dev eth0 parent ffff:fff1
qdisc noqueue 0: dev wlan0 root refcnt 2
qdisc clsact ffff: dev wlan0 parent ffff:fff1
`

func TestParseShapingReadsMobiumsRulesOnly(t *testing.T) {
	lat, down, up, iface := parseShaping(shapedRules, "wlan0")
	if lat != 300 || down != 1000 || up != 500 || iface != "wlan0" {
		t.Errorf("shaped: %dms, %d down, %d up, on %q — want 300, 1000, 500 on wlan0", lat, down, up, iface)
	}
	lat, down, up, iface = parseShaping(bareRules, "")
	if lat != 0 || down != 0 || up != 0 || iface != "" {
		t.Errorf("bare: %dms, %d down, %d up, on %q — want nothing", lat, down, up, iface)
	}
	// Traffic leaving by eth0, which has none of it, is not shaped, whatever
	// wlan0 has: the read-back follows the traffic.
	lat, _, up, _ = parseShaping(shapedRules, "eth0")
	if lat != 0 || up != 0 {
		t.Errorf("eth0 read as shaped: %dms, %d up", lat, up)
	}
}

func TestRatesAndDurationsInTheirUnits(t *testing.T) {
	for _, c := range []struct {
		v, unit string
		want    int
	}{{"1", "M", 1000}, {"256", "K", 256}, {"1500", "K", 1500}, {"64000", "", 64}, {"1", "G", 1000000}} {
		if got := rateKbps(c.v, c.unit); got != c.want {
			t.Errorf("rateKbps(%s%s) = %d, want %d", c.v, c.unit, got, c.want)
		}
	}
	for _, c := range []struct {
		v, unit string
		want    int
	}{{"300.0", "ms", 300}, {"1.5", "s", 1500}, {"800", "us", 1}} {
		if got := durationMs(c.v, c.unit); got != c.want {
			t.Errorf("durationMs(%s%s) = %d, want %d", c.v, c.unit, got, c.want)
		}
	}
}
