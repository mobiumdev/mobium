package device

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/plist"
)

// Biometrics on an emulator and a simulator. Nothing outside a real phone
// can present a finger or a face to it, so both halves here are for virtual
// devices only; the driver refuses a phone before reaching them.

// Fingerprints is what an Android device's fingerprint service reports:
// how many prints are enrolled, and how many times it has accepted,
// rejected, locked out and been touched at all since boot — summed across
// sensors. A touch nobody was listening for moves none of the counters, which
// is how a finger presented to no prompt is told from one a prompt refused.
type Fingerprints struct {
	Enrolled int
	Accepted int
	Rejected int
	Lockouts int
	Acquired int
	// LockedOut says the sensor is locked out now, timed or for good, for
	// strong biometrics: while it is, no touch is counted at all.
	LockedOut bool
}

// lockoutNow is the service's live lockout state, one line per user:
// "(BIOMETRIC_STRONG, permanentLockout=false, timedLockout=false)".
var lockoutNow = regexp.MustCompile(`BIOMETRIC_STRONG, permanentLockout=(\w+), timedLockout=(\w+)`)

// fingerprintJSON is the one line of `dumpsys fingerprint` that is JSON:
// {"service":"FingerprintProvider/default","prints":[{"id":0,"count":1,…}]}.
// Measured on the Pixel 7 AVD, Android 15. The crypto counters are kept
// apart by the service — an app asking with a CryptoObject is counted there —
// so each answer is the sum of both.
var fingerprintJSON = regexp.MustCompile(`(?m)^\{"service":.*"prints":.*\}\s*$`)

// ParseFingerprints reads the counters out of `dumpsys fingerprint`.
func ParseFingerprints(out []byte) (Fingerprints, error) {
	var f Fingerprints
	lines := fingerprintJSON.FindAll(out, -1)
	if len(lines) == 0 {
		return f, mobiumerr.New(mobiumerr.Unsupported, "this device reports no fingerprint sensor")
	}
	for _, line := range lines {
		var v struct {
			Prints []struct {
				Count, Accept, Reject, Acquire, Lockout, PermanentLockout int
				AcceptCrypto, RejectCrypto, AcquireCrypto                 int
			} `json:"prints"`
		}
		if err := json.Unmarshal(line, &v); err != nil {
			return f, mobiumerr.New(mobiumerr.DeviceServer, "read the fingerprint service's counters: %w", err)
		}
		for _, p := range v.Prints {
			f.Enrolled += p.Count
			f.Accepted += p.Accept + p.AcceptCrypto
			f.Rejected += p.Reject + p.RejectCrypto
			f.Lockouts += p.Lockout + p.PermanentLockout
			f.Acquired += p.Acquire + p.AcquireCrypto
		}
	}
	for _, m := range lockoutNow.FindAllSubmatch(out, -1) {
		if string(m[1]) == "true" || string(m[2]) == "true" {
			f.LockedOut = true
		}
	}
	return f, nil
}

// Fingerprints reads the fingerprint service's counters.
func (a *ADB) Fingerprints(ctx context.Context) (Fingerprints, error) {
	out, err := a.Shell(ctx, "dumpsys", "fingerprint")
	if err != nil {
		return Fingerprints{}, err
	}
	return ParseFingerprints(out)
}

// FingerTouch puts a finger on the emulator's sensor and lifts it. The
// console takes a finger by number; enrolling records which ones touched, and
// a number never enrolled is a finger the sensor does not recognize.
func (a *ADB) FingerTouch(ctx context.Context, finger int) error {
	id := fmt.Sprint(finger)
	if err := a.emuConsole(ctx, "finger", "touch", id); err != nil {
		return err
	}
	// Held for as long as a touch takes: lifted at once, the enrolling screen
	// counted some touches and not others.
	select {
	case <-time.After(300 * time.Millisecond):
	case <-ctx.Done():
	}
	return a.emuConsole(context.WithoutCancel(ctx), "finger", "remove", id)
}

// MobiumPIN is the screen lock mobium sets on an emulator that has none,
// because Android enrolls no fingerprint without one. Clearing it removes
// every enrolled fingerprint with it, which is how they are unenrolled.
const MobiumPIN = "1111"

// Screen lock, as enrolling a fingerprint needs to know it.
const (
	LockNone   = "none"   // no credential: mobium may set its own
	LockMobium = "mobium" // mobium's PIN, MobiumPIN
	LockOther  = "other"  // somebody else's, which mobium does not know
)

// ScreenLock says whose screen lock the device has, by asking the lock
// service to check MobiumPIN. Its three answers, on the Pixel 7 AVD, each
// exiting 0: "verified successfully", "didn't match", and "user has no
// password". A bare verify with no --old is no use: with no credential it
// says "verified successfully", and with one it throws.
func (a *ADB) ScreenLock(ctx context.Context) (string, error) {
	out, diag, err := a.ShellDiagnostics(ctx, "cmd", "lock_settings", "verify", "--old", MobiumPIN)
	text := string(out) + string(diag)
	switch {
	case strings.Contains(text, "has no password"):
		return LockNone, nil
	case strings.Contains(text, "verified successfully"):
		return LockMobium, nil
	case strings.Contains(text, "didn't match"):
		return LockOther, nil
	case err != nil:
		return "", err
	}
	return "", mobiumerr.New(mobiumerr.DeviceServer, "could not tell whether the device has a screen lock: %s",
		firstLine(strings.TrimSpace(text)))
}

