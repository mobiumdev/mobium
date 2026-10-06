package device

import (
	"context"
	"errors"
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
	w, err := g.AwaitIdle(context.Background(), time.Time{}, 5*time.Second)
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
	w, err := g.AwaitIdle(context.Background(), time.Time{}, 5*time.Second)
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
	w, err := g.AwaitIdle(context.Background(), time.Now().Add(-time.Second), 5*time.Second)
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
	w, err := g.AwaitIdle(context.Background(), time.Time{}, 200*time.Millisecond)
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
	if _, err := g.AwaitIdle(context.Background(), time.Time{}, 100*time.Millisecond); err != nil {
		t.Errorf("work from before the relaunch is still waited for: %v", err)
	}
}
