package agent

import (
	"testing"
	"time"
)

// A trace outlives the session recording it: replacing a session — on a Fire
// TV, because its Wi-Fi link dropped and came back — hands the trace to the
// next session on the same device, and to no other.
func TestATraceOutlivesItsSession(t *testing.T) {
	h := NewHandlers()
	tr := &sessionTrace{started: time.Now()}
	old := &session{dev: fakeDevice(), driver: &buttonDriver{}, trace: tr}
	h.sessions["tv"] = old

	h.retire("tv", old, "the device stopped answering")
	if _, still := h.sessions["tv"]; still || old.trace != nil {
		t.Fatal("the retired session is still cached, or still holds the trace")
	}

	other := &session{dev: fakeDevice(), driver: &buttonDriver{}}
	h.adopt("phone", other)
	if other.trace != nil {
		t.Error("a session on another device took the trace")
	}

	next := &session{dev: fakeDevice(), driver: &buttonDriver{}}
	h.adopt("tv", next)
	if next.trace != tr {
		t.Error("the next session on the device did not carry the trace on")
	}
	if len(h.heldTraces) != 0 {
		t.Error("the trace is still held after it was carried on")
	}
}
