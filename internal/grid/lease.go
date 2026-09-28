// Package grid holds a mobium grid's leases: the record, on a node, of which
// run a device belongs to. The caller's mobium takes and renews them over
// SSH, and every daemon on the node reads them, so a device leased to one run
// is refused to every other — a run started through the grid, or not.
package grid

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/paths"
)

// TTL is how long a lease lasts unrenewed; a holder renews well inside it,
// so a lease past it belongs to a run that stopped, most likely by dying.
const TTL = 60 * time.Second

// Answer is what taking a lease says: whether it was given, and to whom.
type Answer struct {
	OK     bool   `json:"ok"`
	Holder string `json:"holder"`
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// Key is a serial as a lease file names it: nothing in it reaches the
// filesystem as a path.
func Key(serial string) string { return unsafe.ReplaceAllString(serial, "_") }

func dir() (string, error) {
	d := filepath.Join(paths.Root(), "leases")
	return d, os.MkdirAll(d, 0o700)
}

func file(serial string) (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, Key(serial)), nil
}

// Take gives a device to a holder: a new lease if there is none or the last
// one lapsed, a renewal if the holder already has it, and a refusal naming
// the holder otherwise. The file is created exclusively, so two callers
// asking at once cannot both be told yes.
func Take(serial, holder string) (Answer, error) {
	f, err := file(serial)
	if err != nil {
		return Answer{}, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		if w, err := os.OpenFile(f, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err == nil {
			_, err = w.WriteString(holder)
			w.Close()
			return Answer{OK: err == nil, Holder: holder}, err
		}
		held, info, err := read(f)
		if err != nil {
			continue // released between the two looks; try again
		}
		if held == holder {
			now := time.Now()
			return Answer{OK: true, Holder: holder}, os.Chtimes(f, now, now)
		}
		if time.Since(info.ModTime()) < TTL {
			return Answer{OK: false, Holder: held}, nil
		}
		_ = os.Remove(f) // lapsed
	}
	return Answer{}, mobiumerr.New(mobiumerr.DeviceServer, "could not take the lease on %s", serial)
}

// Drop releases a device, if the holder is the one that has it.
func Drop(serial, holder string) error {
	f, err := file(serial)
	if err != nil {
		return err
	}
	if held, _, err := read(f); err == nil && held == holder {
		return os.Remove(f)
	}
	return nil
}

// Holder names the run a device is leased to, if a live lease says so.
func Holder(serial string) (string, bool) {
	f, err := file(serial)
	if err != nil {
		return "", false
	}
	held, info, err := read(f)
	if err != nil || time.Since(info.ModTime()) >= TTL {
		return "", false
	}
	return held, true
}

// Live maps each leased device's key to its holder, live leases only.
func Live() (map[string]string, error) {
	d, err := dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		held, info, err := read(filepath.Join(d, e.Name()))
		if err == nil && time.Since(info.ModTime()) < TTL {
			out[e.Name()] = held
		}
	}
	return out, nil
}

func read(f string) (string, os.FileInfo, error) {
	info, err := os.Stat(f)
	if err != nil {
		return "", nil, err
	}
	b, err := os.ReadFile(f)
	return strings.TrimSpace(string(b)), info, err
}
