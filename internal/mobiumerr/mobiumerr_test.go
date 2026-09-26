package mobiumerr

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestCodeOfSeesThroughWrapping(t *testing.T) {
	base := New(NoSuchElement, "no element matches text=Go")
	wrapped := fmt.Errorf("tapping: %w", base)
	if CodeOf(wrapped) != NoSuchElement {
		t.Errorf("code lost through %%w: %s", CodeOf(wrapped))
	}
	// An error nobody classified is Unclassified, never empty.
	if CodeOf(errors.New("plain")) != Unclassified {
		t.Error("a plain error was not Unclassified")
	}
	if CodeOf(nil) != "" {
		t.Error("nil has a code")
	}
}

func TestKindIsASentinelForTheWholeCode(t *testing.T) {
	err := fmt.Errorf("x: %w", New(NoDevice, "no device with serial %q", "nope"))
	if !errors.Is(err, Kind(NoDevice)) {
		t.Error("errors.Is did not match the code")
	}
	if errors.Is(err, Kind(Timeout)) {
		t.Error("errors.Is matched a different code")
	}
	// Two concrete errors with the same code are still different errors.
	if errors.Is(New(NoDevice, "a"), New(NoDevice, "b")) {
		t.Error("two messages compared equal")
	}
}

func TestWrapKeepsTheCause(t *testing.T) {
	cause := errors.New("connection refused")
	e := Wrap(DeviceServer, cause, "")
	if e.Error() != "connection refused" || !errors.Is(e, cause) {
		t.Errorf("wrap = %q, cause kept = %v", e, errors.Is(e, cause))
	}
}

func TestPayloadRoundTrip(t *testing.T) {
	e := New(Timeout, "timed out after 10s waiting for label=Go").
		WithRemedy("raise --timeout, or check the locator with map").
		WithDetail("waited_ms", 10000)
	b, _ := json.Marshal(PayloadOf(fmt.Errorf("wait: %w", e)))
	var p Payload
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	back := FromPayload(p)
	if back.Code != Timeout || !back.Retryable || back.Remedy == "" || back.Details["waited_ms"] != float64(10000) {
		t.Errorf("round trip lost something: %+v", back)
	}
	// The message is the whole error text, wrapping included, so the text
	// channel and the structured one say the same thing.
	if p.Message != "wait: timed out after 10s waiting for label=Go" {
		t.Errorf("message = %q", p.Message)
	}
	if PayloadOf(errors.New("plain")).Code != Unclassified {
		t.Error("a plain error crossed the wire without a code")
	}
}

func TestEveryCodeHasAnExitStatusGroup(t *testing.T) {
	seen := map[Code]bool{}
	for _, c := range Codes {
		if seen[c] {
			t.Errorf("%s listed twice", c)
		}
		seen[c] = true
		if s := ExitCode(c); s < 1 || s > 7 {
			t.Errorf("%s exits %d", c, s)
		}
	}
	if ExitCode(NoSuchElement) == ExitCode(Unsupported) {
		t.Error("element failures and unsupported share an exit status")
	}
}

func TestNewKeepsAWrappedCause(t *testing.T) {
	cause := New(DeviceServer, "invalid session id")
	e := New(Timeout, "waited for the server: %w", cause)
	if e.Error() != "waited for the server: invalid session id" {
		t.Errorf("message = %q", e.Error())
	}
	if !errors.Is(e, cause) {
		t.Error("the %%w cause was dropped")
	}
	// The outer code wins: this failure is a timeout, whatever it wraps.
	if CodeOf(e) != Timeout {
		t.Errorf("code = %s", CodeOf(e))
	}
}

func TestNames(t *testing.T) {
	for c, want := range map[Code]string{
		NoSuchElement: "NoSuchElement", NotConfirmed: "NotConfirmed",
		Timeout: "TimedOut", DeviceServer: "DeviceServer", Unclassified: "",
	} {
		if got := Name(c); got != want {
			t.Errorf("Name(%s) = %q, want %q", c, got, want)
		}
	}
}
