package device

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// StreamGrayBox follows the simulator's unified log for the gray-box
// library's lines and feeds each to g as it arrives, until stop is called.
//
// A device-log read on the simulator is `log show` over a window, about
// 0.85s a read — too slow to ask before every action. `log stream`, narrowed
// to the library's subsystem, is live, and carries nothing else.
//
// It returns once the stream has started — `log stream` prints a line saying
// what it filters before anything else — so a launch after it is heard from
// its first line.
func (s *Simctl) StreamGrayBox(g *GrayBox) (stop func(), err error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, s.Path, "simctl", "spawn", s.UDID, "log", "stream",
		"--style", "ndjson", "--level", "default",
		"--predicate", fmt.Sprintf("subsystem == %q", GrayBoxSubsystem))
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("follow the simulator's log for the gray box: %w", err)
	}
	started := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		first := true
		for sc.Scan() {
			if first {
				close(started)
				first = false
			}
			var e struct {
				Message string `json:"eventMessage"`
			}
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				g.Feed(e.Message, time.Now())
			}
		}
		if first {
			close(started)
		}
		_ = cmd.Wait()
		if ctx.Err() == nil {
			// Unlike logcat, the unified log's stream cannot be asked for
			// what it missed; a stream that ends leaves the gray box deaf,
			// and the next gray-box launch starts another.
			g.Deaf("the simulator's log stream stopped; launch the app with gray_box again")
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
	}
	return cancel, nil
}

// GrayBoxExtra is what an Android app is started with to turn the library
// on: an intent extra, which a launch from the home screen never carries.
var GrayBoxExtra = []string{"--ez", "MobiumGrayBox", "true"}

// grayBoxTag is the logcat tag the Android library writes under.
const grayBoxTag = "MobiumGrayBox"

// StreamGrayBox follows logcat for the gray-box library's lines and feeds
// each to g as it arrives, until stop is called.
//
// It starts from the device's own clock, read first: logcat keeps a buffer,
// so a line written after that moment is delivered however late the stream
// attaches, and a line from an earlier launch is not. If logcat exits — the
// device dropped off adb — g is told it is deaf, and the stream starts again
// from the device time of the last line it delivered, so a line written in
// the gap is still read.
func (a *ADB) StreamGrayBox(g *GrayBox) (stop func(), err error) {
	now, err := a.Shell(context.Background(), "date", "+%s.%N")
	if err != nil {
		return nil, fmt.Errorf("read the device clock for the gray box: %w", err)
	}
	since := strings.TrimSpace(string(now))
	if _, perr := strconv.ParseFloat(since, 64); perr != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the device clock read as %q, not seconds", since)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for ctx.Err() == nil {
			why := a.followGrayBox(ctx, g, &since)
			if ctx.Err() != nil {
				return
			}
			g.Deaf(why)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
	return cancel, nil
}

// followGrayBox runs one logcat until it exits, advancing since to the
// device time of each line, and says why it stopped.
func (a *ADB) followGrayBox(ctx context.Context, g *GrayBox, since *string) string {
	cmd := exec.CommandContext(ctx, a.Path, a.args("logcat", "-v", "epoch", "-T", *since,
		grayBoxTag+":I", "*:S")...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err.Error()
	}
	if err := cmd.Start(); err != nil {
		return "logcat would not start: " + err.Error()
	}
	g.Hearing()
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if f := strings.Fields(line); len(f) > 0 {
			if t, perr := strconv.ParseFloat(f[0], 64); perr == nil {
				// Just past this line, so a restart does not read it twice.
				*since = strconv.FormatFloat(t+0.0005, 'f', 3, 64)
			}
		}
		g.Feed(line, time.Now())
	}
	err = cmd.Wait()
	if err != nil {
		return "logcat stopped (" + err.Error() + ")"
	}
	return "logcat stopped"
}
