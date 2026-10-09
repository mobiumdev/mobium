package device

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
)

// IOSTarget is the iOS device a command resolved to: a simulator or a real
// phone, never both. WebDriverAgent drives either, so the choice is made once
// here and everything above it is the same code.
type IOSTarget struct {
	Sim     *Simctl
	SimInfo *Simulator
	Phone   *Devicectl
}

// Serial is the identifier Mobium names the device by: the simulator's UDID,
// or the phone's hardware UDID.
func (t *IOSTarget) Serial() string {
	if t.Phone != nil {
		return t.Phone.Phone.UDID
	}
	return t.SimInfo.UDID
}

// Model is a human name for the device: a phone's model, never its owner's
// name for it.
func (t *IOSTarget) Model() string {
	if t.Phone != nil {
		return t.Phone.Phone.Label()
	}
	return t.SimInfo.Name
}

// errNoIOSDevice is the no-candidates answer once phones are in the picture.
var errNoIOSDevice = mobiumerr.New(mobiumerr.NoDevice,
	"no iOS simulator is booted and no iPhone is connected — boot a simulator with "+
		"`xcrun simctl boot <udid>` (see `mobium devices`), or connect an iPhone with a "+
		"cable, unlock it and tap Trust")

// SelectIOS resolves the iOS device a command should target.
//
// The same rule as SelectSimulator, widened to phones: an explicit reference
// must match exactly one device, and with no reference exactly one booted
// simulator or connected phone is required, so a run never silently drives a
// different device than meant. A Mac without devicectl still selects
// simulators as before.
func SelectIOS(ctx context.Context, ref string) (*IOSTarget, error) {
	sims, simErr := Simulators(ctx)
	phones, phoneErr := Phones(ctx)
	if simErr != nil && phoneErr != nil {
		return nil, simErr
	}

	var targets []*IOSTarget
	if ref != "" {
		for i := range sims {
			if sims[i].UDID == ref || strings.EqualFold(sims[i].Name, ref) {
				targets = append(targets, &IOSTarget{SimInfo: &sims[i]})
			}
		}
		for _, p := range phones {
			if p.Matches(ref) {
				targets = append(targets, &IOSTarget{Phone: &Devicectl{Phone: p}})
			}
		}
		switch {
		case len(targets) == 0:
			return nil, mobiumerr.New(mobiumerr.NoDevice, "no simulator or iPhone matching %q (see `mobium devices`)", ref)
		case len(targets) > 1 && targets[0].SimInfo != nil && targets[len(targets)-1].Phone != nil:
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%q names both a simulator and an iPhone — use a UDID "+
				"from `mobium devices`", ref)
		}
		// Several simulators sharing a name keep SelectSimulator's behavior:
		// the first, which is the booted or newest one.
		return finishTarget(targets[0])
	}

	for i := range sims {
		if sims[i].Booted() {
			targets = append(targets, &IOSTarget{SimInfo: &sims[i]})
		}
	}
	for _, p := range phones {
		if p.Connected() {
			targets = append(targets, &IOSTarget{Phone: &Devicectl{Phone: p}})
		}
	}
	switch len(targets) {
	case 0:
		if phoneErr != nil {
			return nil, ErrNoSimulator
		}
		return nil, errNoIOSDevice
	case 1:
		return finishTarget(targets[0])
	default:
		var names []string
		for _, t := range targets {
			kind := "simulator"
			if t.Phone != nil {
				kind = "iPhone"
			}
			names = append(names, fmt.Sprintf("%s %s (%s)", kind, t.Model(), t.Serial()))
		}
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%d iOS devices are available (%s) — pick one with --device",
			len(targets), strings.Join(names, ", "))
	}
}

// finishTarget locates the toolchain for the chosen device.
func finishTarget(t *IOSTarget) (*IOSTarget, error) {
	if t.Phone != nil {
		d, err := NewDevicectl(t.Phone.Phone)
		if err != nil {
			return nil, err
		}
		t.Phone = d
		return t, nil
	}
	s, err := NewSimctl(t.SimInfo.UDID)
	if err != nil {
		return nil, err
	}
	s.TV = t.SimInfo.TV()
	t.Sim = s
	return t, nil
}
