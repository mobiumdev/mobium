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

func audioNotePath(serial string) string {
	name := paths.SessionName()
	if name == "" {
		name = "default"
	}
	// SessionDir's last segment is the serial made safe for a path.
	return filepath.Join(paths.DaemonDir(), "audio-"+name+"-"+filepath.Base(paths.SessionDir(serial))+".json")
}

func writeAudioNote(serial string, n audioNote) {
	data, err := json.Marshal(n)
	if err != nil {
		return
	}
	if err := os.MkdirAll(paths.DaemonDir(), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(audioNotePath(serial), data, 0o600)
}

func removeAudioNote(serial string) { _ = os.Remove(audioNotePath(serial)) }

// lostAudio says what became of a capture on this device that this daemon
// does not hold, or "" when none was lost.
func lostAudio(serial string) string {
	data, err := os.ReadFile(audioNotePath(serial))
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
	return fmt.Sprintf("the capture started at %s, %s ago, was lost: %s", n.Started.Format("15:04:05"),
		time.Since(n.Started).Round(time.Second), why)
}

// endAudioNote records why a capture ended other than by a stop.
func endAudioNote(serial, why string) {
	data, err := os.ReadFile(audioNotePath(serial))
	if err != nil {
		return
	}
	var n audioNote
	if json.Unmarshal(data, &n) != nil {
		return
	}
	n.Why = why
	writeAudioNote(serial, n)
}
