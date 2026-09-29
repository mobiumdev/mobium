package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Bounds on app_network's numbers: past these a request is a typo, not a
// network anyone is testing against.
const (
	maxLatencyMs = 10000
	maxRateKbps  = 1000000
)

// NetworkView is the result of app_network: the conditions read back after
// any change, never the ones asked for.
type NetworkView struct {
	Device string `json:"device"`
	// Airplane is airplane mode; Online whether the device has a default
	// network, which Wi-Fi left on in airplane mode keeps.
	Airplane bool `json:"airplane"`
	Online   bool `json:"online"`
	// Interface is the one traffic leaves by, and the one shaped.
	Interface string `json:"interface,omitempty"`
	// LatencyMs is added to each round trip; the rates are kbit/s. Zero is
	// none.
	LatencyMs    int `json:"latency_ms"`
	DownloadKbps int `json:"download_kbps"`
	UploadKbps   int `json:"upload_kbps"`
	// Changed says whether this call changed anything.
	Changed bool `json:"changed"`
}

func networkView(serial string, c device.NetworkConditions, changed bool) NetworkView {
	return NetworkView{Device: serial, Airplane: c.Airplane, Online: c.Online, Interface: c.Interface,
		LatencyMs: c.LatencyMs, DownloadKbps: c.DownloadKbps, UploadKbps: c.UploadKbps, Changed: changed}
}

// networkBaseline is a device's conditions before Mobium first changed them,
// which the end of the session puts back. Kept per device on the handlers,
// not on the session, so a change of driver — which closes sessions without
// ending anything — does not forget it.
type networkBaseline struct {
	airplane bool
	// latency, down and up are the shaping last asked for, which coming
	// back online puts back: airplane mode removes part of it on the way.
	latency, down, up int
}

func (h *Handlers) network(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	n, ok := mobiumdriver.AsNetworker(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapNetwork, "set network conditions")
	}

	reset, _, err := boolParam(args, "reset")
	if err != nil {
		return nil, err
	}
	offline, hasOffline, err := boolParam(args, "offline")
	if err != nil {
		return nil, err
	}
	shaping := false
	var latency, down, up int
	for _, k := range []struct {
		name string
		max  int
		into *int
	}{{"latency_ms", maxLatencyMs, &latency}, {"download_kbps", maxRateKbps, &down}, {"upload_kbps", maxRateKbps, &up}} {
		if _, given := args[k.name]; !given {
			continue
		}
		f, err := floatArg(args, k.name)
		if err != nil {
			return nil, err
		}
		v := int(f)
		if float64(v) != f || v < 0 || v > k.max {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s must be a whole number from 0 to %d, got %v", k.name, k.max, f)
		}
		*k.into = v
		shaping = true
	}
	if reset && (hasOffline || shaping) {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "reset puts everything back, so it takes nothing else")
	}

	before, err := n.NetworkConditions(ctx)
	if err != nil {
		return nil, err
	}
	if !reset && !hasOffline && !shaping {
		return Result(describeNetwork(before), networkView(s.dev.Serial, before, false)), nil
	}
	base, kept := h.netBaseline[s.dev.Serial]
	if !kept {
		base = networkBaseline{airplane: before.Airplane}
	}
	if shaping {
		base.latency, base.down, base.up = latency, down, up
	}
	h.netBaseline[s.dev.Serial] = base
	// Coming back online puts back the shaping last asked for, which
	// airplane mode took part of on the way out.
	if hasOffline && !offline && !shaping && (base.latency > 0 || base.down > 0 || base.up > 0) {
		shaping, latency, down, up = true, base.latency, base.down, base.up
	}

	if reset {
		shaping, latency, down, up = before.Shaped(), 0, 0, 0
		hasOffline, offline = before.Airplane, false
	}
	// Shaping needs a network to shape, so it goes on before going offline
	// and after coming back.
	if shaping && !(hasOffline && !offline) {
		if _, err := n.Shape(ctx, latency, down, up); err != nil {
			return nil, err
		}
	}
	if hasOffline && offline != before.Airplane {
		if err := n.SetOffline(ctx, offline); err != nil {
			return nil, err
		}
		// Going offline or coming back can put up a screen or a prompt.
		delete(h.refs, s.dev.Serial)
	}
	if shaping && hasOffline && !offline {
		if _, err := n.Shape(ctx, latency, down, up); err != nil {
			return nil, err
		}
	}
	if reset {
		delete(h.netBaseline, s.dev.Serial)
	}

	after, err := n.NetworkConditions(ctx)
	if err != nil {
		return nil, err
	}
	changed := after != before
	return Result(describeNetwork(after), networkView(s.dev.Serial, after, changed)), nil
}

// boolParam reads an optional boolean, saying whether it was given and
// refusing anything that is not true or false.
func boolParam(args map[string]interface{}, key string) (value, given bool, err error) {
	raw, ok := args[key]
	if !ok {
		return false, false, nil
	}
	b, isBool := raw.(bool)
	if !isBool {
		return false, true, mobiumerr.New(mobiumerr.InvalidArgument, "%s must be true or false", key)
	}
	return b, true, nil
}

// describeNetwork says what the device has, in a line.
func describeNetwork(c device.NetworkConditions) string {
	var parts []string
	switch {
	case c.Airplane && !c.Online:
		parts = append(parts, "offline (airplane mode)")
	case c.Airplane:
		parts = append(parts, "airplane mode on, and still online — Wi-Fi left on in airplane mode")
	case !c.Online:
		parts = append(parts, "no network")
	default:
		parts = append(parts, "online")
	}
	if c.Shaped() {
		var s []string
		if c.LatencyMs > 0 {
			s = append(s, fmt.Sprintf("%dms added to each round trip", c.LatencyMs))
		}
		if c.DownloadKbps > 0 {
			s = append(s, fmt.Sprintf("download %d kbit/s", c.DownloadKbps))
		}
		if c.UploadKbps > 0 {
			s = append(s, fmt.Sprintf("upload %d kbit/s", c.UploadKbps))
		}
		parts = append(parts, strings.Join(s, ", "))
	} else if c.Online {
		parts = append(parts, "no shaping")
	}
	return strings.Join(parts, "; ")
}

// networkRestoreTimeout bounds putting a device's network back at the end.
const networkRestoreTimeout = 40 * time.Second

// restoreNetwork puts back what app_network changed on the session's device:
// no shaping, and airplane mode as it was before the first change. Best
// effort, as the rest of a session's end is; it says whether it did.
func (h *Handlers) restoreNetwork(s *session) bool {
	base, changed := h.netBaseline[s.dev.Serial]
	if !changed {
		return false
	}
	delete(h.netBaseline, s.dev.Serial)
	n, ok := mobiumdriver.AsNetworker(s.driver)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), networkRestoreTimeout)
	defer cancel()
	now, err := n.NetworkConditions(ctx)
	if err != nil {
		return false
	}
	if now.Airplane != base.airplane {
		if err := n.SetOffline(ctx, base.airplane); err != nil {
			return false
		}
	}
	if now.Shaped() {
		if _, err := n.Shape(ctx, 0, 0, 0); err != nil {
			return false
		}
	}
	return true
}
