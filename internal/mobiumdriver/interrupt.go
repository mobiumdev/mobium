package mobiumdriver

import (
	"context"
	"github.com/mobiumdev/mobium/internal/mobiumerr"

	"github.com/mobiumdev/mobium/internal/device"
)

// Interruptions is implemented by backends that can make something *else*
// demand the device.
//
// This is the dimension mobile apps fail in and desktop software largely does
// not: a call arrives mid-form, the clock jumps a timezone, a message lands
// while a screen is half-drawn. The failures are of the kind that only appear
// under interruption — state lost on resume, a callback that never fires — and
// none of them can be provoked by tapping.
//
// Calls and messages are **emulator-only**, and that is a property of phones:
// a real handset cannot be made to ring from outside. Implementations say so
// plainly rather than failing obscurely on hardware.
type Interruptions interface {
	// Call drives a simulated incoming call: "ring", "accept" or "hang".
	Call(ctx context.Context, action, number string) error
	// SendSMS delivers a simulated text message.
	SendSMS(ctx context.Context, from, text string) error
	// CanInterrupt reports whether this device supports the above, so the
	// refusal can be specific.
	CanInterrupt(ctx context.Context) bool
}

// Notifications is implemented by backends that can read the shade, put
// something in it, and open it.
//
// Three things rather than one because they answer different questions.
// Reading is how a test asserts an app posted what it should. Posting is an
// interruption arriving from somewhere else. Opening the shade is what makes
// either of them tappable — a notification cannot be interacted with until it
// is on screen, and that is the half the checklist actually asks for.
type Notifications interface {
	Notifications(ctx context.Context) ([]device.Notification, error)
	PostNotification(ctx context.Context, tag, title, text string) error
	SetShade(ctx context.Context, open bool) error
}

// Clock is implemented by backends that can change what time the device
// thinks it is. Unlike calls, this works on real hardware.
type Clock interface {
	Timezone(ctx context.Context) (string, error)
	SetTimezone(ctx context.Context, tz string) error
}

// Call actions.
const (
	CallRing   = "ring"
	CallAccept = "accept"
	CallHang   = "hang"
)

// CallActions lists the vocabulary.
func CallActions() []string { return []string{CallRing, CallAccept, CallHang} }

type telephony interface {
	IsEmulator(context.Context) bool
	Call(context.Context, string, string) error
	SendSMS(context.Context, string, string) error
}

func androidCall(ctx context.Context, adb telephony, action, number string) error {
	if !adb.IsEmulator(ctx) {
		return mobiumerr.New(mobiumerr.Unsupported, "a real phone cannot be made to ring from outside — simulated "+
			"calls are an emulator feature. Run this flow on an emulator, or interrupt the "+
			"device by hand")
	}
	if number == "" {
		number = "5551234"
	}
	return adb.Call(ctx, action, number)
}

func androidSMS(ctx context.Context, adb telephony, from, text string) error {
	if !adb.IsEmulator(ctx) {
		return mobiumerr.New(mobiumerr.Unsupported, "a real phone cannot be sent a simulated message — this is an "+
			"emulator feature. Send a real one, or run the flow on an emulator")
	}
	if from == "" {
		from = "5551234"
	}
	return adb.SendSMS(ctx, from, text)
}

// Notifications lists what is currently in the shade.
func (a *Android) Notifications(ctx context.Context) ([]device.Notification, error) {
	return a.adb.Notifications(ctx)
}

// PostNotification puts one in the shade and confirms it arrived.
func (a *Android) PostNotification(ctx context.Context, tag, title, text string) error {
	return a.adb.PostNotification(ctx, tag, title, text)
}

// SetShade opens or closes the notification panel.
func (a *Android) SetShade(ctx context.Context, open bool) error {
	return a.adb.SetShade(ctx, open)
}

// Notifications lists what is currently in the shade.
func (u *UIA2) Notifications(ctx context.Context) ([]device.Notification, error) {
	return u.adb.Notifications(ctx)
}

// PostNotification puts one in the shade and confirms it arrived.
func (u *UIA2) PostNotification(ctx context.Context, tag, title, text string) error {
	return u.adb.PostNotification(ctx, tag, title, text)
}

// SetShade opens or closes the notification panel.
func (u *UIA2) SetShade(ctx context.Context, open bool) error {
	return u.adb.SetShade(ctx, open)
}

// Call drives a simulated incoming call.
func (a *Android) Call(ctx context.Context, action, number string) error {
	return androidCall(ctx, a.adb, action, number)
}

// SendSMS delivers a simulated text message.
func (a *Android) SendSMS(ctx context.Context, from, text string) error {
	return androidSMS(ctx, a.adb, from, text)
}

// CanInterrupt reports whether simulated calls and messages are available.
func (a *Android) CanInterrupt(ctx context.Context) bool { return a.adb.IsEmulator(ctx) }

// Timezone reports the device's timezone.
func (a *Android) Timezone(ctx context.Context) (string, error) { return a.adb.Timezone(ctx) }

// SetTimezone changes the device timezone and confirms it.
func (a *Android) SetTimezone(ctx context.Context, tz string) error {
	return a.adb.SetTimezone(ctx, tz)
}

// Call drives a simulated incoming call.
func (u *UIA2) Call(ctx context.Context, action, number string) error {
	return androidCall(ctx, u.adb, action, number)
}

// SendSMS delivers a simulated text message.
func (u *UIA2) SendSMS(ctx context.Context, from, text string) error {
	return androidSMS(ctx, u.adb, from, text)
}

// CanInterrupt reports whether simulated calls and messages are available.
func (u *UIA2) CanInterrupt(ctx context.Context) bool { return u.adb.IsEmulator(ctx) }

// Timezone reports the device's timezone.
func (u *UIA2) Timezone(ctx context.Context) (string, error) { return u.adb.Timezone(ctx) }

// SetTimezone changes the device timezone and confirms it.
func (u *UIA2) SetTimezone(ctx context.Context, tz string) error {
	return u.adb.SetTimezone(ctx, tz)
}
