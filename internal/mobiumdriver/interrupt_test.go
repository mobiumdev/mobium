package mobiumdriver

import (
	"context"
	"strings"
	"testing"
)

type fakePhone struct {
	emulator bool
	calls    []string
	messages []string
}

func (f *fakePhone) IsEmulator(context.Context) bool { return f.emulator }
func (f *fakePhone) Call(_ context.Context, action, number string) error {
	f.calls = append(f.calls, action+":"+number)
	return nil
}
func (f *fakePhone) SendSMS(_ context.Context, from, text string) error {
	f.messages = append(f.messages, from+":"+text)
	return nil
}

// TestRealHardwareIsRefusedWithTheReason: a phone cannot be made to ring from
// outside. That is a property of phones, not a gap in mobium, and the refusal
// has to say so — otherwise it reads as a bug and someone goes looking.
func TestRealHardwareIsRefusedWithTheReason(t *testing.T) {
	phone := &fakePhone{emulator: false}
	err := androidCall(context.Background(), phone, CallRing, "555")
	if err == nil {
		t.Fatal("tried to ring a real phone")
	}
	for _, want := range []string{"real phone", "emulator"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
	if len(phone.calls) != 0 {
		t.Error("the command reached the device anyway")
	}

	err = androidSMS(context.Background(), phone, "555", "hi")
	if err == nil || !strings.Contains(err.Error(), "emulator") {
		t.Errorf("sms on hardware = %v", err)
	}
	if len(phone.messages) != 0 {
		t.Error("the message reached the device anyway")
	}
}

func TestEmulatorAcceptsCallsAndMessages(t *testing.T) {
	emu := &fakePhone{emulator: true}
	for _, a := range CallActions() {
		if err := androidCall(context.Background(), emu, a, "5559876"); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
	if len(emu.calls) != 3 {
		t.Errorf("calls = %v", emu.calls)
	}
	// A number is optional; something has to be dialled.
	if err := androidCall(context.Background(), emu, CallRing, ""); err != nil {
		t.Fatal(err)
	}
	if last := emu.calls[len(emu.calls)-1]; strings.HasSuffix(last, ":") {
		t.Errorf("an empty number reached the device: %q", last)
	}
	if err := androidSMS(context.Background(), emu, "", "hello"); err != nil {
		t.Fatal(err)
	}
	if last := emu.messages[len(emu.messages)-1]; strings.HasPrefix(last, ":") {
		t.Errorf("an empty sender reached the device: %q", last)
	}
}
