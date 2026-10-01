package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/grid"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/spf13/cobra"
)

// A grid has no hub: MOBIUM_GRID names the
// nodes, the caller's own mobium routes each run to a free device that
// matches, and the lease that keeps a device to one run lives on the node, so
// callers on different machines cannot take the same one. Nothing new listens
// anywhere — every step is SSH, and the connection is --remote's (Stage 2).

// leaseEvery is how often a holder renews, well inside grid.TTL.
const leaseEvery = 20 * time.Second

var (
	gridActive bool   // this process routes through MOBIUM_GRID
	gridRouted bool   // and has chosen its node and device
	gridHolder string // who this process is, in a lease
)

func gridNodes() []string {
	var out []string
	for _, n := range strings.Split(os.Getenv("MOBIUM_GRID"), ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// ---- the node's side -------------------------------------------------------

// nodeStatus is what `mobium grid node --json` prints: the devices here, and
// which of them a run holds.
type nodeStatus struct {
	Devices []gridDevice          `json:"devices"`
	Leases  map[string]string     `json:"leases"` // serial -> holder, live leases only
	Held    map[string]grid.Lease `json:"held"`   // serial -> the lease in full, for a grid's view
	Waiting []grid.Waiting        `json:"waiting"`
}

// gridDevice is a device as a grid routes it: what `devices` reports, and
// the OS it runs, for MOBIUM_GRID_OS.
type gridDevice struct {
	agent.DeviceView
	OS string `json:"os,omitempty"`
	// Offered says the node lends this device to the grid: every emulator
	// and simulator, and a physical phone only when the node's own
	// environment says MOBIUM_GRID_PHONES=1. A phone plugged into a node is
	// usually somebody's, and a grid run that took it would install, tap and
	// change settings on it — so it is the node's choice, never the caller's.
	Offered bool `json:"offered"`
}

func newGridCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grid",
		Short: "Look at the grid MOBIUM_GRID names: its nodes, devices, leases and queue",
		Long: "`mobium grid status` prints it, and `mobium grid ui` serves it as a page on this\n" +
			"machine. The other subcommands are the node's half of the grid, which the\n" +
			"caller's mobium runs over SSH.",
	}
	var holder string
	node := &cobra.Command{
		Use:  "node",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := daemonCall("app_devices", map[string]interface{}{})
			if err != nil {
				return err
			}
			var dv agent.DevicesView
			if err := remarshal(res.StructuredContent, &dv); err != nil {
				return err
			}
			var st nodeStatus
			for _, d := range dv.Devices {
				st.Devices = append(st.Devices, gridDevice{DeviceView: d, OS: deviceOS(d),
					Offered: d.Emulator || os.Getenv("MOBIUM_GRID_PHONES") == "1"})
			}
			if st.Leases, err = grid.Live(); err != nil {
				return err
			}
			if st.Held, err = grid.LiveLeases(); err != nil {
				return err
			}
			if st.Waiting, err = grid.Queue(); err != nil {
				return err
			}
			return printJSON(st)
		},
	}
	lease := &cobra.Command{
		Use:  "lease <serial>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			got, err := grid.Take(args[0], holder)
			if err != nil {
				return err
			}
			return printJSON(got)
		},
	}
	release := &cobra.Command{
		Use:  "release <serial>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return grid.Drop(args[0], holder)
		},
	}
	for _, c := range []*cobra.Command{lease, release} {
		c.Flags().StringVar(&holder, "holder", "", "who holds the lease")
		_ = c.MarkFlagRequired("holder")
	}
	var want string
	wait := &cobra.Command{
		Use:  "wait <holder>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return grid.Wait(args[0], want) },
	}
	wait.Flags().StringVar(&want, "want", "", "what the run is waiting for")
	unwait := &cobra.Command{
		Use:  "unwait <holder>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return grid.Unwait(args[0]) },
	}
	for _, c := range []*cobra.Command{node, lease, release, wait, unwait} {
		c.Hidden = true
	}
	cmd.AddCommand(node, lease, release, wait, unwait, newGridStatusCmd(), newGridUICmd())
	return cmd
}

