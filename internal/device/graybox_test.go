package device

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

func TestGrayBoxIgnoresOtherLines(t *testing.T) {
	g := NewGrayBox()
	g.Feed("SpringBoard: something else", time.Now())
	g.Feed("MOBIUM-GRAYBOX", time.Now())
	g.Feed("MOBIUM-GRAYBOX nonsense t=1", time.Now())
	g.Feed("MOBIUM-GRAYBOX busy=x tag=a", time.Now())
	if g.On() {
		t.Fatal("a line that is not the library's turned the gray box on")
	}
	g.Feed("MOBIUM-GRAYBOX on t=1791244770773", time.Now())
	if !g.On() {
		t.Fatal("the library's first line did not turn the gray box on")
	}
}

func TestGrayBoxWaitsOutWork(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX busy=1 tag=quiet t=1", time.Now())
	go func() {
		time.Sleep(300 * time.Millisecond)
		g.Feed("MOBIUM-GRAYBOX busy=0 tag=quiet t=2", time.Now())
	}()
	w, err := g.AwaitIdle(context.Background(), Previous{}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if w.Waited < 250*time.Millisecond {
		t.Errorf("returned after %s, before the work finished", w.Waited)
	}
	if len(w.Busy) != 1 || w.Busy[0] != "quiet" {
		t.Errorf("busy = %v, want [quiet]", w.Busy)
	}
}

// A tap's busy line arrives just after its lift; until the grace after the
// lift has passed, a count of zero is not yet to be trusted.
func TestGrayBoxWaitsTheGraceAfterALift(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX on", time.Now())
	g.Feed("MOBIUM-GRAYBOX lift t=1", time.Now())
	go func() {
		time.Sleep(20 * time.Millisecond)
		g.Feed("MOBIUM-GRAYBOX busy=1 tag=late t=2", time.Now())
		time.Sleep(200 * time.Millisecond)
		g.Feed("MOBIUM-GRAYBOX busy=0 tag=late t=3", time.Now())
	}()
	w, err := g.AwaitIdle(context.Background(), Previous{}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if w.Waited < 200*time.Millisecond || len(w.Busy) != 1 {
		t.Errorf("waited %s for %v: the busy line after the lift was missed", w.Waited, w.Busy)
	}
}

func TestGrayBoxIdleAtOnceWhenQuiet(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX on", time.Now().Add(-time.Second))
	w, err := g.AwaitIdle(context.Background(), Previous{End: time.Now().Add(-time.Second)}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if w.Waited > 20*time.Millisecond {
		t.Errorf("an idle app was waited for %s", w.Waited)
	}
}

func TestGrayBoxNamesWorkThatNeverEnds(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX busy=1 tag=poll", time.Now())
	g.Feed("MOBIUM-GRAYBOX busy=2 tag=upload", time.Now())
	w, err := g.AwaitIdle(context.Background(), Previous{}, 200*time.Millisecond)
	var me *mobiumerr.Error
	if !errors.As(err, &me) || me.Code != mobiumerr.Timeout {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if len(w.Busy) != 2 || w.Busy[0] != "poll" || w.Busy[1] != "upload" {
		t.Errorf("busy = %v, want [poll upload]", w.Busy)
	}
}

func TestGrayBoxResetForgets(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX busy=1 tag=a", time.Now())
	g.Reset()
	if g.On() {
		t.Error("a reset gray box still reads as heard")
	}
	if _, err := g.AwaitIdle(context.Background(), Previous{}, 100*time.Millisecond); err != nil {
		t.Errorf("work from before the relaunch is still waited for: %v", err)
	}
}

// Busy is a lease: an app that stops restating it — crashed holding the
// work, or suspended — is not waited on past the lease, and the wait says so.
func TestGrayBoxBusyLeaseRunsOut(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX busy=1 tag=doomed", time.Now())
	w, err := g.AwaitIdle(context.Background(), Previous{}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if w.Unwaited == "" || !strings.Contains(w.Unwaited, "doomed") {
		t.Errorf("not waited = %q, want the lease's end naming doomed", w.Unwaited)
	}
	if w.Waited < grayBoxLease-100*time.Millisecond || w.Waited > grayBoxLease+500*time.Millisecond {
		t.Errorf("waited %s, want about the lease, %s", w.Waited, grayBoxLease)
	}
}

// A lease restated is held: still lines keep the wait going past the lease.
func TestGrayBoxStillHoldsTheLease(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX busy=1 tag=long", time.Now())
	stop := make(chan struct{})
	go func() {
		for i := 0; i < 6; i++ {
			time.Sleep(400 * time.Millisecond)
			g.Feed("MOBIUM-GRAYBOX still busy=1", time.Now())
		}
		g.Feed("MOBIUM-GRAYBOX busy=0 tag=long", time.Now())
		close(stop)
	}()
	w, err := g.AwaitIdle(context.Background(), Previous{}, 10*time.Second)
	<-stop
	if err != nil || w.Unwaited != "" {
		t.Fatalf("w=%+v err=%v: a restated lease was given up", w, err)
	}
	if w.Waited < 2*time.Second {
		t.Errorf("waited %s, want the whole of the work", w.Waited)
	}
}

func TestGrayBoxAwayIsNotWaitedOn(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX busy=1 tag=quiet", time.Now())
	g.Feed("MOBIUM-GRAYBOX away", time.Now())
	w, err := g.AwaitIdle(context.Background(), Previous{}, 5*time.Second)
	if err != nil || !strings.Contains(w.Unwaited, "background") || w.Waited > 100*time.Millisecond {
		t.Fatalf("w=%+v err=%v: an app in the background was waited on", w, err)
	}
	g.Feed("MOBIUM-GRAYBOX back", time.Now())
	if w, _ := g.AwaitIdle(context.Background(), Previous{}, 200*time.Millisecond); w.Unwaited != "" {
		t.Errorf("back in front and still not waited on: %q", w.Unwaited)
	}
}

// A process starting over holds nothing the last one did.
func TestGrayBoxOnForgetsTheLastProcess(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX busy=2 tag=a", time.Now())
	g.Feed("MOBIUM-GRAYBOX on", time.Now())
	w, err := g.AwaitIdle(context.Background(), Previous{}, time.Second)
	if err != nil || w.Waited > 100*time.Millisecond {
		t.Fatalf("w=%+v err=%v: work from before a restart was waited on", w, err)
	}
}

// A tap on a button in an alert lifts in the alert's own window, which the
// library does not watch: with no lift during an action, the grace counts
// from the action's end.
func TestGrayBoxGraceAfterAnActionWithNoLift(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX on", time.Now())
	start := time.Now().Add(-300 * time.Millisecond)
	prev := Previous{Start: start, End: time.Now(), Acted: true}
	go func() {
		time.Sleep(80 * time.Millisecond)
		g.Feed("MOBIUM-GRAYBOX busy=1 tag=alert", time.Now())
		time.Sleep(300 * time.Millisecond)
		g.Feed("MOBIUM-GRAYBOX busy=0 tag=alert", time.Now())
	}()
	w, err := g.AwaitIdle(context.Background(), prev, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if w.Waited < 300*time.Millisecond || len(w.Busy) != 1 {
		t.Errorf("waited %s for %v: work announced 80ms after an action with no lift was missed", w.Waited, w.Busy)
	}
	// A read cannot have started work: no grace after it.
	g2 := NewGrayBox()
	g2.Feed("MOBIUM-GRAYBOX on", time.Now())
	w2, _ := g2.AwaitIdle(context.Background(), Previous{Start: start, End: time.Now(), Acted: false}, time.Second)
	if w2.Waited > grayBoxDelivery+30*time.Millisecond {
		t.Errorf("waited %s after a read", w2.Waited)
	}
}

func TestGrayBoxDeafIsSaid(t *testing.T) {
	g := NewGrayBox()
	g.Feed("MOBIUM-GRAYBOX busy=1 tag=quiet", time.Now())
	g.Deaf("logcat stopped")
	w, err := g.AwaitIdle(context.Background(), Previous{}, 5*time.Second)
	if err != nil || !strings.Contains(w.Unwaited, "logcat stopped") {
		t.Fatalf("w=%+v err=%v", w, err)
	}
	g.Hearing()
	if w, _ := g.AwaitIdle(context.Background(), Previous{}, 100*time.Millisecond); strings.Contains(w.Unwaited, "logcat") {
		t.Errorf("still deaf after hearing: %q", w.Unwaited)
	}
}
