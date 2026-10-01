package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/daemon"
	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// daemonIdleTimeout is how long an auto-started daemon lives without work.
// Long enough to span a person thinking between commands, short enough that a
// forgotten daemon does not outlive the emulator it was talking to.
const daemonIdleTimeout = "30m"

// daemonCall runs a tool through the daemon, starting one if none is running.
func daemonCall(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
	return daemonCallMeta(tool, args, nil)
}

// daemonCallMeta is daemonCall with the request's _meta passed through, for
// the pipe, whose clients may send one.
func daemonCallMeta(tool string, args, meta map[string]interface{}) (*agent.ToolsCallResult, error) {
	if gridActive && !gridRouted {
		if err := routeGrid(args); err != nil {
			return nil, err
		}
	}
	// These flags are per-command, so they travel with the call rather than
	// being stored in a daemon shared with other terminals.
	if deviceSerial != "" {
		args["device"] = deviceSerial
	}
	if backendName != "" {
		if _, err := agent.ParseBackend(backendName); err != nil {
			return nil, err
		}
		args["driver"] = backendName
	}
	finish, err := prepareFiles(tool, args)
	if err != nil {
		return nil, err
	}

	result, err := daemon.CallMeta(tool, args, meta)
	if err == nil {
		return finish(result)
	}
	// A tool that ran and failed is an answer, not a reason to start a second
	// daemon.
	if !daemon.IsConnectionError(err) {
		return nil, err
	}
	// Through a forward, the daemon is the node's: starting one here would
	// drive this machine's devices under the node's name.
	if remoteActive {
		return nil, mobiumerr.New(mobiumerr.DeviceNotReady, "the node's daemon stopped answering through the SSH forward: %v", err).
			WithRemedy("run the command again; `mobium daemon up` on the node says whether its daemon is running")
	}

	daemon.CleanStale()
	if err := autoStartDaemon(); err != nil {
		return nil, err
	}
	result, err = daemon.CallMeta(tool, args, meta)
	if err != nil {
		return nil, err
	}
	return finish(result)
}

// prepareFiles readies a call's file arguments for a daemon that may not
// share the caller's directory or disk, and returns what to do with the
// result. A batch's steps are calls too, and each is readied as one.
func prepareFiles(tool string, args map[string]interface{}) (func(*agent.ToolsCallResult) (*agent.ToolsCallResult, error), error) {
	if tool == "app_batch" {
		return prepareBatchFiles(args)
	}
	// A relative path means the caller's directory, and only the caller
	// knows it: the daemon resolves one against its own, which is wherever
	// it happened to start. `mobium screenshot -o rel.png` run in one
	// directory saved into another, reporting the wrong path as a success.
	// Resolved here because the CLI and every client (through `pipe`) come
	// through this one function.
	for _, key := range agent.PathArguments[tool] {
		if p, ok := args[key].(string); ok && p != "" && !filepath.IsAbs(p) {
			if abs, err := filepath.Abs(p); err == nil {
				args[key] = abs
			}
		}
	}

	// When the daemon's disk is not the caller's, a path means a file on the
	// wrong machine: send the file's content instead, and save what comes
	// back where the caller asked.
	finish := func(r *agent.ToolsCallResult) (*agent.ToolsCallResult, error) { return r, nil }
	if filesAsContent() {
		f, err := sendFilesAsContent(tool, args)
		if err != nil {
			return nil, err
		}
		finish = f
	}
	return finish, nil
}

// autoStartDaemon spawns a detached daemon and waits for it to answer.
func autoStartDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find mobium executable: %w", err)
	}

	cmd := exec.Command(exe, "daemon", "start", "--idle-timeout", daemonIdleTimeout)
	// Detach: the daemon must outlive the command that started it.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	device.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	// The child is reparented to init; not reaping it here would leave a
	// zombie for the life of this process.
	go cmd.Wait()

	logf("started daemon (pid %d)", cmd.Process.Pid)

	// Poll rather than sleep a fixed amount: a warm start answers in
	// milliseconds and a cold one should not be punished by a fixed wait.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := daemon.Status(); err == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not come up within 10s (try `mobium daemon start` to see why)")
}

// resultText joins a tool result's text blocks, which is what the CLI prints.
func resultText(result *agent.ToolsCallResult) string {
	var parts []string
	for _, c := range result.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}
