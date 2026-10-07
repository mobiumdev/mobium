package agent

import (
	"os"
	"strings"
	"testing"
	"time"
)

// CHALLENGES 280: a capture lost with its daemon was answered as never
// started. The note is what lets the next daemon say otherwise.
func TestLostAudio(t *testing.T) {
	t.Setenv("MOBIUM_HOME", t.TempDir())
	t.Setenv("MOBIUM_SESSION", "")
	const dev = "emulator-5554"

	if got := lostAudio(dev); got != "" {
		t.Errorf("no capture was started, and lostAudio said %q", got)
	}

	started := time.Now().Add(-3 * time.Second)
	writeAudioNote(dev, audioNote{PID: os.Getpid() + 100000, Started: started})
	if got := lostAudio(dev); !strings.Contains(got, "ended without stopping it") || !strings.Contains(got, started.Format("15:04:05")) {
		t.Errorf("a capture left by a daemon that is gone: %q", got)
	}

	endAudioNote(dev, "the daemon was stopped")
	if got := lostAudio(dev); !strings.HasSuffix(got, "was lost: the daemon was stopped") {
		t.Errorf("a capture its daemon discarded on the way down: %q", got)
	}

	writeAudioNote(dev, audioNote{PID: os.Getpid(), Started: started})
	if got := lostAudio(dev); !strings.HasSuffix(got, "its session was closed") {
		t.Errorf("a capture this daemon holds no more: %q", got)
	}

	removeAudioNote(dev)
	if got := lostAudio(dev); got != "" {
		t.Errorf("a stopped capture still reads as lost: %q", got)
	}

	// One session's note is not another's: a check's own daemon does not
	// report the default session's capture as lost.
	writeAudioNote(dev, audioNote{PID: 1, Started: started})
	t.Setenv("MOBIUM_SESSION", "ck1")
	if got := lostAudio(dev); got != "" {
		t.Errorf("another session's capture was reported here: %q", got)
	}
}
