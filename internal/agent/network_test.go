package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// netFake is a device whose network does what it is told.
type netFake struct {
	*fakeDriver
	c device.NetworkConditions
}

func (n *netFake) NetworkConditions(context.Context) (device.NetworkConditions, error) {
	return n.c, nil
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