// ---- the caller's side -----------------------------------------------------

// routeGrid chooses the node and device for this run, from what its first
// call asks for — a serial, or a platform, or a driver that implies one —
// takes the device's lease on its node, and connects to that node as
// --remote would. With nothing free that matches it waits, up to
// MOBIUM_GRID_WAIT (default 60s), asking again every two seconds.
func routeGrid(args map[string]interface{}) error {
	gridRouted = true
	serial, _ := args["device"].(string)
	if serial == "" {
		serial = deviceSerial
	}
	platform, _ := args["platform"].(string)
	driver, _ := args["driver"].(string)
	if driver == "" {
		driver = backendName
	}
	switch {
	case platform != "":
	case driver == "wda":
		platform = "ios"
	case strings.HasPrefix(driver, "uiautomator"):
		platform = "android"
	}
	wait := 60 * time.Second
	if d, err := time.ParseDuration(os.Getenv("MOBIUM_GRID_WAIT")); err == nil {
		wait = d
	}
	gridHolder = newHolder()
	// The run's daemon on the node is named by its lease, so it alone may
	// use the device; this process's own socket is named the same.
	if err := os.Setenv("MOBIUM_SESSION", gridHolder); err != nil {
		return err
	}
	nodes := gridNodes()
	deadline := time.Now().Add(wait)
	want := describeWant(serial, platform)
	// A queued run leaves a note on each node it can reach, so a grid's view
	// shows the queue; it lapses by itself if this run dies.
	var noted []string
	defer func() {
		for _, n := range noted {
			_ = sshSession(n, "", "grid unwait "+gridHolder).Run()
		}
	}()

	for {
		statuses, down := askNodes(nodes)
		var busy []string
		for _, node := range nodes {
			st, ok := statuses[node]
			if !ok {
				continue
			}
			for _, d := range st.Devices {
				if !deviceReady(d.DeviceView) || (serial != "" && d.ID != serial) || (platform != "" && d.Platform != platform) || !matches(d) {
					continue
				}
				if !d.Offered {
					busy = append(busy, fmt.Sprintf("%s on %s (a phone the node does not offer; MOBIUM_GRID_PHONES=1 there lends it)", d.ID, node))
					continue
				}
				if h, held := st.Leases[grid.Key(d.ID)]; held {
					busy = append(busy, fmt.Sprintf("%s on %s (held by %s)", d.ID, node, h))
					continue
				}
				got, err := askLease(node, "lease", d.ID)
				if err != nil || !got.OK {
					busy = append(busy, fmt.Sprintf("%s on %s (held by %s)", d.ID, node, got.Holder))
					continue
				}
				return useLease(node, d.ID)
			}
		}
		if time.Now().After(deadline) {
			return noDevice(serial, platform, wait, busy, down)
		}
		noted = noted[:0]
		for n := range statuses {
			if sshSession(n, "", "grid wait "+gridHolder+" --want "+shellWord(want)).Run() == nil {
				noted = append(noted, n)
			}
		}
		time.Sleep(2 * time.Second)
	}
}

// useLease connects to the node that holds the device, pins the run to it,
// and keeps the lease alive until the process ends, releasing it then.
func useLease(node, serial string) error {
	stop := make(chan struct{})
	var once sync.Once
	cleanups = append(cleanups, func() {
		once.Do(func() { close(stop) })
		_, _ = askLease(node, "release", serial)
		// The run's own daemon on the node goes with it.
		_ = sshCommand(node, "daemon stop").Run()
	})
	if err := openRemote(node); err != nil {
		return err
	}
	deviceSerial = serial
	logf("grid: %s on %s", serial, node)
	go func() {
		t := time.NewTicker(leaseEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if got, err := askLease(node, "lease", serial); err != nil || !got.OK {
					fmt.Fprintf(os.Stderr, "mobium: could not renew the lease on %s at %s: %v\n", serial, node, err)
				}
			}
		}
	}()
	return nil
}

