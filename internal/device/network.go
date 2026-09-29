package device

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// NetworkConditions are Android's network conditions: offline by airplane mode, which a real phone
// takes too, and latency and bandwidth by Linux traffic control, which needs
// root and so an emulator.
//
// Not the emulator console's `network speed` and `network delay`, though
// they look made for it. On emulator 37.1.11 with the Pixel 7 AVD (API 35)
// both were accepted and read back by `network status`, and changed nothing
// measured: 2MB from the Mac at a 1000 kbit/s limit took 1.1s where 16s was
// due, a 500ms delay left a ping at 1ms and a TCP connect to the internet at
// 490ms, over Wi-Fi and over emulated mobile data alike. Given at launch
// (-netdelay) a delay did slow outside traffic, and then the console could
// take it away but not put it back. `tc` on the interface the traffic leaves
// by shapes every packet: a 500ms netem delay made that ping 502ms, and a
// 1 Mbit/s token bucket on the redirected incoming traffic made the same 2MB
// take 17.7s.
type NetworkConditions struct {
	// Airplane is airplane mode as the device reports it.
	Airplane bool `json:"airplane"`
	// Online says whether the device has a default network — what an app
	// asking "am I connected" is told. Airplane mode normally ends it, but
	// Wi-Fi left on in airplane mode keeps it, so it is read, not inferred.
	Online bool `json:"online"`
	// Interface is the one traffic leaves by, and the one shaped.
	Interface string `json:"interface,omitempty"`
	// LatencyMs is added to every round trip; zero is none.
	LatencyMs int `json:"latency_ms"`
	// DownloadKbps and UploadKbps are rate limits in kilobits a second;
	// zero is no limit.
	DownloadKbps int `json:"download_kbps"`
	UploadKbps   int `json:"upload_kbps"`
}

// Shaped says whether any latency or rate limit is in place.
func (c NetworkConditions) Shaped() bool {
	return c.LatencyMs > 0 || c.DownloadKbps > 0 || c.UploadKbps > 0
}

// Every rule Mobium adds carries these, so it removes its own and nothing
// else: Android keeps its own clsact hook on the interface for traffic
// accounting, and it must survive a reset.
const (
	shapeRootHandle = "1:"
	shapeRateHandle = "10:"
	shapeFilterPref = "49000"
	shapeIngressDev = "ifb0"
)

var ifaceRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,14}$`)

// defaultInterface names the interface the default route leaves by, or ""
// with no route — offline, say.
func (a *ADB) defaultInterface(ctx context.Context) string {
	out, err := a.Shell(ctx, "ip", "route", "get", "8.8.8.8")
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "dev" && ifaceRe.MatchString(f[i+1]) {
			return f[i+1]
		}
	}
	return ""
}

var (
	netemDelayRe = regexp.MustCompile(`qdisc netem 1: dev (\S+) root .*?delay ([0-9.]+)(us|ms|s)\b`)
	tbfRateRe    = regexp.MustCompile(`qdisc tbf \S+ dev (\S+) (?:root|parent \S+) .*?rate ([0-9.]+)([KMG]?)bit`)
)

// NetworkConditions reads what is in place: airplane mode and the default
// network from the connectivity service, and the shaping from the rules
// themselves, which the shell user can read without root.
func (a *ADB) NetworkConditions(ctx context.Context) (NetworkConditions, error) {
	var c NetworkConditions
	out, err := a.Shell(ctx, "cmd", "connectivity", "airplane-mode")
	if err != nil {
		return c, err
	}
	switch strings.TrimSpace(string(out)) {
	case "enabled":
		c.Airplane = true
	case "disabled":
	default:
		return c, mobiumerr.New(mobiumerr.DeviceServer, "the device did not say whether airplane mode is on: %q",
			strings.TrimSpace(string(out)))
	}
	if c.Online, err = a.online(ctx); err != nil {
		return c, err
	}
	c.Interface = a.defaultInterface(ctx)

	rules, err := a.Shell(ctx, "tc", "qdisc", "show")
	if err != nil {
		return c, err
	}
	c.LatencyMs, c.DownloadKbps, c.UploadKbps, c.Interface = parseShaping(string(rules), c.Interface)
	// The download limit applies only while the filter feeding it is there,
	// and airplane mode removes that filter while the limit stays: read back
	// from the limit alone, a device going offline and back reported a
	// download limit nothing was applying, measured on the emulator.
	if c.DownloadKbps > 0 && !a.redirecting(ctx, c.Interface) {
		c.DownloadKbps = 0
	}
	return c, nil
}

// redirecting reports whether the interface still sends its incoming traffic
// to the download limit.
func (a *ADB) redirecting(ctx context.Context, iface string) bool {
	if !ifaceRe.MatchString(iface) {
		return false
	}
	out, err := a.Shell(ctx, "tc", "filter", "show", "dev", iface, "ingress")
	return err == nil && strings.Contains(string(out), "pref "+shapeFilterPref+" ")
}

// parseShaping reads Mobium's rules out of `tc qdisc show`: the netem delay
// at handle 1:, the token bucket under it (upload) and the one on ifb0
// (download), and the interface the delay is on. Android's own qdiscs —
// noqueue, pfifo_fast, clsact — are not Mobium's and are skipped.
//
// on names the interface to read the delay and upload limit from — the one
// traffic leaves by; "" takes the first that has them, and says which.
func parseShaping(rules, on string) (latencyMs, downloadKbps, uploadKbps int, iface string) {
	iface = on
	for _, line := range strings.Split(rules, "\n") {
		if m := netemDelayRe.FindStringSubmatch(line); m != nil && (iface == "" || m[1] == iface) {
			latencyMs, iface = durationMs(m[2], m[3]), m[1]
		}
		if m := tbfRateRe.FindStringSubmatch(line); m != nil {
			kbps := rateKbps(m[2], m[3])
			switch {
			case m[1] == shapeIngressDev:
				downloadKbps = kbps
			case iface == "" || m[1] == iface:
				uploadKbps, iface = kbps, m[1]
			}
		}
	}
	return latencyMs, downloadKbps, uploadKbps, iface
}

func durationMs(v, unit string) int {
	f, _ := strconv.ParseFloat(v, 64)
	switch unit {
	case "us":
		f /= 1000
	case "s":
		f *= 1000
	}
	return int(f + 0.5)
}

func rateKbps(v, unit string) int {
	f, _ := strconv.ParseFloat(v, 64)
	switch unit {
	case "":
		f /= 1000
	case "M":
		f *= 1000
	case "G":
		f *= 1000 * 1000
	}
	return int(f + 0.5)
}

// online reports whether the connectivity service has a default network.
func (a *ADB) online(ctx context.Context) (bool, error) {
	out, err := a.Shell(ctx, "dumpsys", "connectivity")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Active default network:"); ok {
			return strings.TrimSpace(v) != "none", nil
		}
	}
	return false, mobiumerr.New(mobiumerr.DeviceServer, "the connectivity service did not name a default network")
}

// Network settles on these: offline arrived within 3s on the emulator and
// the Pixel 8 Pro, and the network came back within 6s after, both measured.
const (
	offlineTimeout = 15 * time.Second
	onlineTimeout  = 30 * time.Second
)

// SetOffline turns airplane mode on or off, and waits for the device to be
// what was asked — without a default network, or with one again. Airplane
// mode is the setting; the network is the outcome, and Wi-Fi a person left on
// in airplane mode keeps a device online with the setting on.
func (a *ADB) SetOffline(ctx context.Context, offline bool) error {
	verb := "disable"
	if offline {
		verb = "enable"
	}
	out, diag, err := a.ShellDiagnostics(ctx, "cmd", "connectivity", "airplane-mode", verb)
	if err != nil {
		return err
	}
	if msg := strings.TrimSpace(string(out) + " " + string(diag)); msg != "" {
		return mobiumerr.New(mobiumerr.DeviceServer, "the device did not switch airplane mode: %s", firstLine(msg))
	}
	timeout := onlineTimeout
	if offline {
		timeout = offlineTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		on, err := a.online(ctx)
		if err != nil {
			return err
		}
		if on != offline {
			if !offline {
				a.settleRoute(ctx)
			}
			return nil
		}
		if time.Now().After(deadline) {
			if offline {
				return mobiumerr.New(mobiumerr.NotConfirmed, "airplane mode is on and the device still has a "+
					"network after %s — Wi-Fi or Bluetooth was set to stay on in airplane mode", timeout).
					WithRemedy("turn Wi-Fi off in the device's quick settings while airplane mode is on, then ask again")
			}
			return mobiumerr.New(mobiumerr.NotConfirmed, "airplane mode is off and the device has no network "+
				"after %s — nothing it was connected to came back", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// settleRoute waits for the default route to stop moving: back from
// airplane mode, the emulator was online on eth0 and moved to wlan0 a few
// seconds later. Settled is the same interface three reads running, a
// second apart; it gives up after routeSettle, when the network is up
// whichever way it goes.
func (a *ADB) settleRoute(ctx context.Context) {
	deadline := time.Now().Add(routeSettle)
	last, same := "", 0
	for time.Now().Before(deadline) && same < 3 {
		now := a.defaultInterface(ctx)
		if now != "" && now == last {
			same++
		} else {
			last, same = now, 1
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

const routeSettle = 15 * time.Second

// asRoot runs a script as root through su, which a userdebug build — an
// emulator's Google APIs image — has and a phone's user build does not.
// Through su rather than `adb root`, which restarts adbd and with it every
// forward and instrumentation a session holds.
func (a *ADB) asRoot(ctx context.Context, script string) error {
	out, diag, err := a.ShellDiagnostics(ctx, "su 0 sh -c "+shellQuote(script+" && echo mobium-ok"))
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), "mobium-ok") {
		return mobiumerr.New(mobiumerr.DeviceServer, "shaping the network failed: %s",
			firstLine(strings.TrimSpace(string(out)+" "+string(diag))))
	}
	return nil
}

// CanShape reports whether traffic can be shaped here: su has to give root.
func (a *ADB) CanShape(ctx context.Context) bool {
	out, err := a.Shell(ctx, "su 0 id -u")
	return err == nil && strings.TrimSpace(string(out)) == "0"
}

// Shape puts a latency and rate limits on the interface traffic leaves by,
// replacing any Mobium put there before; zeros remove them. It is read back
// after, and anything that did not take is reported.
//
// Latency is a netem delay on outgoing traffic, so it adds to every round
// trip. Upload is a token bucket under it. Download is a token bucket on
// ifb0, fed by a filter that redirects the interface's incoming traffic —
// added at a priority of its own, beside the clsact hook Android already
// keeps there, which is left alone.
func (a *ADB) Shape(ctx context.Context, latencyMs, downloadKbps, uploadKbps int) (NetworkConditions, error) {
	if a.defaultInterface(ctx) == "" {
		return NetworkConditions{}, mobiumerr.New(mobiumerr.DeviceNotReady, "the device has no default network "+
			"to shape — it is offline").WithRemedy("bring it back online first (offline false)")
	}
	ifaces := a.trafficInterfaces(ctx)
	if err := a.Unshape(ctx); err != nil {
		return NetworkConditions{}, err
	}
	var steps []string
	if downloadKbps > 0 {
		steps = append(steps, "ip link set "+shapeIngressDev+" up",
			fmt.Sprintf("tc qdisc replace dev %s root tbf rate %dkbit burst 16kb latency 1s", shapeIngressDev,
				downloadKbps))
	}
	for _, iface := range ifaces {
		if latencyMs > 0 || uploadKbps > 0 {
			steps = append(steps, fmt.Sprintf("tc qdisc add dev %s root handle %s netem delay %dms", iface,
				shapeRootHandle, latencyMs))
		}
		if uploadKbps > 0 {
			steps = append(steps, fmt.Sprintf("tc qdisc add dev %s parent 1:1 handle %s tbf rate %dkbit "+
				"burst 16kb latency 1s", iface, shapeRateHandle, uploadKbps))
		}
		if downloadKbps > 0 {
			steps = append(steps, fmt.Sprintf("tc filter add dev %s ingress pref %s protocol all matchall "+
				"action mirred egress redirect dev %s", iface, shapeFilterPref, shapeIngressDev))
		}
	}
	if len(steps) > 0 {
		if err := a.asRoot(ctx, strings.Join(steps, " && ")); err != nil {
			_ = a.Unshape(ctx)
			return NetworkConditions{}, err
		}
	}
	got, err := a.NetworkConditions(ctx)
	if err != nil {
		return got, err
	}
	if got.LatencyMs != latencyMs || got.DownloadKbps != downloadKbps || got.UploadKbps != uploadKbps {
		return got, mobiumerr.New(mobiumerr.NotConfirmed, "asked for %dms, %d kbit/s down and %d kbit/s up, and "+
			"the device has %dms, %d down and %d up", latencyMs, downloadKbps, uploadKbps, got.LatencyMs,
			got.DownloadKbps, got.UploadKbps)
	}
	return got, nil
}

// trafficInterfaces are the interfaces traffic can leave by: Wi-Fi, and the
// mobile data the emulator calls eth0. Every one is shaped, because the
// default route moves between them — after airplane mode the emulator came
// back on eth0 and moved to wlan0 seconds later, and a delay put on the
// default interface of that moment was on one nothing used, measured. Not
// hwsim0, which carries the emulator's virtual Wi-Fi radio itself.
func (a *ADB) trafficInterfaces(ctx context.Context) []string {
	out, err := a.Shell(ctx, "ls", "/sys/class/net")
	if err != nil {
		return nil
	}
	var ifaces []string
	for _, f := range strings.Fields(string(out)) {
		if trafficIfaceRe.MatchString(f) {
			ifaces = append(ifaces, f)
		}
	}
	return ifaces
}

var trafficIfaceRe = regexp.MustCompile(`^(wlan|eth|rmnet|radio)[0-9]+$`)

// Unshape removes what Shape added, and only that, from every interface:
// the default route can leave by another one for a while after the network
// comes back, and a cleanup that looked only at the default interface left a
// 200ms delay on wlan0 at the end of a session, measured on the emulator.
// Mobium's rules are told apart by their handle and priority; deleting one
// that is not there fails, and that is not an error here.
func (a *ADB) Unshape(ctx context.Context) error {
	if !a.CanShape(ctx) {
		// Nothing could have been put there without root, so nothing is.
		return nil
	}
	script := fmt.Sprintf("for d in $(ls /sys/class/net); do "+
		"tc qdisc show dev $d | grep -q 'netem %s ' && tc qdisc del dev $d root; "+
		"tc filter del dev $d ingress pref %s 2>/dev/null; "+
		"done; "+
		"tc qdisc show dev %s | grep -q tbf && tc qdisc del dev %s root; "+
		"ip link set %s down; true",
		shapeRootHandle, shapeFilterPref, shapeIngressDev, shapeIngressDev, shapeIngressDev)
	return a.asRoot(ctx, script)
}
