package device

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// GrayBox is an app's own account of when it is busy, read from the device
// log. An app that links Mobium's gray-box library and was launched with
// GrayBoxArgument writes one line for each change, under GrayBoxPrefix:
//
//	MOBIUM-GRAYBOX on                 the library is listening
//	MOBIUM-GRAYBOX busy=1 tag=fetch   work started; 1 thing in flight
//	MOBIUM-GRAYBOX busy=0 tag=fetch   that work finished, and is on screen
//	MOBIUM-GRAYBOX lift               a finger came up
//
// The app says when it is busy; nothing here guesses. Without the argument
// the library writes nothing, so an app launched the ordinary way is driven
// as any other.
//
// Measured on an iPhone 15 Plus on iOS 26.6.2, over syslog_relay: a line
// arrives 1 to 4ms after the app writes it, and a busy line the app wrote
// for a tap came 0 to 1ms after that tap's lift.
type GrayBox struct {
	mu      sync.Mutex
	changed chan struct{}
	on      bool
	busy    int
	// tags counts what the app said it is busy with, by tag, so a wait
	// that runs out can name it.
	tags map[string]int
	lift time.Time
}

// GrayBoxPrefix starts every line the gray-box library writes.
const GrayBoxPrefix = "MOBIUM-GRAYBOX "

// GrayBoxArgument is what an app is launched with to turn the library on.
// iOS keeps a launch argument in UserDefaults' argument domain for that
// launch only, so it is never remembered.
var GrayBoxArgument = []string{"-MobiumGrayBox", "YES"}

// GrayBoxSubsystem is the os_log subsystem the iOS library writes under.
const GrayBoxSubsystem = "dev.mobium.graybox"

// grayBoxGrace is how long after a finger lifts work it started can still
// be unannounced. The app writes its busy line as the tap is handled,
// measured at 0 to 1ms after the lift, so this is a wide margin.
const grayBoxGrace = 150 * time.Millisecond

// grayBoxDelivery is how long a line can take to reach Mobium after the app
// writes it: measured at 1 to 4ms on a phone.
const grayBoxDelivery = 50 * time.Millisecond

// NewGrayBox returns a gray box that has heard nothing yet.
func NewGrayBox() *GrayBox {
	return &GrayBox{changed: make(chan struct{}), tags: map[string]int{}}
}

// Reset forgets everything heard, for an app launched again.
func (g *GrayBox) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.on, g.busy, g.tags, g.lift = false, 0, map[string]int{}, time.Time{}
	g.notify()
}

func (g *GrayBox) notify() {
	close(g.changed)
	g.changed = make(chan struct{})
}

// Feed takes one log message, received at at. Anything that is not a
// gray-box line is ignored.
func (g *GrayBox) Feed(msg string, at time.Time) {
	i := strings.Index(msg, GrayBoxPrefix)
	if i < 0 {
		return
	}
	fields := strings.Fields(msg[i+len(GrayBoxPrefix):])
	if len(fields) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case fields[0] == "on":
	case fields[0] == "lift":
		g.lift = at
	case strings.HasPrefix(fields[0], "busy="):
		n, err := strconv.Atoi(strings.TrimPrefix(fields[0], "busy="))
		if err != nil || n < 0 {
			return
		}
		tag := "work"
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "tag=") && len(f) > 4 {
				tag = f[4:]
			}
		}
		if n > g.busy {
			g.tags[tag] += n - g.busy
		} else if n < g.busy && g.tags[tag] > 0 {
			g.tags[tag]--
			if g.tags[tag] == 0 {
				delete(g.tags, tag)
			}
		}
		if n == 0 {
			g.tags = map[string]int{}
		}
		g.busy = n
	default:
		return
	}
	g.on = true
	g.notify()
}

// On reports whether the app has said anything since it was launched.
func (g *GrayBox) On() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.on
}

// AwaitOn waits up to timeout for the app's first line.
func (g *GrayBox) AwaitOn(ctx context.Context, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		g.mu.Lock()
		on, ch := g.on, g.changed
		g.mu.Unlock()
		if on {
			return true
		}
		select {
		case <-ch:
		case <-deadline.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// IdleWait is what waiting for the app took.
type IdleWait struct {
	Waited time.Duration `json:"-"`
	Ms     int64         `json:"waited_ms"`
	// Busy is what the app said it was busy with while it was waited for.
	Busy []string `json:"busy,omitempty"`
}

// AwaitIdle waits until the app has nothing in flight. since is when the
// previous call ended: work a tap started is announced as the tap is
// handled, so the wait first lets that call's lift arrive and then the
// grace after it, and only then trusts a count of zero. It gives up after
// limit, naming what the app was still busy with.
func (g *GrayBox) AwaitIdle(ctx context.Context, since time.Time, limit time.Duration) (IdleWait, error) {
	start := time.Now()
	deadline := start.Add(limit)
	seen := map[string]bool{}
	for {
		now := time.Now()
		g.mu.Lock()
		busy, lift, ch := g.busy, g.lift, g.changed
		for t := range g.tags {
			seen[t] = true
		}
		var still []string
		for t := range g.tags {
			still = append(still, t)
		}
		g.mu.Unlock()

		ready := since.Add(grayBoxDelivery)
		if l := lift.Add(grayBoxGrace); l.After(ready) {
			ready = l
		}
		if busy == 0 && !now.Before(ready) {
			w := IdleWait{Waited: now.Sub(start), Ms: now.Sub(start).Milliseconds()}
			for t := range seen {
				w.Busy = append(w.Busy, t)
			}
			sort.Strings(w.Busy)
			return w, nil
		}
		if now.After(deadline) {
			sort.Strings(still)
			return IdleWait{Waited: now.Sub(start), Ms: now.Sub(start).Milliseconds(), Busy: still},
				mobiumerr.New(mobiumerr.Timeout, "the app was still busy after %s, with %s", limit, strings.Join(still, ", "))
		}
		wake := deadline
		if busy == 0 && ready.Before(wake) {
			wake = ready
		}
		timer := time.NewTimer(time.Until(wake))
		select {
		case <-ch:
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return IdleWait{}, ctx.Err()
		}
		timer.Stop()
	}
}
