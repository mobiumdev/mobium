package mobiumdriver

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// fakeSensor stands in for an emulator's fingerprint service: each touch
// moves the counters as answer says.
type fakeSensor struct {
	emulator bool
	counts   device.Fingerprints
	answer   func(finger int, c *device.Fingerprints)
	touched  []int
	lock     string
	cleared  bool
}

func (f *fakeSensor) IsEmulator(context.Context) bool { return f.emulator }
func (f *fakeSensor) Fingerprints(context.Context) (device.Fingerprints, error) {
	return f.counts, nil
}
func (f *fakeSensor) FingerTouch(_ context.Context, finger int) error {
	f.touched = append(f.touched, finger)
	if f.answer != nil {
		f.answer(finger, &f.counts)
	}
	return nil
}
func (f *fakeSensor) ScreenLock(context.Context) (string, error) { return f.lock, nil }
func (f *fakeSensor) SetMobiumPIN(context.Context) error         { f.lock = device.LockMobium; return nil }
func (f *fakeSensor) ClearMobiumPIN(context.Context) error {
	f.lock, f.cleared, f.counts.Enrolled = device.LockNone, true, 0
	return nil
}
func (f *fakeSensor) Shell(context.Context, ...string) ([]byte, error) { return nil, nil }
func (f *fakeSensor) LaunchApp(context.Context, string) error          { return nil }

// enrolledOne answers as the emulator does with finger 1 enrolled and a
// prompt up: finger 1 accepted, any other rejected.
func enrolledOne(finger int, c *device.Fingerprints) {
	c.Acquired++
	if finger == enrolledFinger {
		c.Accepted++
	} else {
		c.Rejected++
	}
}

func quickSettle(t *testing.T) {
	was := fingerSettle
	fingerSettle = 50 * time.Millisecond
	t.Cleanup(func() { fingerSettle = was })
}

// A real phone refuses every action, status too, and nothing touches it.
func TestBiometricRefusesAPhone(t *testing.T) {
	phone := &fakeSensor{}
	for _, action := range []string{BioStatus, BioEnroll, BioMatch} {
		_, err := androidBiometric(context.Background(), nil, phone, action)
		if mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
			t.Errorf("%s on a phone: %v", action, err)
		}
	}
	if len(phone.touched) != 0 {
		t.Errorf("touched a phone's sensor: %v", phone.touched)
	}
	iphone := &WDA{phone: &device.Devicectl{}}
	if _, err := iphone.Biometric(context.Background(), BioStatus); mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
		t.Errorf("an iPhone: %v", err)
	}
}

// What the service counted is the answer: the enrolled finger accepted, a
// stranger's not recognized, and the touch that tipped it into lockout
// reported as that.
func TestFingerprintOutcomesAreReadBack(t *testing.T) {
	quickSettle(t)
	ctx := context.Background()
	emu := &fakeSensor{emulator: true, counts: device.Fingerprints{Enrolled: 1}, answer: enrolledOne}
	if st, err := androidBiometric(ctx, nil, emu, BioMatch); err != nil || st.Outcome != BioAccepted {
		t.Errorf("match: %+v, %v", st, err)
	}
	if st, err := androidBiometric(ctx, nil, emu, BioNoMatch); err != nil || st.Outcome != BioNotRecognized {
		t.Errorf("nomatch: %+v, %v", st, err)
	}
	if emu.touched[1] == enrolledFinger {
		t.Error("nomatch touched with the enrolled finger")
	}
	emu.answer = func(_ int, c *device.Fingerprints) { c.Acquired++; c.Rejected++; c.Lockouts++ }
	if st, err := androidBiometric(ctx, nil, emu, BioNoMatch); err != nil || st.Outcome != BioLockedOut {
		t.Errorf("the fifth: %+v, %v", st, err)
	}
}

// A touch nothing counted went to no prompt, and is refused rather than
// reported as sent; with the sensor locked out, the refusal says so, since
// opening the prompt again would not help.
func TestFingerprintNobodyAskedIsRefused(t *testing.T) {
	quickSettle(t)
	ctx := context.Background()
	emu := &fakeSensor{emulator: true, counts: device.Fingerprints{Enrolled: 1}}
	_, err := androidBiometric(ctx, nil, emu, BioMatch)
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "nothing was asking") {
		t.Errorf("no prompt: %v", err)
	}
	emu.counts.LockedOut = true
	_, err = androidBiometric(ctx, nil, emu, BioMatch)
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "locked out") {
		t.Errorf("locked out: %v", err)
	}
}

