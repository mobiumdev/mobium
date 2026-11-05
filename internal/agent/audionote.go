package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mobiumdev/mobium/internal/paths"
)

// A capture lives in the memory of the daemon that started it, so it ends
// with that daemon, or with the session that holds it. A daemon stopped or
// killed mid-capture left the next one answering stop with "no audio is
// being captured — start with action "start"": the wrong cause, and a
// remedy that recovers nothing, while status said "not capturing audio" as
// if none had been started. CHALLENGES 280.
//
// So a running capture leaves a note beside the daemon's socket, named for
// the session and the device, which only the same session's next daemon
// reads. A stop or a new start removes it; whatever else ends the capture
// writes down why.
type audioNote struct {
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
	Why     string    `json:"why,omitempty"`
}

// A screen recording and a trace live in the daemon too, and leave the same
// note. CHALLENGES 388.
const (
	audioNoteKind     = "audio"
	recordingNoteKind = "recording"
	traceNoteKind     = "trace"
)

func notePath(kind, serial string) string {
	name := paths.SessionName()
	if name == "" {
		name = "default"
	}
	// SessionDir's last segment is the serial made safe for a path.
	return filepath.Join(paths.DaemonDir(), kind+"-"+name+"-"+filepath.Base(paths.SessionDir(serial))+".json")
}

func writeAudioNote(serial string, n audioNote) { writeNote(audioNoteKind, serial, n) }

func writeNote(kind, serial string, n audioNote) {
	data, err := json.Marshal(n)
	if err != nil {
		return
	}
	if err := os.MkdirAll(paths.DaemonDir(), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(notePath(kind, serial), data, 0o600)
}

func removeAudioNote(serial string) { removeNote(audioNoteKind, serial) }

func removeNote(kind, serial string) { _ = os.Remove(notePath(kind, serial)) }

// lostAudio says what became of a capture on this device that this daemon
// does not hold, or "" when none was lost.
func lostAudio(serial string) string { return lostNote(audioNoteKind, "capture", serial) }

func lostNote(kind, what, serial string) string {
	data, err := os.ReadFile(notePath(kind, serial))
	if err != nil {
		return ""
	}
	var n audioNote
	if json.Unmarshal(data, &n) != nil || n.Started.IsZero() {
		return ""
	}
	why := n.Why
	switch {
	case why != "":
	case n.PID != os.Getpid():
		why = fmt.Sprintf("the daemon holding it (pid %d) ended without stopping it", n.PID)
	default:
		why = "its session was closed"
	}
	return fmt.Sprintf("the %s started at %s, %s ago, was lost: %s", what, n.Started.Format("15:04:05"),
		time.Since(n.Started).Round(time.Second), why)
}

// endAudioNote records why a capture ended other than by a stop.
func endAudioNote(serial, why string) { endNote(audioNoteKind, serial, why) }

func endNote(kind, serial, why string) {
	data, err := os.ReadFile(notePath(kind, serial))
	if err != nil {
		return
	}
	var n audioNote
	if json.Unmarshal(data, &n) != nil {
		return
	}
	n.Why = why
	writeNote(kind, serial, n)
}
