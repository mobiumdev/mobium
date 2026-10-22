package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// netFake is a device whose network does what it is told.
type netFake struct {
	*fakeDriver
	c device.NetworkConditions
	// reversed are the device's loopback ports reaching this machine, and
	// order what was done to the proxy and the reverses, in order.
	reversed map[int]bool
	order    []string
}

func (n *netFake) NetworkConditions(context.Context) (device.NetworkConditions, error) {
	c := n.c
	c.ProxyReversed = c.Proxy != "" && device.IsLoopback(c.Proxy) && n.reversed[device.ProxyPort(c.Proxy)]
	return c, nil
}
func (n *netFake) SetProxy(_ context.Context, hostport string) error {
	n.c.Proxy = hostport
	n.order = append(n.order, "proxy "+hostport)
	return nil
}
func (n *netFake) ReversePort(_ context.Context, port int, on bool) (bool, error) {
	if n.reversed == nil {
		n.reversed = map[int]bool{}
	}
	if on && n.reversed[port] {
		return false, nil
	}
	n.reversed[port] = on
	n.order = append(n.order, fmt.Sprintf("reverse %d %v", port, on))
	return true, nil
}
func (n *netFake) SetOffline(_ context.Context, off bool) error {
	n.c.Airplane, n.c.Online = off, !off
	return nil
}
func (n *netFake) Shape(_ context.Context, l, d, u int) (device.NetworkConditions, error) {
	n.c.LatencyMs, n.c.DownloadKbps, n.c.UploadKbps = l, d, u
	return n.c, nil
}

func withNet(t *testing.T) (*Handlers, *session, *netFake) {
	t.Helper()
	h, s, f := withFake(t, screen(t, "Go"))
	n := &netFake{fakeDriver: f, c: device.NetworkConditions{Online: true, Interface: "wlan0"}}
	s.driver = n
	return h, s, n
}

func TestNetworkRefusesWhatCannotBeMeant(t *testing.T) {
	h, _, _ := withNet(t)
	for _, c := range []struct {
		name string
		args map[string]interface{}
	}{
		{"reset with anything else", map[string]interface{}{"reset": true, "offline": true}},
		{"negative latency", map[string]interface{}{"latency_ms": -1}},
		{"fractional rate", map[string]interface{}{"download_kbps": 1.5}},
		{"latency past the bound", map[string]interface{}{"latency_ms": maxLatencyMs + 1}},
		{"offline that is not a boolean", map[string]interface{}{"offline": "yes"}},
		{"a proxy with a scheme", map[string]interface{}{"proxy": "http://127.0.0.1:8888"}},
		{"a proxy with no port", map[string]interface{}{"proxy": "127.0.0.1"}},
		{"a proxy on port 0", map[string]interface{}{"proxy": "127.0.0.1:0"}},
		{"a proxy that is not a string", map[string]interface{}{"proxy": 8888}},
		{"reset with a proxy", map[string]interface{}{"reset": true, "proxy": ""}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := h.network(context.Background(), c.args)
			if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
				t.Errorf("accepted, or refused as %s: %v", mobiumerr.CodeOf(err), err)
			}
		})
	}
}

func TestNetworkReadsBackWhatItSet(t *testing.T) {
	h, _, n := withNet(t)
	res, err := h.network(context.Background(), map[string]interface{}{"latency_ms": 300, "download_kbps": 1000})
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(NetworkView)
	if v.LatencyMs != 300 || v.DownloadKbps != 1000 || !v.Changed {
		t.Errorf("view %+v", v)
	}
	if !strings.Contains(res.Content[0].Text, "300ms") {
		t.Errorf("text %q", res.Content[0].Text)
	}
	// Shaping given replaces all of it: what is omitted is no limit.
	if _, err := h.network(context.Background(), map[string]interface{}{"upload_kbps": 500}); err != nil {
		t.Fatal(err)
	}
	if n.c.LatencyMs != 0 || n.c.DownloadKbps != 0 || n.c.UploadKbps != 500 {
		t.Errorf("after a second shaping: %+v", n.c)
	}
}

