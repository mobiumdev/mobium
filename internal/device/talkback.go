package device

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// What TalkBack says, and moving its cursor from outside.
//
// TalkBack's cursor cannot be moved by an injected event: `input` touches pass
// through it as clicks, `input keycombination` Alt+Right reaches no TalkBack
// shortcut, and writing to the touchscreen with sendevent is refused by
// SELinux without root. A keyboard can: TalkBack moves to the next item on a
// real keyboard's Alt+Right, and Android's own `hid` command makes one — a USB
// keyboard over /dev/uhid, which the shell user may open (group uhid) on a
// user build. Measured on the Android 15 emulator: it registers in about a
// tenth of a second, three presses moved the cursor three items, and it is
// gone from `dumpsys input` once `hid` reads the end of its input.
//
// What TalkBack says is in no tree. With its Developer settings' "Log output
// level" at VERBOSE it logs each utterance as `Speaking fragment text="…",
// utteranceId=talkback_N`, with the event that caused it; the one a move
// causes is TYPE_VIEW_ACCESSIBILITY_FOCUSED, about 70ms after the press. At
// the default, ERROR, nothing of it is logged.

// TalkBackOn reports whether TalkBack is bound with touch exploration on —
// what the accessibility manager says, not the setting.
func (a *ADB) TalkBackOn(ctx context.Context) (bool, error) {
	ok, err := a.talkBackInstalled(ctx)
	if err != nil || !ok {
		return false, err
	}
	bound, exploring, err := a.talkBackState(ctx)
	return bound && exploring, err
}

// Keyboard is a USB keyboard made with `hid`, for as long as it is open.
type Keyboard struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *SyncBuffer
}

// keyboardName is what the keyboard is called in `dumpsys input`.
const keyboardName = "Mobium keyboard"

// keyboardDescriptor is a boot keyboard: a modifier byte, a reserved byte and
// six key codes, with no LEDs.
var keyboardDescriptor = []int{
	0x05, 0x01, 0x09, 0x06, 0xa1, 0x01, 0x05, 0x07, 0x19, 0xe0, 0x29, 0xe7, 0x15, 0x00, 0x25, 0x01, 0x75, 0x01,
	0x95, 0x08, 0x81, 0x02, 0x95, 0x01, 0x75, 0x08, 0x81, 0x01, 0x95, 0x06, 0x75, 0x08, 0x15, 0x00, 0x25, 0x65,
	0x05, 0x07, 0x19, 0x00, 0x29, 0x65, 0x81, 0x00, 0xc0,
}

// HID usages and the modifier bit Keyboard.Press takes.
const (
	KeyRight = 0x4F
	KeyLeft  = 0x50
	ModAlt   = 0x04
)

// keyboardWait bounds waiting for Android to add the keyboard.
const keyboardWait = 5 * time.Second

