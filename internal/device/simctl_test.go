package device

import (
	"strings"
	"testing"
)

const simctlJSON = `{
  "devices": {
    "com.apple.CoreSimulator.SimRuntime.iOS-18-0": [
      {"udid":"AAA-1","name":"iPhone 16","state":"Shutdown","isAvailable":true},
      {"udid":"AAA-2","name":"iPhone 16 Pro","state":"Booted","isAvailable":true},
      {"udid":"AAA-3","name":"Broken","state":"Shutdown","isAvailable":false}
    ],
    "com.apple.CoreSimulator.SimRuntime.iOS-17-5": [
      {"udid":"BBB-1","name":"iPhone 15","state":"Shutdown","isAvailable":true}
    ]
  }
}`

func TestParseSimulators(t *testing.T) {
	sims, err := parseSimulators([]byte(simctlJSON))
	if err != nil {
		t.Fatalf("parseSimulators: %v", err)
	}
	// The unavailable runtime entry must be dropped.
	if len(sims) != 3 {
		t.Fatalf("parsed %d simulators, want 3: %+v", len(sims), sims)
	}
	// Booted first: offering something that needs a 30-second boot ahead of a
	// running simulator is the wrong default.
	if sims[0].UDID != "AAA-2" || !sims[0].Booted() {
		t.Errorf("first simulator = %+v, want the booted one", sims[0])
	}
	// Then newest runtime.
	if sims[1].Runtime != "iOS 18.0" {
		t.Errorf("second simulator runtime = %q", sims[1].Runtime)
	}
	for _, s := range sims {
		if s.Name == "Broken" {
			t.Error("an unavailable simulator was listed")
		}
	}
}

func TestShortRuntime(t *testing.T) {
	tests := []struct{ in, want string }{
		{"com.apple.CoreSimulator.SimRuntime.iOS-18-0", "iOS 18.0"},
		{"com.apple.CoreSimulator.SimRuntime.iOS-17-5", "iOS 17.5"},
		{"com.apple.CoreSimulator.SimRuntime.watchOS-11-0", "watchOS 11.0"},
		{"nonsense", "nonsense"},
	}
	for _, tc := range tests {
		if got := shortRuntime(tc.in); got != tc.want {
			t.Errorf("shortRuntime(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseSimulatorsRejectsGarbage(t *testing.T) {
	if _, err := parseSimulators([]byte("not json")); err == nil {
		t.Error("expected an error for non-JSON input")
	}
}

func TestErrNoXcodeIsActionable(t *testing.T) {
	// Under CLT-only, `xcrun simctl` fails with "unable to find utility",
	// which reads like a PATH problem. This message is the whole recovery
	// path, so it has to name the real cause.
	msg := ErrNoXcode.Error()
	for _, want := range []string{"Command Line Tools", "simctl", "xcode-select"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ErrNoXcode does not mention %q: %v", want, msg)
		}
	}
}

func TestWDAArtifactIsPinnedAndChecksummed(t *testing.T) {
	if len(wdaSimArm64.sha256) != 64 {
		t.Errorf("WDA checksum is %d chars", len(wdaSimArm64.sha256))
	}
	if !strings.Contains(wdaSimArm64.url, WDAVersion) {
		t.Errorf("WDA url %q is not pinned to a version", wdaSimArm64.url)
	}
	if !strings.HasPrefix(wdaSimArm64.url, "https://") {
		t.Error("WDA is not fetched over https")
	}
	if !strings.Contains(wdaSimArm64.url, "Sim") {
		t.Errorf("WDA url %q is not the simulator build", wdaSimArm64.url)
	}
}
