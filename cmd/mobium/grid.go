package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/paths"
	"github.com/spf13/cobra"
)

// A grid is Selenium Grid's shape without its hub: MOBIUM_GRID names the
// nodes, the caller's own mobium routes each run to a free device that
// matches, and the lease that keeps a device to one run lives on the node, so
// callers on different machines cannot take the same one. Nothing new listens
// anywhere — every step is SSH, and the connection is --remote's (Stage 2).

const (
	leaseTTL   = 60 * time.Second // a lease no heartbeat has renewed for this long is free
	leaseEvery = 20 * time.Second // how often a holder renews
)

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
	Devices []agent.DeviceView `json:"devices"`
	Leases  map[string]string  `json:"leases"` // serial -> holder, live leases only
}

// leaseAnswer is what `mobium grid lease` prints.
type leaseAnswer struct {
	OK     bool   `json:"ok"`
	Holder string `json:"holder"`
}

func newGridCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "grid",
		Short:  "The node's half of a mobium grid, run over SSH by the caller's mobium",
		Hidden: true,
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
			var st nodeStatus
			if err := remarshal(res.StructuredContent, &st); err != nil {
				return err
			}
			if st.Leases, err = liveLeases(); err != nil {
				return err
			}
			return printJSON(st)
		},
	}
	lease := &cobra.Command{
		Use:  "lease <serial>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			got, err := takeLease(args[0], holder)
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
			return dropLease(args[0], holder)
		},
	}
	for _, c := range []*cobra.Command{lease, release} {
		c.Flags().StringVar(&holder, "holder", "", "who holds the lease")
		_ = c.MarkFlagRequired("holder")
	}
	cmd.AddCommand(node, lease, release)
	return cmd
}

var unsafeSerial = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func leaseDir() (string, error) {
	dir := filepath.Join(paths.Root(), "leases")
	return dir, os.MkdirAll(dir, 0o700)
}

func leaseFile(serial string) (string, error) {
	dir, err := leaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, unsafeSerial.ReplaceAllString(serial, "_")), nil
}

// takeLease gives a device to a holder: a new lease if there is none or the
// last one lapsed, a renewal if the holder already has it, and a refusal
// naming the holder otherwise. The file is created exclusively, so two
// callers asking at once cannot both be told yes.
func takeLease(serial, holder string) (leaseAnswer, error) {
	f, err := leaseFile(serial)
	if err != nil {
		return leaseAnswer{}, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		if w, err := os.OpenFile(f, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err == nil {
			_, err = w.WriteString(holder)
			w.Close()
			return leaseAnswer{OK: err == nil, Holder: holder}, err
		}
		held, info, err := readLease(f)
		if err != nil {
			continue // released between the two looks; try again
		}
		if held == holder {
			now := time.Now()
			return leaseAnswer{OK: true, Holder: holder}, os.Chtimes(f, now, now)
		}
		if time.Since(info.ModTime()) < leaseTTL {
			return leaseAnswer{OK: false, Holder: held}, nil
		}
		// Lapsed: its holder stopped renewing, most likely because it died.
		_ = os.Remove(f)
	}
	return leaseAnswer{}, mobiumerr.New(mobiumerr.DeviceServer, "could not take the lease on %s", serial)
}

func dropLease(serial, holder string) error {
	f, err := leaseFile(serial)
	if err != nil {
		return err
	}
	if held, _, err := readLease(f); err == nil && held == holder {
		return os.Remove(f)
	}
	return nil
}

func readLease(f string) (string, os.FileInfo, error) {
	info, err := os.Stat(f)
	if err != nil {
		return "", nil, err
	}
	b, err := os.ReadFile(f)
	return strings.TrimSpace(string(b)), info, err
}

func liveLeases() (map[string]string, error) {
	dir, err := leaseDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		held, info, err := readLease(filepath.Join(dir, e.Name()))
		if err == nil && time.Since(info.ModTime()) < leaseTTL {
			out[e.Name()] = held
		}
	}
	return out, nil
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
	nodes := gridNodes()
	deadline := time.Now().Add(wait)

	for {
		statuses, down := askNodes(nodes)
		var busy []string
		for _, node := range nodes {
			st, ok := statuses[node]
			if !ok {
				continue
			}
			for _, d := range st.Devices {
				if !deviceReady(d) || (serial != "" && d.ID != serial) || (platform != "" && d.Platform != platform) {
					continue
				}
				if h, held := st.Leases[unsafeSerial.ReplaceAllString(d.ID, "_")]; held {
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
			err := runJSON(sshCommand(n, "grid node --json"), &st)
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

func askLease(node, verb, serial string) (leaseAnswer, error) {
	var got leaseAnswer
	cmd := sshCommand(node, fmt.Sprintf("grid %s %s --holder %s", verb, shellWord(serial), gridHolder))
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

// newHolder names this run in a lease: the machine, the process, and a
// random part, since two runs on one machine are two holders.
func newHolder() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "caller"
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%d-%s", unsafeSerial.ReplaceAllString(host, "_"), os.Getpid(), hex.EncodeToString(b))
}

func noDevice(serial, platform string, wait time.Duration, busy, down []string) error {
	want := "a device"
	switch {
	case serial != "":
		want = serial
	case platform != "":
		want = "an " + platform + " device"
	}
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
