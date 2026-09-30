package device

import (
	"os"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Captured on the Pixel 7 AVD, Android 15, after a print was enrolled, used
// and unenrolled: none enrolled, and the counters kept since boot.
func TestParseFingerprints(t *testing.T) {
	data, err := os.ReadFile("testdata/dumpsys-fingerprint-api35.txt")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFingerprints(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Enrolled != 0 || f.Accepted == 0 || f.Rejected == 0 || f.Acquired < f.Accepted+f.Rejected {
		t.Errorf("counters read wrong: %+v", f)
	}
	if f.LockedOut {
		t.Error("read a lockout the capture does not have")
	}
}

// The crypto counters are an app asking with a CryptoObject, counted apart
// by the service; an answer is either. And the live lockout is its own line.
func TestParseFingerprintsSumsCryptoAndReadsLockout(t *testing.T) {
	out := []byte(`Dumping for sensorId: 0, provider: FingerprintProvider
{"service":"FingerprintProvider\/default","prints":[{"id":0,"count":2,"accept":1,"reject":2,"acquire":3,"lockout":0,"permanentLockout":0,"acceptCrypto":4,"rejectCrypto":1,"acquireCrypto":5}]}
UserId=0, {(BIOMETRIC_STRONG, permanentLockout=false, timedLockout=true), (BIOMETRIC_WEAK, permanentLockout=false, timedLockout=false)}
`)
	f, err := ParseFingerprints(out)
	if err != nil {
		t.Fatal(err)
	}
	want := Fingerprints{Enrolled: 2, Accepted: 5, Rejected: 3, Acquired: 8, LockedOut: true}
	if f != want {
		t.Errorf("got %+v, want %+v", f, want)
	}
}

func TestParseFingerprintsWithNoSensor(t *testing.T) {
	_, err := ParseFingerprints([]byte("Can't find service: fingerprint\n"))
	if mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
		t.Errorf("no sensor: %v", err)
	}
}