// SetMobiumPIN sets MobiumPIN as the screen lock, and reads it back.
func (a *ADB) SetMobiumPIN(ctx context.Context) error {
	if _, err := a.Shell(ctx, "locksettings", "set-pin", MobiumPIN); err != nil {
		return err
	}
	if lock, err := a.ScreenLock(ctx); err != nil || lock != LockMobium {
		return mobiumerr.New(mobiumerr.NotConfirmed, "set a PIN on the emulator, and its screen lock reads %q", lock)
	}
	return nil
}

// ClearMobiumPIN removes MobiumPIN, and with it every enrolled fingerprint
// (measured: the count went from 1 to 0). It reads the lock back.
func (a *ADB) ClearMobiumPIN(ctx context.Context) error {
	if _, err := a.Shell(ctx, "locksettings", "clear", "--old", MobiumPIN); err != nil {
		return err
	}
	if lock, err := a.ScreenLock(ctx); err != nil || lock != LockNone {
		return mobiumerr.New(mobiumerr.NotConfirmed, "cleared the emulator's PIN, and its screen lock reads %q", lock)
	}
	return nil
}

// A simulator's biometrics are Darwin notifications, the ones its Features
// menu posts. Enrollment is a notification's state, which reads back.
const (
	simEnrollment = "com.apple.BiometricKit.enrollmentChanged"
	// simFace is Face ID's sensor, which Apple calls Pearl; Touch ID's is
	// fingerTouch.
	simFace   = "com.apple.BiometricKit_Sim.pearl."
	simFinger = "com.apple.BiometricKit_Sim.fingerTouch."
)

// Biometric kinds.
const (
	BiometricFace   = "face"
	BiometricFinger = "fingerprint"
)

// BiometricKind says whether the simulator is a Face ID or a Touch ID model,
// from its device type's capabilities — pearl-id and touch-id. "" means
// neither, which an iPad without a sensor would be.
func (s *Simctl) BiometricKind(ctx context.Context) (string, error) {
	caps, err := s.deviceTypeCapabilities(ctx)
	if err != nil {
		return "", err
	}
	switch {
	case caps["pearl-id"] == true:
		return BiometricFace, nil
	case caps["touch-id"] == true:
		return BiometricFinger, nil
	}
	return "", nil
}

func (s *Simctl) deviceTypeCapabilities(ctx context.Context) (map[string]any, error) {
	out, err := s.Run(ctx, "list", "devices", "-j")
	if err != nil {
		return nil, err
	}
	var devs struct {
		Devices map[string][]struct {
			UDID       string `json:"udid"`
			DeviceType string `json:"deviceTypeIdentifier"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(out, &devs); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "read simctl's device list: %w", err)
	}
	var typ string
	for _, list := range devs.Devices {
		for _, d := range list {
			if d.UDID == s.UDID {
				typ = d.DeviceType
			}
		}
	}
	out, err = s.Run(ctx, "list", "devicetypes", "-j")
	if err != nil {
		return nil, err
	}
	var types struct {
		DeviceTypes []struct {
			Identifier string `json:"identifier"`
			BundlePath string `json:"bundlePath"`
		} `json:"devicetypes"`
	}
	if err := json.Unmarshal(out, &types); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "read simctl's device types: %w", err)
	}
	for _, t := range types.DeviceTypes {
		if t.Identifier != typ || typ == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(t.BundlePath, "Contents", "Resources", "capabilities.plist"))
		if err != nil {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "read the capabilities of %s: %w", typ, err)
		}
		v, err := plist.Unmarshal(data)
		if err != nil {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "read the capabilities of %s: %w", typ, err)
		}
		root, _ := v.(map[string]any)
		caps, _ := root["capabilities"].(map[string]any)
		return caps, nil
	}
	return nil, mobiumerr.New(mobiumerr.DeviceServer, "simctl names no device type for %s", s.UDID)
}

// BiometricEnrolled reads whether the simulator has a face or finger
// enrolled: the enrollment notification's state, 1 or 0.
func (s *Simctl) BiometricEnrolled(ctx context.Context) (bool, error) {
	out, err := s.Run(ctx, "spawn", s.UDID, "notifyutil", "-g", simEnrollment)
	if err != nil {
		return false, err
	}
	f := strings.Fields(string(out))
	if len(f) != 2 || f[0] != simEnrollment {
		return false, mobiumerr.New(mobiumerr.DeviceServer, "could not read the simulator's enrollment: %q",
			strings.TrimSpace(string(out)))
	}
	return f[1] != "0", nil
}

// SetBiometricEnrolled enrolls or unenrolls, as the Features menu does: the
// state is set, then posted so a running app hears of it — MobiumApp's
// Biometrics Demo read the change within a second, with no relaunch. It is
// read back.
func (s *Simctl) SetBiometricEnrolled(ctx context.Context, on bool) error {
	state := "0"
	if on {
		state = "1"
	}
	if _, err := s.Run(ctx, "spawn", s.UDID, "notifyutil", "-s", simEnrollment, state); err != nil {
		return err
	}
	if _, err := s.Run(ctx, "spawn", s.UDID, "notifyutil", "-p", simEnrollment); err != nil {
		return err
	}
	got, err := s.BiometricEnrolled(ctx)
	if err != nil {
		return err
	}
	if got != on {
		return mobiumerr.New(mobiumerr.NotConfirmed, "set the simulator's enrollment to %v, and it reads %v", on, got)
	}
	return nil
}

// PresentBiometric shows the simulator's sensor a matching face or finger,
// or one that does not match. kind is BiometricFace or BiometricFinger.
func (s *Simctl) PresentBiometric(ctx context.Context, kind string, match bool) error {
	name := simFace
	if kind == BiometricFinger {
		name = simFinger
	}
	if match {
		name += "match"
	} else {
		name += "nomatch"
	}
	_, err := s.Run(ctx, "spawn", s.UDID, "notifyutil", "-p", name)
	return err
}
