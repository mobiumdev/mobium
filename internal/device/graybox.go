package device

import (
	"context"
	"fmt"
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
//	MOBIUM-GRAYBOX still busy=1       the work is still in flight, every
//	                                  half second while it is
//	MOBIUM-GRAYBOX away / back        the app left the foreground / returned
//
// Busy is a lease: a count above zero that nothing has restated for
// grayBoxLease is stale — the app crashed holding it, or was suspended —
// and is not waited on. Nor is an app that said it is away, or one the
// stream has stopped hearing; each of those is said in the result instead.
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
	// said is when the app last stated its count: a busy line, or still.
	said time.Time
	away bool
	// deaf is why the stream stopped, while it is stopped.
	deaf string
	// regained is when a stream that had stopped was heard again. Lines
	// from the gap are lost — a busy=1 among them — so until the app has had
	// time to restate its count, zero is not trusted.
	regained time.Time
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

// grayBoxLease is how long a busy count stands without being restated. The
// library restates it every 500ms, so three missed beats.
const grayBoxLease = 1500 * time.Millisecond

// grayBoxRecover is how long after a stream is regained a count of zero is
// not trusted: one restatement of the count (every 500ms) and its delivery.
const grayBoxRecover = 700 * time.Millisecond

// NewGrayBox returns a gray box that has heard nothing yet.
func NewGrayBox() *GrayBox {
	return &GrayBox{changed: make(chan struct{}), tags: map[string]int{}}
}

// Reset forgets everything heard, for an app launched again.
func (g *GrayBox) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.on, g.busy, g.tags, g.lift, g.said, g.away = false, 0, map[string]int{}, time.Time{}, time.Time{}, false
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
		// A process starting over: whatever the last one held is gone.
		g.busy, g.tags, g.away = 0, map[string]int{}, false
	case fields[0] == "lift":
		g.lift = at
	case fields[0] == "away":
		g.away = true
	case fields[0] == "back":
		g.away = false
	case fields[0] == "still" && len(fields) > 1 && strings.HasPrefix(fields[1], "busy="):
		n, err := strconv.Atoi(strings.TrimPrefix(fields[1], "busy="))
		if err != nil || n < 0 {
			return
		}
		if n == 0 {
			g.tags = map[string]int{}
		}
		g.busy, g.said = n, at
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
		g.busy, g.said = n, at
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

// Deaf marks the stream as stopped, saying why; Hearing, as running again.
func (g *GrayBox) Deaf(why string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.deaf = why
	g.notify()
}

// IsDeaf reports whether the stream has stopped.
func (g *GrayBox) IsDeaf() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.deaf != ""
}

// Hearing marks the stream as running.
func (g *GrayBox) Hearing() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.deaf != "" {
		// What it held before the gap is stale either way: work may have
		// finished unheard. The count starts again from zero, and work
		// still in flight restates itself within grayBoxRecover.
		g.regained = time.Now()
		g.busy, g.tags = 0, map[string]int{}
	}
	g.deaf = ""
	g.notify()
}

// IdleWait is what waiting for the app took.
type IdleWait struct {
	Waited time.Duration `json:"-"`
	Ms     int64         `json:"waited_ms"`
	// Busy is what the app said it was busy with while it was waited for.
	Busy []string `json:"busy,omitempty"`
	// Unwaited says why the app was not waited for, when it was not: it
	// said it is away, its busy lease ran out, or the stream is not
	// hearing it.
	Unwaited string `json:"not_waited,omitempty"`
	// Regained says the stream had dropped and was heard again, so the
	// wait gave the app time to restate its count first.
	Regained bool `json:"stream_regained,omitempty"`
}

// Previous is the call before this one, which a wait counts its grace from.
type Previous struct {
	Start, End time.Time
	// Acted is whether it could have touched the app: a read cannot start
	// work, so its end needs no grace.
	Acted bool
}

// AwaitIdle waits until the app has nothing in flight. since is when the
// previous call ended: work a tap started is announced as the tap is
// handled, so the wait first lets that call's lift arrive and then the
// grace after it, and only then trusts a count of zero. It gives up after
// limit, naming what the app was still busy with.
func (g *GrayBox) AwaitIdle(ctx context.Context, prev Previous, limit time.Duration) (IdleWait, error) {
	start := time.Now()
	deadline := start.Add(limit)
	seen := map[string]bool{}
	for {
		now := time.Now()
		g.mu.Lock()
		busy, lift, ch, said, away, deaf, regained := g.busy, g.lift, g.changed, g.said, g.away, g.deaf, g.regained
		for t := range g.tags {
			seen[t] = true
		}
		var still []string
		for t := range g.tags {
			still = append(still, t)
		}
		g.mu.Unlock()

		// Work a tap starts is announced as the tap is handled: after its
		// lift, the grace; with no lift heard during an action — a button
		// in an alert, which is a window of its own — after the action.
		ready := prev.End.Add(grayBoxDelivery)
		if !lift.Before(prev.Start) && !lift.IsZero() {
			if l := lift.Add(grayBoxGrace); l.After(ready) {
				ready = l
			}
		} else if prev.Acted {
			if l := prev.End.Add(grayBoxGrace); l.After(ready) {
				ready = l
			}
		}
		if !regained.IsZero() {
			if r := regained.Add(grayBoxRecover); r.After(ready) {
				ready = r
			}
		}
		unwaited := ""
		switch {
		case deaf != "":
			unwaited = "not hearing the app: " + deaf
		case away:
			unwaited = "the app said it is in the background"
		case busy > 0 && now.Sub(said) > grayBoxLease:
			unwaited = fmt.Sprintf("the app stopped saying it is busy %s ago (it was busy with %s)",
				now.Sub(said).Round(100*time.Millisecond), strings.Join(still, ", "))
		}
		if unwaited != "" && !now.Before(ready) {
			sort.Strings(still)
			return IdleWait{Waited: now.Sub(start), Ms: now.Sub(start).Milliseconds(), Unwaited: unwaited}, nil
		}
		if busy == 0 && !now.Before(ready) {
			w := IdleWait{Waited: now.Sub(start), Ms: now.Sub(start).Milliseconds(), Regained: !regained.IsZero()}
			if !regained.IsZero() {
				g.mu.Lock()
				g.regained = time.Time{}
				g.mu.Unlock()
			}
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
		if (busy == 0 || unwaited != "") && ready.Before(wake) {
			wake = ready
		}
		if busy > 0 && unwaited == "" {
			// The lease may run out with nothing arriving to say so.
			if l := said.Add(grayBoxLease + time.Millisecond); l.Before(wake) {
				wake = l
			}
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
