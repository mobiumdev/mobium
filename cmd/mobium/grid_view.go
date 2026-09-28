package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/mobiumdev/mobium/internal/grid"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/spf13/cobra"
)

// gridView is the grid as its nodes report it now — what `grid status`
// prints and `grid ui` shows. It is built from the same `grid node` answers
// the router reads, so the view cannot disagree with where runs are routed.
type gridView struct {
	At    time.Time  `json:"at"`
	Nodes []nodeView `json:"nodes"`
}

type nodeView struct {
	Name    string         `json:"name"`
	Up      bool           `json:"up"`
	Error   string         `json:"error,omitempty"`
	Devices []deviceView   `json:"devices"`
	Waiting []grid.Waiting `json:"waiting"`
}

type deviceView struct {
	gridDevice
	Lease *grid.Lease `json:"lease,omitempty"`
}

// readGrid asks every node at once, as routing does.
func readGrid() (gridView, error) {
	nodes := gridNodes()
	if len(nodes) == 0 {
		return gridView{}, mobiumerr.New(mobiumerr.InvalidArgument, "MOBIUM_GRID names no nodes").
			WithRemedy("set MOBIUM_GRID=node1,node2 — the machines whose devices the grid spreads runs over")
	}
	view := gridView{At: time.Now(), Nodes: make([]nodeView, len(nodes))}
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Add(1)
		go func(i int, n string) {
			defer wg.Done()
			nv := nodeView{Name: n}
			var st nodeStatus
			if err := runJSON(sshSession(n, "", "grid node --json"), &st); err != nil {
				nv.Error = err.Error()
			} else {
				nv.Up, nv.Waiting = true, st.Waiting
				for _, d := range st.Devices {
					dv := deviceView{gridDevice: d}
					if l, ok := st.Held[grid.Key(d.ID)]; ok {
						l := l
						dv.Lease = &l
					}
					nv.Devices = append(nv.Devices, dv)
				}
			}
			view.Nodes[i] = nv
		}(i, n)
	}
	wg.Wait()
	return view, nil
}

func newGridStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print each node's devices, who holds them, and who is waiting",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := readGrid()
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(view)
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "NODE\tDEVICE\tPLATFORM\tOS\tMODEL\tSTATE\tHELD BY")
			for _, n := range view.Nodes {
				if !n.Up {
					fmt.Fprintf(w, "%s\t(not answering: %s)\t\t\t\t\t\n", n.Name, firstLine(n.Error))
					continue
				}
				if len(n.Devices) == 0 {
					fmt.Fprintf(w, "%s\t(no devices)\t\t\t\t\t\n", n.Name)
				}
				for _, d := range n.Devices {
					held := "free"
					if !d.Offered {
						held = "not offered (a phone; MOBIUM_GRID_PHONES=1 on the node lends it)"
					}
					if d.Lease != nil {
						held = fmt.Sprintf("%s for %s", d.Lease.Holder, time.Since(d.Lease.Since).Round(time.Second))
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", n.Name, d.ID, d.Platform, d.OS, d.Model, d.State, held)
				}
			}
			if err := w.Flush(); err != nil {
				return err
			}
			var queued []string
			for _, n := range view.Nodes {
				for _, q := range n.Waiting {
					queued = append(queued, fmt.Sprintf("  %s wants %s, waiting %s (seen by %s)",
						q.Holder, q.Want, time.Since(q.Since).Round(time.Second), n.Name))
				}
			}
			if len(queued) > 0 {
				sort.Strings(queued)
				fmt.Println("\nwaiting:")
				fmt.Println(strings.Join(queued, "\n"))
			}
			return nil
		},
	}
}

//go:embed grid_ui.html
var gridPage []byte

func newGridUICmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Serve the grid's status as a page on this machine",
		Long: "Serves a page showing each node, its devices, who holds each and for how\\n" +
			"long, and who is waiting — refreshed every few seconds from the nodes. It\\n" +
			"listens on 127.0.0.1 only: nothing off this machine can reach it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(gridNodes()) == 0 {
				_, err := readGrid()
				return err
			}
			// One reading at a time, and reused for two seconds, so a page left
			// open in several tabs does not multiply the SSH calls.
			var mu sync.Mutex
			var last gridView
			var lastAt time.Time
			mux := http.NewServeMux()
			mux.HandleFunc("/status.json", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				if time.Since(lastAt) > 2*time.Second {
					if v, err := readGrid(); err == nil {
						last, lastAt = v, time.Now()
					}
				}
				v := last
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				_ = json.NewEncoder(w).Encode(v)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write(gridPage)
			})
			ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				return err
			}
			fmt.Printf("grid ui on http://%s — Ctrl-C to stop\n", ln.Addr())
			srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, os.Interrupt)
			go func() { <-stop; _ = srv.Close() }()
			if err := srv.Serve(ln); err != http.ErrServerClosed {
				return err
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "port on 127.0.0.1 (default: any free one)")
	return cmd
}

func firstLine(s string) string { return strings.TrimSpace(strings.SplitN(s, "\n", 2)[0]) }