// OpenKeyboard makes a USB keyboard on the device and waits for Android to
// add it. The caller must Close it: while it is there Android counts a
// hardware keyboard, and the on-screen one stays down.
func (a *ADB) OpenKeyboard(ctx context.Context) (*Keyboard, error) {
	cmd := exec.Command(a.Path, a.args("shell", "hid", "-")...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out := &SyncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start hid: %w", err)
	}
	k := &Keyboard{cmd: cmd, stdin: stdin, out: out}
	if err := k.send(map[string]any{
		"id": 1, "command": "register", "name": keyboardName, "vid": 0x18d1, "pid": 0x4ee7, "bus": "usb",
		"descriptor": keyboardDescriptor,
	}); err != nil {
		k.Close()
		return nil, err
	}
	deadline := time.Now().Add(keyboardWait)
	for {
		dump, err := a.Shell(ctx, "dumpsys", "input")
		if err == nil && bytes.Contains(dump, []byte(keyboardName)) {
			return k, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			why := strings.TrimSpace(out.String())
			k.Close()
			if why == "" {
				why = "it never appeared in dumpsys input"
			}
			return nil, mobiumerr.New(mobiumerr.Unsupported, "Mobium moves the screen reader's cursor with a "+
				"keyboard made by Android's hid command, and none could be made on this device: %s", why)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Press presses key with the modifier held, and lets go of both.
func (k *Keyboard) Press(mod, key int) error {
	for _, report := range [][]int{{mod, 0, 0, 0, 0, 0, 0, 0}, {mod, 0, key, 0, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 0, 0, 0}} {
		if err := k.send(map[string]any{"id": 1, "command": "report", "report": report}); err != nil {
			return err
		}
		time.Sleep(30 * time.Millisecond)
	}
	return nil
}

func (k *Keyboard) send(cmd map[string]any) error {
	b, _ := json.Marshal(cmd)
	if _, err := k.stdin.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("the keyboard went away: %w (%s)", err, strings.TrimSpace(k.out.String()))
	}
	return nil
}

// Close ends hid, which takes the keyboard away.
func (k *Keyboard) Close() {
	_ = k.stdin.Close()
	done := make(chan struct{})
	go func() { _ = k.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = k.cmd.Process.Kill()
		<-done
	}
}

// Spoken is one thing TalkBack said.
type Spoken struct {
	// At is when it was logged, by the device's clock, in milliseconds. It
	// is what tells two utterances apart: TalkBack numbers them from 1 again
	// each time it is turned on, in the same process — measured, process
	// 3147 before and after — and the log keeps the last time's.
	At  int64
	PID int
	ID  int
	// Text is what was said, the fragments of one utterance joined.
	Text string
	// Focus says a move of the cursor caused it, rather than an
	// announcement or a change on the screen.
	Focus bool
	// Event is what caused it, as TalkBack logs it — TYPE_ANNOUNCEMENT,
	// TYPE_WINDOW_CONTENT_CHANGED for a live region — or "" when it logged
	// none ("TalkBack on").
	Event string
}

// Key tells one utterance from every other.
func (s Spoken) Key() [3]int64 { return [3]int64{s.At, int64(s.PID), int64(s.ID)} }

// spokenRe reads TalkBack's verbose speech line in logcat's epoch format:
// seconds.millis, pid, tid, level, then the message.
var spokenRe = regexp.MustCompile(`^\s*(\d+)\.(\d{3})\s+(\d+)\s+\d+\s+V\s.*?Speaking fragment text="(.*?)", ` +
	`utteranceId=talkback_(\d+), .*?event=(.*)$`)

// subtypeRe reads the event's type out of TalkBack's event description.
var subtypeRe = regexp.MustCompile(`subtype:([A-Z_]+)`)

// TalkBackSpoken reads what TalkBack logged it said, oldest first. None at
// all is TalkBack's log output level short of VERBOSE: it logs its speech
// at no other, and it speaks as it comes on ("TalkBack on"). The lines are
// picked by their message — their tag is "talkback: SpeechControllerImpl",
// which logcat's tag filter does not match as "talkback".
func (a *ADB) TalkBackSpoken(ctx context.Context) ([]Spoken, error) {
	spoken, _, err := a.TalkBackHeard(ctx)
	return spoken, err
}

// TalkBackHeard is TalkBackSpoken, and when TalkBack last began a word, by
// the device's clock in milliseconds: it logs each word as it speaks it, so
// a word logged since the last read means it is still talking. A sentence is
// logged when it is queued, not when it is said — a live region's polite
// change waited about five seconds behind a paragraph TalkBack was reading.
func (a *ADB) TalkBackHeard(ctx context.Context) (spoken []Spoken, lastWord int64, err error) {
	// Quoted: adb shell hands the line to the device's shell, where the bar
	// is a pipe and the space splits the pattern in two.
	out, err := a.Shell(ctx, "logcat", "-d", "-v", "epoch", "-e", shellQuote("Speaking fragment|onFragmentRangeStarted"))
	if err != nil {
		return nil, 0, err
	}
	return parseTalkBackHeard(out)
}

// wordRe reads the line TalkBack logs as it begins saying a word.
var wordRe = regexp.MustCompile(`^\s*(\d+)\.(\d{3})\s.*onFragmentRangeStarted`)

func parseTalkBackSpoken(out []byte) ([]Spoken, error) {
	spoken, _, err := parseTalkBackHeard(out)
	return spoken, err
}

func parseTalkBackHeard(out []byte) ([]Spoken, int64, error) {
	var spoken []Spoken
	var lastWord int64
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if w := wordRe.FindStringSubmatch(line); w != nil {
			sec, _ := strconv.ParseInt(w[1], 10, 64)
			ms, _ := strconv.ParseInt(w[2], 10, 64)
			lastWord = sec*1000 + ms
			continue
		}
		m := spokenRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sec, _ := strconv.ParseInt(m[1], 10, 64)
		ms, _ := strconv.ParseInt(m[2], 10, 64)
		pid, _ := strconv.Atoi(m[3])
		id, _ := strconv.Atoi(m[5])
		event := ""
		if e := subtypeRe.FindStringSubmatch(m[6]); e != nil {
			event = e[1]
		}
		focus := event == "TYPE_VIEW_ACCESSIBILITY_FOCUSED"
		// One utterance's fragments are logged one after another.
		if n := len(spoken); n > 0 && spoken[n-1].PID == pid && spoken[n-1].ID == id {
			spoken[n-1].Text = strings.TrimSpace(spoken[n-1].Text + " " + m[4])
			spoken[n-1].Focus = spoken[n-1].Focus || focus
			continue
		}
		spoken = append(spoken, Spoken{At: sec*1000 + ms, PID: pid, ID: id, Text: m[4], Focus: focus, Event: event})
	}
	return spoken, lastWord, sc.Err()
}

// ErrTalkBackQuiet refuses reading TalkBack's speech while its log output
// level is not VERBOSE.
var ErrTalkBackQuiet = mobiumerr.New(mobiumerr.DeviceNotReady, "TalkBack logs what it says only with its log "+
	"output level at VERBOSE, and it is not — so what it says cannot be read").
	WithRemedy("in TalkBack's settings, open Advanced settings, then Developer settings, set Log output level to " +
		"VERBOSE and confirm; verbose logging puts everything TalkBack says in the device log, so set it back to " +
		"ERROR afterward")