// Nothing enrolled cannot match, and is refused before touching; a stranger
// accepted means a finger enrolled by hand, and is not called a non-match.
func TestFingerprintMismatchedAnswers(t *testing.T) {
	quickSettle(t)
	ctx := context.Background()
	empty := &fakeSensor{emulator: true, answer: enrolledOne}
	if _, err := androidBiometric(ctx, nil, empty, BioMatch); mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || len(empty.touched) != 0 {
		t.Errorf("nothing enrolled: %v, touched %v", err, empty.touched)
	}
	every := &fakeSensor{emulator: true, counts: device.Fingerprints{Enrolled: 1},
		answer: func(_ int, c *device.Fingerprints) { c.Acquired++; c.Accepted++ }}
	if _, err := androidBiometric(ctx, nil, every, BioNoMatch); mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed {
		t.Errorf("a stranger accepted: %v", err)
	}
}

// Unenrolling clears mobium's PIN and the prints with it; a lock that is
// somebody else's is left alone.
func TestUnenrollClearsOnlyMobiumsPIN(t *testing.T) {
	ctx := context.Background()
	emu := &fakeSensor{emulator: true, counts: device.Fingerprints{Enrolled: 1}, lock: device.LockMobium}
	if st, err := androidBiometric(ctx, nil, emu, BioUnenroll); err != nil || st.Enrolled || !emu.cleared {
		t.Errorf("mobium's PIN: %+v, %v, cleared %v", st, err, emu.cleared)
	}
	other := &fakeSensor{emulator: true, counts: device.Fingerprints{Enrolled: 1}, lock: device.LockOther}
	if _, err := androidBiometric(ctx, nil, other, BioUnenroll); mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || other.cleared {
		t.Errorf("somebody's PIN: %v, cleared %v", err, other.cleared)
	}
	if _, err := androidBiometric(ctx, nil, other, BioEnroll); err != nil && mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
		t.Errorf("enrolled already: %v", err)
	}
}

func parseFixture(t *testing.T, name string, parse func([]byte) (*uitree.Tree, error)) *uitree.Tree {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// Settings' enrollment, captured on the Pixel 7 AVD, one screen at a time:
// the PIN field, the right-hand footer button (MORE, then I AGREE), and on
// the screen that asks for the finger, only the left-hand DO IT LATER — so
// the sensor is touched rather than enrollment declined.
func TestEnrollStepOnEachSettingsScreen(t *testing.T) {
	for _, tc := range []struct {
		file string
		want int
	}{
		{"enroll-pin-uia2.xml", enrollTypePIN},
		{"enroll-intro-uia2.xml", enrollGoOn},
		{"enroll-agree-uia2.xml", enrollGoOn},
		{"enroll-touch-uia2.xml", enrollTouch},
	} {
		tree := parseFixture(t, tc.file, uitree.ParseAndroid)
		step, x, y := enrollStep(tree)
		if step != tc.want {
			t.Errorf("%s: step %d, want %d", tc.file, step, tc.want)
			continue
		}
		if step == enrollGoOn && x < 540 {
			t.Errorf("%s: would tap the left-hand button at (%d, %d)", tc.file, x, y)
		}
	}
}

// The simulator's prompt, as SpringBoard's tree shows it: Face ID waiting,
// Face ID after a face it did not know, and Touch ID before and after a
// finger it did not know — whose alert changes only its words.
func TestLocalAuthenticationPromptIsRead(t *testing.T) {
	faceWait := laPrompt(parseFixture(t, "ios26-faceid-waiting.xml", uitree.ParseIOS))
	if faceWait.shown != laWaiting || faceWait.retry {
		t.Errorf("Face ID waiting: %+v", faceWait)
	}
	faceRetry := laPrompt(parseFixture(t, "../../uitree/testdata/ios26-faceid-not-recognized.xml", uitree.ParseIOS))
	if !faceRetry.retry || !strings.HasPrefix(faceRetry.shown, "alert: ") {
		t.Errorf("Face ID not recognized: %+v", faceRetry)
	}
	touchWait := laPrompt(parseFixture(t, "ios26-touchid-waiting.xml", uitree.ParseIOS))
	touchAgain := laPrompt(parseFixture(t, "ios26-touchid-try-again.xml", uitree.ParseIOS))
	if touchWait.shown == "" || touchWait.retry || touchAgain.retry || touchAgain.shown == touchWait.shown {
		t.Errorf("Touch ID: waiting %+v, then %+v", touchWait, touchAgain)
	}
	if none := laPrompt(parseFixture(t, "springboard-banner-ios26.xml", uitree.ParseIOS)); none.shown != "" {
		t.Errorf("found a prompt on a screen with none: %+v", none)
	}
}
