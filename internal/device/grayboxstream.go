package device

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
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
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
	}
	return cancel, nil
}
