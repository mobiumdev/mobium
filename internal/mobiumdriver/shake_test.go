package mobiumdriver

import (
	"context"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

type fakeShaker struct {
	emulator bool
	shook    int
}

func (f *fakeShaker) IsEmulator(context.Context) bool { return f.emulator }
func (f *fakeShaker) Shake(context.Context) error     { f.shook++; return nil }

// A real phone refuses, saying why, and nothing is sent to it; an emulator
// is shaken.
func TestShakeOnlyWhereSomethingCanBeShaken(t *testing.T) {
	phone := &fakeShaker{}
	err := androidShake(context.Background(), phone)
	if mobiumerr.CodeOf(err) != mobiumerr.Unsupported || phone.shook != 0 {
		t.Errorf("a phone: %v, shook %d times", err, phone.shook)
	}
	emu := &fakeShaker{emulator: true}
	if err := androidShake(context.Background(), emu); err != nil || emu.shook != 1 {
		t.Errorf("an emulator: %v, shook %d times", err, emu.shook)
	}
	iphone := &WDA{phone: &device.Devicectl{}}
	if err := iphone.Shake(context.Background()); mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
		t.Errorf("an iPhone: %v", err)
	}
}
