package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/daemon"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/paths"
)

// remoteNode is the machine whose daemon this command drives: --remote, or
// MOBIUM_REMOTE. Empty means this machine.
var remoteNode string

// remoteActive says the daemon is a node's, reached through a forward; a
// connection that fails is then the node's to explain, and a local daemon
// must not be started in its place — that would drive this machine's
// devices while the caller believed it was driving the node's.
var remoteActive bool

// cleanups run on every way out of the process, so a forward and its
// directory never outlive the command that made them.
var cleanups []func()

func runCleanups() {
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
	cleanups = nil
}

// localOnly are the commands that are about this machine, and stay here.
var localOnly = map[string]bool{"daemon": true, "doctor": true, "mcp": true, "version": true, "help": true, "completion": true, "grid": true}

// upStatus is what `mobium daemon up --json` prints on the node.
type upStatus struct {
	Socket  string `json:"socket"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

// openRemote reaches a node's daemon over SSH: it asks the node to have a
// daemon running and say where it listens, forwards that socket to one in a
// private directory here, and points this process at it, with file arguments
// traveling as content (MOBIUM_FILES), since the node's disk is not this
// one. SSH is the transport because it already authenticates and encrypts,
// and the daemon's own socket stays owner-only on the node: nothing new
// listens on a network. Measured by hand first, against this Mac standing in
// for a node — ROADMAP, "A mobium grid".
func openRemote(node string) error {
	if err := remoteSupported(); err != nil {
		return err
	}
	bin := remoteBin()
	up := sshCommand(node, "daemon up --json")
	var out, errb bytes.Buffer
	up.Stdout, up.Stderr = &out, &errb
	if err := up.Run(); err != nil {
		return mobiumerr.New(mobiumerr.DeviceNotReady, "could not reach mobium on %s over SSH: %v — %s", node, err,
			strings.TrimSpace(errb.String())).
			WithRemedy(fmt.Sprintf("check that `ssh %s %s --version` works without a prompt; "+
				"MOBIUM_REMOTE_BIN is what the node's shell runs as mobium", node, bin))
	}
	var st upStatus
	if err := json.Unmarshal(out.Bytes(), &st); err != nil || st.Socket == "" {
		return mobiumerr.New(mobiumerr.DeviceServer, "mobium on %s did not say where its daemon listens: %q", node,
			strings.TrimSpace(out.String()))
	}
	if st.Version != version {
		fmt.Fprintf(os.Stderr, "mobium: %s runs mobium %s, and this is %s\n", node, st.Version, version)
	}

	sweepRemoteHomes()
	// A private home here, short enough for a socket path: the OS caps
	// those at about 104 bytes, and the temporary directory macOS gives
	// every process is most of that already.
	home, err := os.MkdirTemp(remoteTempRoot(), "mbr-")
	if err != nil {
		return err
	}
	cleanups = append(cleanups, func() { _ = os.RemoveAll(home) })
	if err := os.MkdirAll(filepath.Join(home, "daemon"), 0o700); err != nil {
		return err
	}
	if err := os.Setenv("MOBIUM_HOME", home); err != nil {
		return err
	}
	local, err := paths.SocketPath()
	if err != nil {
		return err
	}

	// Not -N: the forward runs `cat` on the node, reading a pipe this
	// process holds. A kill -9 gives no chance to clean up, and left the
	// forward running with nothing on this end, for good; the system closes
	// the pipe with the process instead, cat ends, and SSH with it.
	fwd := sshExec("-T", "-o", "ExitOnForwardFailure=yes", "-o", "StreamLocalBindUnlink=yes",
		"-L", local+":"+st.Socket, node, "cat >/dev/null")
	lifeline, err := fwd.StdinPipe()
	if err != nil {
		return err
	}
	var fwdErr bytes.Buffer
	fwd.Stderr = &fwdErr
	// Its own process group: a Ctrl-C meant for a client must not end the
	// forward before the client's own cleanup has used it to end its session.
	setOwnGroup(fwd)
	if err := fwd.Start(); err != nil {
		return fmt.Errorf("start the SSH forward: %w", err)
	}
	done := make(chan struct{})
	go func() { _ = fwd.Wait(); close(done) }()
	cleanups = append(cleanups, func() {
		_ = lifeline.Close()
		_ = fwd.Process.Kill()
		<-done
	})

	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := daemon.Status(); err == nil {
			break
		}
		select {
		case <-done:
			return mobiumerr.New(mobiumerr.DeviceNotReady, "the SSH forward to %s ended: %s", node, strings.TrimSpace(fwdErr.String()))
		default:
		}
		if time.Now().After(deadline) {
			return mobiumerr.New(mobiumerr.Timeout, "no answer from %s's daemon through the forward within 15s", node)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// The forward's socket carries commands that drive the node's devices;
	// nobody else on this machine should be able to use it either.
	if info, err := os.Stat(local); err != nil || info.Mode().Perm()&0o077 != 0 {
		return mobiumerr.New(mobiumerr.DeviceServer, "the forwarded socket %s is not owner-only", local)
	}
	remoteActive = true
	return os.Setenv("MOBIUM_FILES", "content")
}

// sshExec is the ssh command, as MOBIUM_SSH gives it, with mobium's own
// options and then these arguments. Never a prompt: a password or host-key
// question would be read from, and answered into, the stream a client speaks
// JSON-RPC on. And a node that does not answer is given up on in seconds, so
// a grid with one down still routes to the others.
func sshExec(args ...string) *exec.Cmd {
	ssh := strings.Fields(os.Getenv("MOBIUM_SSH"))
	if len(ssh) == 0 {
		ssh = []string{"ssh"}
	}
	a := append([]string{}, ssh[1:]...)
	a = append(a, "-o", "BatchMode=yes", "-o", "ServerAliveInterval=15", "-o", "ConnectTimeout=5")
	return exec.Command(ssh[0], append(a, args...)...)
}

// sshCommand runs `mobium <sub>` on a node, in the session this process
// uses, as MOBIUM_REMOTE_BIN says the node's shell runs mobium.
func sshCommand(node, sub string) *exec.Cmd {
	return sshSession(node, paths.SessionName(), sub)
}

// sshSession is sshCommand in a named session on the node: "" is the node's
// default daemon, which a grid's queries and leases use, so asking which
// devices are free never starts a daemon of a run's own.
func sshSession(node, session, sub string) *exec.Cmd {
	remote := remoteBin() + " " + sub
	if session != "" {
		remote = "MOBIUM_SESSION=" + session + " " + remote
	}
	return sshExec("-T", node, remote)
}

// remoteBin is what a node's shell runs as mobium: MOBIUM_REMOTE_BIN, which
// may set that shell's environment first, or mobium on its PATH.
func remoteBin() string {
	if b := os.Getenv("MOBIUM_REMOTE_BIN"); b != "" {
		return b
	}
	return "mobium"
}

// sweepRemoteHomes removes the private homes of runs that died without
// cleaning up — a kill -9 leaves one behind — recognized by a socket that no
// longer answers. Only this user's, and only those older than half a minute,
// so a run still starting is never mistaken for a dead one.
func sweepRemoteHomes() {
	dirs, _ := filepath.Glob(filepath.Join(remoteTempRoot(), "mbr-*"))
	for _, d := range dirs {
		info, err := os.Stat(d)
		if err != nil || !info.IsDir() || time.Since(info.ModTime()) < 30*time.Second || !ownedByMe(info) {
			continue
		}
		socks, _ := filepath.Glob(filepath.Join(d, "daemon", "*.sock"))
		alive := false
		for _, s := range socks {
			if c, err := net.DialTimeout("unix", s, 300*time.Millisecond); err == nil {
				c.Close()
				alive = true
			}
		}
		if !alive {
			_ = os.RemoveAll(d)
		}
	}
}
