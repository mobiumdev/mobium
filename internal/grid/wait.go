package grid

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/paths"
)

// WaitTTL is how long a waiting note lasts untouched. A queued run touches
// its note every two seconds, so one older than this belongs to a run that
// stopped waiting — got a device elsewhere, gave up, or died.
const WaitTTL = 15 * time.Second

// Waiting is a run queued for a device, as a node heard it: who, what it
// asked for, and since when.
type Waiting struct {
	Holder string    `json:"holder"`
	Want   string    `json:"want"`
	Since  time.Time `json:"since"`
}

func waitDir() (string, error) {
	d := filepath.Join(paths.Root(), "waiting")
	return d, os.MkdirAll(d, 0o700)
}

// Wait records, or refreshes, a run waiting for a device. Nothing routes by
// it: it is there so a grid's view can show the queue, which otherwise lives
// only in each waiting run's own process.
func Wait(holder, want string) error {
	d, err := waitDir()
	if err != nil {
		return err
	}
	f := filepath.Join(d, Key(holder))
	if _, err := os.Stat(f); err == nil {
		now := time.Now()
		return os.Chtimes(f, now, now)
	}
	return os.WriteFile(f, []byte(want+"\n"+time.Now().UTC().Format(time.RFC3339)), 0o600)
}

// Unwait removes a run's waiting note.
func Unwait(holder string) error {
	d, err := waitDir()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(d, Key(holder))); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Queue lists the runs waiting now, oldest first.
func Queue() ([]Waiting, error) {
	d, err := waitDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		return nil, err
	}
	var out []Waiting
	for _, e := range entries {
		f := filepath.Join(d, e.Name())
		info, err := os.Stat(f)
		if err != nil || time.Since(info.ModTime()) >= WaitTTL {
			continue
		}
		b, _ := os.ReadFile(f)
		parts := strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)
		w := Waiting{Holder: e.Name(), Want: parts[0], Since: info.ModTime()}
		if len(parts) == 2 {
			if t, err := time.Parse(time.RFC3339, parts[1]); err == nil {
				w.Since = t
			}
		}
		out = append(out, w)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Since.Before(out[j-1].Since); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}