// deviceOS names the OS a device runs: Android's release, read from the
// device, and iOS's runtime, which the listing already carries.
func deviceOS(d agent.DeviceView) string {
	if d.Platform == "ios" {
		return d.Runtime
	}
	if d.Platform != "android" || !deviceReady(d) {
		return ""
	}
	adb, err := device.New(d.ID)
	if err != nil {
		return ""
	}
	out, err := adb.Shell(context.Background(), "getprop", "ro.build.version.release")
	if err != nil {
		return ""
	}
	return "Android " + strings.TrimSpace(string(out))
}

// matches applies MOBIUM_GRID_MODEL — part of the model's name, in any case
// — and MOBIUM_GRID_OS — the start of a word of the OS, so "17" matches
// "Android 17" and "Android 17.1" and not "Android 15".
func matches(d gridDevice) bool {
	if m := strings.ToLower(os.Getenv("MOBIUM_GRID_MODEL")); m != "" && !strings.Contains(strings.ToLower(d.Model), m) {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(os.Getenv("MOBIUM_GRID_OS")))
	if want == "" {
		return true
	}
	have := strings.ToLower(d.OS)
	if strings.HasPrefix(have, want) {
		return true
	}
	for _, w := range strings.Fields(have) {
		if strings.HasPrefix(w, want) {
			return true
		}
	}
	return false
}

func deviceReady(d agent.DeviceView) bool {
	switch strings.ToLower(d.State) {
	case "device", "booted", "connected":
		return true
	}
	return false
}

// askNodes asks every node for its devices and leases at once; a node that
// does not answer is reported as down, and routing goes on without it.
func askNodes(nodes []string) (map[string]nodeStatus, []string) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := map[string]nodeStatus{}
	var down []string
	for _, n := range nodes {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			var st nodeStatus
			err := runJSON(sshSession(n, "", "grid node --json"), &st)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				down = append(down, fmt.Sprintf("%s (%v)", n, err))
				return
			}
			out[n] = st
		}(n)
	}
	wg.Wait()
	sort.Strings(down)
	return out, down
}

func askLease(node, verb, serial string) (grid.Answer, error) {
	var got grid.Answer
	cmd := sshSession(node, "", fmt.Sprintf("grid %s %s --holder %s", verb, shellWord(serial), gridHolder))
	if verb == "release" {
		return got, cmd.Run()
	}
	return got, runJSON(cmd, &got)
}

func runJSON(cmd interface {
	Output() ([]byte, error)
}, into interface{}) error {
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes.TrimSpace(out), into)
}

// shellWord quotes a serial for the node's shell; serials are tame, and this
// keeps one that is not from becoming a command.
func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// newHolder names this run in a lease, and is also the session name of the
// daemon the run gets on its node: every daemon there refuses a device leased
// to a holder other than its own session. Random, since two runs on one
// machine are two holders, and short, since it is part of a socket's path.
func newHolder() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "g" + hex.EncodeToString(b)
}

// describeWant says what a run asks the grid for, as its error and the
// queue both show it.
func describeWant(serial, platform string) string {
	want := "a device"
	switch {
	case serial != "":
		want = serial
	case platform != "":
		want = "an " + platform + " device"
	}
	if m := os.Getenv("MOBIUM_GRID_MODEL"); m != "" {
		want += ", model " + m
	}
	if o := os.Getenv("MOBIUM_GRID_OS"); o != "" {
		want += ", OS " + o
	}
	return want
}

func noDevice(serial, platform string, wait time.Duration, busy, down []string) error {
	want := describeWant(serial, platform)
	msg := fmt.Sprintf("no node in MOBIUM_GRID had %s free within %s", want, wait)
	if len(busy) > 0 {
		msg += "; busy: " + strings.Join(busy, ", ")
	}
	if len(down) > 0 {
		msg += "; not answering: " + strings.Join(down, ", ")
	}
	return mobiumerr.New(mobiumerr.NoDevice, "%s", msg).
		WithRemedy("wait for a run to finish, raise MOBIUM_GRID_WAIT, or add a node to MOBIUM_GRID")
}
