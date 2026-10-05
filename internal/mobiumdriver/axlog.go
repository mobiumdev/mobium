package mobiumdriver

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/paths"
)

// axLog appends a line to accessibility.log in mobium's state directory:
// every read of a phone's accessibility setting, what a change recorded it
// was, every change and every restore, with the time and the phone.
//
// A phone once ended a run with Reduce Motion the opposite of what the run
// found, and nothing kept could say which read was wrong (ROADMAP's open
// lead, #118). The restore runs inside the daemon as the session ends, where
// no caller is watching, so this is a file rather than verbose output. It
// holds setting names and on or off, never anything the phone shows, and it
// never fails the action it records: a log that cannot be written is
// skipped. CHALLENGES 243.
func axLog(phone *device.Devicectl, format string, args ...interface{}) {
	who := "phone"
	if phone != nil && phone.Phone.UDID != "" {
		who = phone.Phone.UDID
	}
	line := fmt.Sprintf("%s %s %s\n", time.Now().Format(time.RFC3339), who, fmt.Sprintf(format, args...))
	axLogMu.Lock()
	defer axLogMu.Unlock()
	path := filepath.Join(paths.Root(), "accessibility.log")
	// Kept to about a megabyte: the newest half-life of it is what a
	// recurrence needs.
	if info, err := os.Stat(path); err == nil && info.Size() > axLogMax {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

var axLogMu sync.Mutex

const axLogMax = 1 << 20

// errNote is ", failed: <err>" for a step that failed, and nothing otherwise.
func errNote(err error) string {
	if err == nil {
		return ""
	}
	return ", failed: " + err.Error()
}