// Airplane mode takes part of the shaping with it, so coming back online
// puts back what was last asked for.
func TestComingBackOnlineReappliesTheShaping(t *testing.T) {
	h, _, n := withNet(t)
	ctx := context.Background()
	if _, err := h.network(ctx, map[string]interface{}{"latency_ms": 100, "download_kbps": 1000}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.network(ctx, map[string]interface{}{"offline": true}); err != nil {
		t.Fatal(err)
	}
	n.c.DownloadKbps = 0 // what airplane mode does to it
	if _, err := h.network(ctx, map[string]interface{}{"offline": false}); err != nil {
		t.Fatal(err)
	}
	if n.c.DownloadKbps != 1000 || n.c.LatencyMs != 100 {
		t.Errorf("after coming back: %+v", n.c)
	}
}

// The end of the session puts the network back as it found it — airplane
// mode as it was, and no shaping — and a device nobody changed is left alone.
func TestTheEndOfTheSessionPutsTheNetworkBack(t *testing.T) {
	h, s, n := withNet(t)
	if h.restoreNetwork(s) {
		t.Error("restored a network nothing had changed")
	}
	ctx := context.Background()
	if _, err := h.network(ctx, map[string]interface{}{"latency_ms": 200}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.network(ctx, map[string]interface{}{"offline": true}); err != nil {
		t.Fatal(err)
	}
	if !h.restoreNetwork(s) {
		t.Fatal("did not restore")
	}
	if n.c.Airplane || n.c.Shaped() {
		t.Errorf("after the end: %+v", n.c)
	}
}

// A proxy on the device's loopback is this machine's: its port is reversed
// before the device is pointed at it, so the device never has a proxy that
// goes nowhere, and taking the proxy off removes the reverse Mobium added.
func TestALoopbackProxyIsReversedFirst(t *testing.T) {
	h, _, n := withNet(t)
	ctx := context.Background()
	res, err := h.network(ctx, map[string]interface{}{"proxy": "127.0.0.1:8888"})
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(NetworkView)
	if v.Proxy != "127.0.0.1:8888" || !v.ProxyReversed || !v.Changed {
		t.Errorf("view %+v", v)
	}
	if !strings.Contains(res.Content[0].Text, "this machine's through adb reverse") {
		t.Errorf("text %q", res.Content[0].Text)
	}
	if strings.Join(n.order, "; ") != "reverse 8888 true; proxy 127.0.0.1:8888" {
		t.Errorf("order: %v", n.order)
	}
	if _, err := h.network(ctx, map[string]interface{}{"proxy": ""}); err != nil {
		t.Fatal(err)
	}
	if n.c.Proxy != "" || n.reversed[8888] {
		t.Errorf("after taking it off: %+v, reversed %v", n.c, n.reversed)
	}
}

// A proxy elsewhere on the network needs no reverse; one that was reversed
// before Mobium came is not Mobium's to remove.
func TestAReverseThatWasThereIsLeft(t *testing.T) {
	h, s, n := withNet(t)
	ctx := context.Background()
	if _, err := h.network(ctx, map[string]interface{}{"proxy": "10.0.0.5:8080"}); err != nil {
		t.Fatal(err)
	}
	if len(n.reversed) != 0 {
		t.Errorf("reversed for a proxy on the network: %v", n.reversed)
	}
	n.reversed = map[int]bool{9090: true}
	if _, err := h.network(ctx, map[string]interface{}{"proxy": "127.0.0.1:9090"}); err != nil {
		t.Fatal(err)
	}
	if !h.restoreNetwork(s) {
		t.Fatal("did not restore")
	}
	if n.c.Proxy != "" || !n.reversed[9090] {
		t.Errorf("after the end: %+v, reversed %v", n.c, n.reversed)
	}
}

// The end of the session puts back a proxy that was set before, and takes
// off the one this session set.
func TestTheEndOfTheSessionPutsTheProxyBack(t *testing.T) {
	h, s, n := withNet(t)
	n.c.Proxy = "10.0.0.5:8080"
	ctx := context.Background()
	if _, err := h.network(ctx, map[string]interface{}{"proxy": "127.0.0.1:8888"}); err != nil {
		t.Fatal(err)
	}
	if !h.restoreNetwork(s) {
		t.Fatal("did not restore")
	}
	if n.c.Proxy != "10.0.0.5:8080" || n.reversed[8888] {
		t.Errorf("after the end: %+v, reversed %v", n.c, n.reversed)
	}
}
