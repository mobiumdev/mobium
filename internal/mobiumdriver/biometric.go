package mobiumdriver

import (
	"context"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// Biometric actions.
const (
	BioStatus   = "status"
	BioEnroll   = "enroll"
	BioUnenroll = "unenroll"
	BioMatch    = "match"
	BioNoMatch  = "nomatch"
)

// Biometric outcomes: what the device did with a face or finger, read back
// rather than assumed.
const (
	BioAccepted      = "accepted"
	BioNotRecognized = "not recognized"
	BioLockedOut     = "locked out"
	// BioFailed is a prompt that closed after a face or finger that did not
	// match: it gave up, and the app was told the sign-in failed. Touch ID
	// does it on the third, measured on an iPhone SE simulator.
	BioFailed = "failed"
)

// BiometricState is what a biometric action found, or left.
type BiometricState struct {
	// Kind is device.BiometricFace or device.BiometricFinger: the one this
	// device can be shown.
	Kind     string
	Enrolled bool
	// LockedOut says the sensor refuses every touch for now, after too many
	// that did not match. Android reports it; a simulator has no such state.
	LockedOut bool
	// Outcome is set by match and nomatch: accepted, not recognized, or
	// locked out.
	Outcome string
	// Note says anything done on the way the caller should know of, such as
	// a PIN set so that a fingerprint could be enrolled.
	Note string
}

// Biometrics is implemented by backends that can enroll a face or finger on
// a virtual device and present one to it.
type Biometrics interface {
	Biometric(ctx context.Context, action string) (BiometricState, error)
}

// A real phone refuses every action, status included: whether a person has
// enrolled a finger is theirs, and nothing presented from outside could use
// the answer.
func phoneBiometricRefusal(what string) error {
	return mobiumerr.New(mobiumerr.Unsupported, "nothing outside a real phone can present a finger or a face "+
		"to it, and its enrollment is its owner's — biometrics are for an emulator or a simulator; on %s, "+
		"sign in with the real sensor by hand, or run the flow on a virtual device", what)
}

// Biometric enrolls, reads or presents a fingerprint on an emulator.
func (a *Android) Biometric(ctx context.Context, action string) (BiometricState, error) {
	return androidBiometric(ctx, a, a.adb, action)
}

// Biometric enrolls, reads or presents a fingerprint on an emulator.
func (u *UIA2) Biometric(ctx context.Context, action string) (BiometricState, error) {
	return androidBiometric(ctx, u, u.adb, action)
}

// fingerprintADB is the part of adb biometrics need, so the logic can be
// tested without a device.
type fingerprintADB interface {
	IsEmulator(context.Context) bool
	Fingerprints(context.Context) (device.Fingerprints, error)
	FingerTouch(ctx context.Context, finger int) error
	ScreenLock(context.Context) (string, error)
	SetMobiumPIN(context.Context) error
	ClearMobiumPIN(context.Context) error
	Shell(ctx context.Context, rest ...string) ([]byte, error)
	LaunchApp(ctx context.Context, pkg string) error
}

// The emulator's console takes a finger by number. Enrolling uses
// enrolledFinger, so it is the one that matches; strangerFinger is never
// enrolled here, so the sensor does not know it.
const (
	enrolledFinger = 1
	strangerFinger = 9
)

// fingerSettle is how long a device is given to answer a face or finger:
// on the Pixel 7 AVD the fingerprint counters had moved within 300ms. A
// variable so the tests need not wait it out.
var fingerSettle = 3 * time.Second

func androidBiometric(ctx context.Context, d Driver, adb fingerprintADB, action string) (BiometricState, error) {
	if !adb.IsEmulator(ctx) {
		return BiometricState{}, phoneBiometricRefusal("an Android phone")
	}
	before, err := adb.Fingerprints(ctx)
	if err != nil {
		return BiometricState{}, err
	}
	state := BiometricState{Kind: device.BiometricFinger, Enrolled: before.Enrolled > 0, LockedOut: before.LockedOut}
	switch action {
	case BioStatus:
		return state, nil
	case BioEnroll:
		if state.Enrolled {
			return state, nil
		}
		return enrollFingerprint(ctx, d, adb, before)
	case BioUnenroll:
		return unenrollFingerprint(ctx, adb, state)
	case BioMatch, BioNoMatch:
		return presentFingerprint(ctx, adb, state, before, action == BioMatch)
	}
	return state, mobiumerr.New(mobiumerr.InvalidArgument, "unknown biometric action %q", action)
}

// presentFingerprint touches the sensor with the enrolled finger or a
// stranger's, and reads the fingerprint service's counters to say what it
// made of it. A touch no prompt and no lock screen was listening for moves
// none of them, and is refused as such rather than reported as sent.
func presentFingerprint(ctx context.Context, adb fingerprintADB, state BiometricState, before device.Fingerprints, match bool) (BiometricState, error) {
	if match && !state.Enrolled {
		return state, mobiumerr.New(mobiumerr.DeviceNotReady, "no fingerprint is enrolled on this emulator, so "+
			"none can match — enroll one first").WithRemedy("app_biometric with action \"enroll\"")
	}
	finger := enrolledFinger
	if !match {
		finger = strangerFinger
	}
	if err := adb.FingerTouch(ctx, finger); err != nil {
		return state, err
	}
	deadline := time.Now().Add(fingerSettle)
	for {
		after, err := adb.Fingerprints(ctx)
		if err != nil {
			return state, err
		}
		switch {
		case after.Lockouts > before.Lockouts:
			state.Outcome = BioLockedOut
		case after.Accepted > before.Accepted:
			state.Outcome = BioAccepted
		case after.Rejected > before.Rejected:
			state.Outcome = BioNotRecognized
		}
		if state.Outcome != "" {
			if match && state.Outcome == BioLockedOut {
				// Measured on the Pixel 7 AVD: failed touches carry over from one
				// prompt to the next — neither a success nor the PIN reset them
				// every time — so the first touch of a prompt can lock it out.
				state.Note = "failed touches carry over from earlier prompts, and the sensor was at its limit"
			}
			if match != (state.Outcome == BioAccepted) && state.Outcome != BioLockedOut {
				return state, mobiumerr.New(mobiumerr.NotConfirmed, "presented finger %d and the emulator "+
					"answered %s; enrolling here uses finger %d, so a finger enrolled by hand may differ",
					finger, state.Outcome, enrolledFinger)
			}
			return state, nil
		}
		if time.Now().After(deadline) && after.LockedOut {
			return state, mobiumerr.New(mobiumerr.DeviceNotReady, "the fingerprint sensor is locked out after "+
				"too many failed touches, and counts none until the lockout ends — about 30 seconds, or "+
				"for good until the PIN is used").
				WithRemedy("wait for the lockout to end, or unlock with the screen lock")
		}
		if time.Now().After(deadline) {
			return state, mobiumerr.New(mobiumerr.DeviceNotReady, "touched the sensor and nothing was asking "+
				"for a fingerprint — no prompt and no lock screen counted the touch; open the app's "+
				"fingerprint prompt first, and present the finger while it is up").
				WithRemedy("start the app's biometric sign-in, then app_biometric again")
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// settingsPackage is the Settings app, which enrolling walks.
const settingsPackage = "com.android.settings"

// enrollBudget bounds the walk through Settings. It took about 15 seconds on
// the Pixel 7 AVD.
const enrollBudget = 60 * time.Second

// settingsArrive bounds the wait for Settings to come to the front.
const settingsArrive = 10 * time.Second

// enrollFingerprint walks Settings' own enrollment, as a person would: a
// screen lock first, since Android enrolls no fingerprint without one, then
// the credential typed back, the introduction agreed to, and the finger
// touched to the sensor until the service counts one more print.
//
// Nothing is found by what it says, which changes with the language: the
// PIN field by its resource id, and the button that goes on by being the
// footer's right-hand one — MORE, then I AGREE, on the Pixel 7 AVD — while
// the left-hand one declines (NOT NOW, DO IT LATER). On the screen that asks
// for the finger only the left-hand one remains, and that is when the
// sensor is touched. Done is when the count rises, read from the
// fingerprint service, not from any screen.
func enrollFingerprint(ctx context.Context, d Driver, adb fingerprintADB, before device.Fingerprints) (BiometricState, error) {
	state := BiometricState{Kind: device.BiometricFinger}
	lock, err := adb.ScreenLock(ctx)
	if err != nil {
		return state, err
	}
	switch lock {
	case device.LockOther:
		return state, mobiumerr.New(mobiumerr.DeviceNotReady, "this emulator has a screen lock mobium did not "+
			"set, and enrolling a fingerprint means typing it into Settings — remove it in Settings > "+
			"Security, or enroll by hand in Settings > Security > Fingerprint, touching the sensor with "+
			"`adb emu finger touch %d`", enrolledFinger)
	case device.LockNone:
		if err := adb.SetMobiumPIN(ctx); err != nil {
			return state, err
		}
		state.Note = "set the emulator's screen lock to PIN " + device.MobiumPIN + ", since Android enrolls " +
			"no fingerprint without one; unenroll removes both"
	}

	back := ""
	if t, err := d.Snapshot(ctx); err == nil {
		back = t.Package()
	}
	if _, err := adb.Shell(ctx, "am", "start", "-a", "android.settings.FINGERPRINT_ENROLL"); err != nil {
		return state, err
	}
	defer func() {
		_, _ = adb.Shell(context.WithoutCancel(ctx), "am", "force-stop", settingsPackage)
		if back != "" && back != settingsPackage {
			_ = adb.LaunchApp(context.WithoutCancel(ctx), back)
		}
	}()

	typedPIN, inSettings := false, false
	deadline := time.Now().Add(enrollBudget)
	arrive := time.Now().Add(settingsArrive)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return state, ctx.Err()
		}
		now, err := adb.Fingerprints(ctx)
		if err != nil {
			return state, err
		}
		if now.Enrolled > before.Enrolled {
			state.Enrolled = true
			return state, nil
		}
		tree, err := d.Snapshot(ctx)
		if err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		// `am start` answers when the intent is dispatched, not when Settings
		// is in front: the first read after it still showed MobiumApp, and
		// was taken for Settings having gone. Only an app in front once
		// Settings has been is a walk that left.
		if pkg := tree.Package(); pkg != settingsPackage {
			if inSettings {
				return state, mobiumerr.New(mobiumerr.NotConfirmed, "enrolling a fingerprint left Settings for "+
					"%q before the emulator counted a print", pkg)
			}
			if time.Now().After(arrive) {
				return state, mobiumerr.New(mobiumerr.NotConfirmed, "asked Settings for fingerprint enrollment, "+
					"and %q is still in front after %s", pkg, settingsArrive)
			}
			time.Sleep(300 * time.Millisecond)
			continue
		}
		inSettings = true
		switch step, x, y := enrollStep(tree); step {
		case enrollTypePIN:
			if typedPIN {
				return state, mobiumerr.New(mobiumerr.NotConfirmed, "Settings asked for the screen lock again "+
					"after PIN %s was typed", device.MobiumPIN)
			}
			typedPIN = true
			if err := d.Tap(ctx, x, y); err != nil {
				return state, err
			}
			// Digits only, so `input text` cannot mangle it.
			if _, err := adb.Shell(ctx, "input", "text", device.MobiumPIN); err != nil {
				return state, err
			}
			if _, err := adb.Shell(ctx, "input", "keyevent", "KEYCODE_ENTER"); err != nil {
				return state, err
			}
			time.Sleep(time.Second)
		case enrollGoOn:
			if err := d.Tap(ctx, x, y); err != nil {
				return state, err
			}
			time.Sleep(700 * time.Millisecond)
		default:
			if err := adb.FingerTouch(ctx, enrolledFinger); err != nil {
				return state, err
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	return state, mobiumerr.New(mobiumerr.Timeout, "walked Settings' fingerprint enrollment for %s and the "+
		"emulator counted no new print", enrollBudget)
}

const (
	enrollTouch = iota
	enrollTypePIN
	enrollGoOn
)

// enrollStep says what the enrollment screen in front wants: the PIN typed
// into its field, the footer's right-hand button pressed, or — neither being
// there — the finger on the sensor. It answers where to tap.
func enrollStep(tree *uitree.Tree) (step, x, y int) {
	width, height := 0, 0
	if tree.Root != nil {
		width, height = tree.Root.Bounds.X2, tree.Root.Bounds.Y2
	}
	step = enrollTouch
	tree.Walk(func(n *uitree.Node) bool {
		if strings.HasSuffix(n.TestID, ":id/password_entry") {
			step = enrollTypePIN
			x, y = n.Bounds.Center()
			return false
		}
		cx, cy := n.Bounds.Center()
		if n.Clickable && n.Enabled && strings.HasSuffix(n.Class, ".Button") &&
			cx > width/2 && cy > height*3/4 {
			step, x, y = enrollGoOn, cx, cy
		}
		return true
	})
	return step, x, y
}

// unenrollFingerprint removes mobium's PIN, which takes every enrolled print
// with it, and reads the count back.
func unenrollFingerprint(ctx context.Context, adb fingerprintADB, state BiometricState) (BiometricState, error) {
	lock, err := adb.ScreenLock(ctx)
	if err != nil {
		return state, err
	}
	switch lock {
	case device.LockNone:
		// No lock, so no prints: Android removes them with it.
		return state, nil
	case device.LockOther:
		return state, mobiumerr.New(mobiumerr.DeviceNotReady, "this emulator's screen lock is not the one "+
			"mobium sets, so its fingerprints are removed in Settings > Security > Fingerprint, by hand")
	}
	if err := adb.ClearMobiumPIN(ctx); err != nil {
		return state, err
	}
	after, err := adb.Fingerprints(ctx)
	if err != nil {
		return state, err
	}
	state.Enrolled = after.Enrolled > 0
	state.Note = "removed the emulator's PIN " + device.MobiumPIN + ", and every fingerprint with it"
	if state.Enrolled {
		return state, mobiumerr.New(mobiumerr.NotConfirmed, "removed the emulator's PIN, and %d fingerprints "+
			"are still enrolled", after.Enrolled)
	}
	return state, nil
}

// The simulator's LocalAuthentication prompt, as WebDriverAgent reads it,
// in SpringBoard's tree. Face ID waits for a face as an element named
// authentication_ui, and after one that did not match becomes an alert
// offering Try Face ID Again and Cancel. Touch ID is that alert from the
// start — "Touch ID for “MobiumApp”", then "Try Again" — with only Cancel.
// Read on an iPhone 17 Pro and an iPhone SE simulator, iOS 26.5.
const (
	laWaiting  = "authentication_ui"
	laAlert    = "com.apple.localauthentication.ax.authentication.alert"
	laTryAgain = "com.apple.localauthentication.ax.authentication.button.try-again"
)

// laState is the prompt as the tree shows it.
type laState struct {
	// shown is "" for no prompt; otherwise what it says, so a change can be
	// seen: the waiting element, or the alert's value.
	shown string
	// retry is Face ID's Not Recognized alert, which waits for Try Again.
	retry bool
}

func laPrompt(tree *uitree.Tree) laState {
	var st laState
	tree.Walk(func(n *uitree.Node) bool {
		switch n.TestID {
		case laAlert:
			st.shown = "alert: " + n.Label + "#" + n.Text
		case laWaiting:
			if st.shown == "" {
				st.shown = laWaiting
			}
		case laTryAgain:
			st.retry = true
		}
		return true
	})
	return st
}

// Biometric enrolls, reads or presents a face or finger on a simulator. A
// phone refuses.
func (w *WDA) Biometric(ctx context.Context, action string) (BiometricState, error) {
	if w.phone != nil {
		return BiometricState{}, phoneBiometricRefusal("an iPhone")
	}
	kind, err := w.sim.BiometricKind(ctx)
	if err != nil {
		return BiometricState{}, err
	}
	if kind == "" {
		return BiometricState{}, mobiumerr.New(mobiumerr.Unsupported, "this simulator's model has neither "+
			"Face ID nor Touch ID; boot one that has")
	}
	enrolled, err := w.sim.BiometricEnrolled(ctx)
	if err != nil {
		return BiometricState{}, err
	}
	state := BiometricState{Kind: kind, Enrolled: enrolled}
	switch action {
	case BioStatus:
		return state, nil
	case BioEnroll, BioUnenroll:
		on := action == BioEnroll
		if err := w.sim.SetBiometricEnrolled(ctx, on); err != nil {
			return state, err
		}
		state.Enrolled = on
		return state, nil
	case BioMatch, BioNoMatch:
		return w.presentBiometric(ctx, state, action == BioMatch)
	}
	return state, mobiumerr.New(mobiumerr.InvalidArgument, "unknown biometric action %q", action)
}

// presentBiometric shows the simulator a face or finger, but only to a
// prompt that is up — shown to none, it goes nowhere and says nothing — and
// reads the prompt again to say what it did: gone after a match, showing
// Not Recognized after one that did not.
func (w *WDA) presentBiometric(ctx context.Context, state BiometricState, match bool) (BiometricState, error) {
	if !state.Enrolled {
		return state, mobiumerr.New(mobiumerr.DeviceNotReady, "no %s is enrolled on this simulator, so the "+
			"app's prompt fails before it looks — enroll one first", state.Kind).
			WithRemedy("app_biometric with action \"enroll\"")
	}
	tree, err := w.Snapshot(ctx)
	if err != nil {
		return state, err
	}
	before := laPrompt(tree)
	if before.shown == "" {
		return state, mobiumerr.New(mobiumerr.DeviceNotReady, "no biometric prompt is up, so nothing would "+
			"look at a %s — start the app's sign-in first, and present it while the prompt waits", state.Kind).
			WithRemedy("start the app's biometric sign-in, then app_biometric again")
	}
	// Measured on the simulator: a face that does not match, shown to Face
	// ID's Not Recognized alert, changes nothing, while one that does is
	// still accepted and Cancel still cancels. Try Again leaves the alert up
	// however it is tapped — by coordinates, or as WebDriverAgent's own
	// element click — so it is not a remedy, and a second failure in a row
	// cannot be shown to one Face ID prompt here.
	if before.retry && !match {
		return state, mobiumerr.New(mobiumerr.DeviceNotReady, "the prompt is showing Not Recognized, and on a "+
			"simulator looks at no further face that does not match — Try Again leaves it up; present a "+
			"match, or tap Cancel and start the sign-in again").
			WithRemedy("app_biometric with action \"match\", or tap the prompt's Cancel")
	}
	if err := w.sim.PresentBiometric(ctx, state.Kind, match); err != nil {
		return state, err
	}
	deadline := time.Now().Add(fingerSettle)
	for {
		time.Sleep(300 * time.Millisecond)
		tree, err := w.Snapshot(ctx)
		if err == nil {
			after := laPrompt(tree)
			switch {
			case match && after.shown == "":
				state.Outcome = BioAccepted
				return state, nil
			case !match && after.shown == "":
				state.Outcome = BioFailed
				return state, nil
			case !match && after.shown != before.shown:
				state.Outcome = BioNotRecognized
				return state, nil
			}
		}
		if time.Now().After(deadline) {
			// Touch ID's second failure: the prompt already said Try Again,
			// and says it again. Sent to a prompt that was listening, and
			// nothing on screen can say what it made of it.
			if !match && strings.HasPrefix(before.shown, "alert: ") {
				state.Note = "the prompt was already asking to try again and still is, so nothing on " +
					"screen confirms what it made of this one"
				return state, nil
			}
			want := "close"
			if !match {
				want = "change"
			}
			return state, mobiumerr.New(mobiumerr.NotConfirmed, "presented a %s and the prompt did not %s "+
				"within %s", state.Kind, want, fingerSettle)
		}
	}
}
