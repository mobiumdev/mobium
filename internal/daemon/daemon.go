// Package daemon keeps mobium's tool layer alive between CLI invocations.
//
// Today it holds the @ref table and the selected device, which a per-command
// process cannot. It exists now rather than later because the UiAutomator2 and
// WebDriverAgent sessions of steps 3 and 4 must be long-lived, and this is
// where they will live.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/paths"
)

// Daemon serves the tool layer over a Unix socket, or a named pipe on Windows.
type Daemon struct {
	listener     net.Listener
	handlers     *agent.Handlers
	mu           sync.Mutex     // serializes handler access
	wg           sync.WaitGroup // tracks in-flight connections
	version      string
	startTime    time.Time
	lastActivity time.Time
	idleTimeout  time.Duration
	socketPath   string
	shutdownOnce sync.Once
	done         chan struct{}
	shutdownDone chan struct{}

	// closeHandlers ends every device session. A field so a test can make it
	// hang the way a stuck tool call makes the real one hang.
	closeHandlers func()
}

// closeTimeout bounds closing the device sessions at shutdown. A var so
// tests can shrink it. Closing is usually a second or two — stopping an
// instrumentation, deleting a WebDriverAgent session — but putting a real
// iPhone's accessibility settings back goes through its Settings app, about
// ten seconds a page, and at 20s the stop ran out after two of six and left
// four of a person's settings changed (CHALLENGES 160). Running out is the
// worse harm, so it is generous.
var closeTimeout = 75 * time.Second

// Options configures a Daemon.
type Options struct {
	Version     string
	IdleTimeout time.Duration
}

// New creates a Daemon.
func New(opts Options) *Daemon {
	d := &Daemon{
		handlers:     agent.NewHandlers(),
		version:      opts.Version,
		idleTimeout:  opts.IdleTimeout,
		startTime:    time.Now(),
		lastActivity: time.Now(),
		done:         make(chan struct{}),
		shutdownDone: make(chan struct{}),
	}
	d.closeHandlers = d.handlers.Close
	return d
}

// Run listens until the context is canceled or the daemon is shut down.
func (d *Daemon) Run(ctx context.Context) error {
	socketPath, err := paths.SocketPath()
	if err != nil {
		return err
	}
	d.socketPath = socketPath

	if err := paths.EnsureDaemonDir(); err != nil {
		return err
	}
	listener, err := listen(socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	d.listener = listener

	if err := WritePID(); err != nil {
		listener.Close()
		return err
	}

	if d.idleTimeout > 0 {
		go d.watchIdle(ctx)
	}
	go func() {
		<-ctx.Done()
		d.Shutdown()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-d.done:
				<-d.shutdownDone
				return nil
			default:
				continue
			}
		}
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.handleConnection(conn)
		}()
	}
}

// Shutdown stops the daemon and cleans up its socket and PID file.
func (d *Daemon) Shutdown() {
	d.shutdownOnce.Do(func() {
		defer close(d.shutdownDone)
		close(d.done)

		if d.listener != nil {
			d.listener.Close()
		}

		// Let in-flight calls finish before tearing down handler state.
		waitDone := make(chan struct{})
		go func() { d.wg.Wait(); close(waitDone) }()
		select {
		case <-waitDone:
		case <-time.After(10 * time.Second):
		}

		// Bounded, because a tool call that never returns holds the lock
		// Close needs. Unbounded, a stuck call made SIGTERM and `daemon stop`
		// hang for good — with the socket already gone, since closing the
		// listener unlinks it, so the daemon looked dead to every client
		// while its PID file said otherwise and refused a replacement.
		// Exiting with a session still open is the lesser harm, and it is
		// said, not hidden: SHUTDOWN.md says how to clear what it leaves.
		closed := make(chan struct{})
		go func() {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.closeHandlers()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(closeTimeout):
			fmt.Fprintf(os.Stderr, "mobium daemon: a tool call is still running after %s, so its device "+
				"session was left open — see docs/SHUTDOWN.md to clear what it left on the device\n", closeTimeout)
		}

		if d.socketPath != "" {
			removeSocket(d.socketPath)
		}
		if err := RemovePID(); err != nil {
			// Said, because a PID file left behind names a daemon that is
			// gone, and whoever is waiting for it to go waits out the grace.
			fmt.Fprintf(os.Stderr, "mobium daemon: could not remove the PID file: %v\n", err)
		}
	})
}

// SocketPath is where the daemon is listening; empty before Run binds.
func (d *Daemon) SocketPath() string { return d.socketPath }

func (d *Daemon) touchActivity() {
	d.mu.Lock()
	d.lastActivity = time.Now()
	d.mu.Unlock()
}

// watchIdle shuts the daemon down once nothing has used it for idleTimeout.
func (d *Daemon) watchIdle(ctx context.Context) {
	tick := d.idleTimeout / 4
	if tick > time.Minute {
		tick = time.Minute
	}
	if tick < time.Second {
		tick = time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.mu.Lock()
			idle := time.Since(d.lastActivity)
			d.mu.Unlock()
			if idle >= d.idleTimeout {
				d.Shutdown()
				return
			}
		case <-d.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// progressMethod is the notification the daemon writes while a tool call does
// slow one-time work — downloading and installing the UiAutomator2 server.
//
// It serves two purposes, both learned from vibium: the client can tell the
// user why the command is taking a minute, and it can extend its read deadline
// so a legitimately slow install is not cut off as a wedged daemon.
const progressMethod = "mobium/progress"

// connReadTimeout bounds how long a client may take to send its request.
const connReadTimeout = 60 * time.Second

// connWriteTimeout bounds one response write.
const connWriteTimeout = 60 * time.Second

// handleConnection serves one request/response exchange.
func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()
	d.touchActivity()

	conn.SetReadDeadline(time.Now().Add(connReadTimeout))
	// bufio.Reader grows as needed; a Scanner would cap the request size.
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}

	// Progress notifications go out on this connection ahead of the
	// response. The handler runs synchronously below, so these writes cannot
	// interleave with the response write.
	notify := func(msg string) {
		payload, err := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  progressMethod,
			"params":  map[string]string{"message": msg},
		})
		if err != nil {
			return
		}
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprintf(conn, "%s\n", payload)
	}

	resp := d.handleRequest(line, notify)
	if resp == nil {
		return
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	conn.SetWriteDeadline(time.Now().Add(connWriteTimeout))
	fmt.Fprintf(conn, "%s\n", data)
}

func (d *Daemon) handleRequest(data []byte, notify func(string)) *agent.Response {
	var req agent.Request
	if err := json.Unmarshal(data, &req); err != nil {
		return &agent.Response{JSONRPC: "2.0", Error: &agent.Error{
			Code: agent.ParseError, Message: "Parse error", Data: err.Error(),
		}}
	}
	if req.JSONRPC != "2.0" {
		return &agent.Response{JSONRPC: "2.0", ID: req.ID, Error: &agent.Error{
			Code: agent.InvalidRequest, Message: "Invalid Request", Data: "jsonrpc must be '2.0'",
		}}
	}

	// The handlers are not safe for concurrent use, and two CLI commands can
	// arrive at once. The progress callback targets this request's
	// connection, so it is installed and cleared under the same lock.
	d.mu.Lock()
	d.handlers.SetProgress(notify)
	result, rpcErr := agent.Route(req, d.handlers, d.version, d.daemonMethods)
	d.handlers.SetProgress(nil)
	d.mu.Unlock()

	if req.ID == nil {
		return nil
	}
	if rpcErr != nil {
		return &agent.Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
	}
	return &agent.Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// daemonMethods serves the methods only the daemon has; everything else falls
// through to the shared MCP routing.
func (d *Daemon) daemonMethods(req agent.Request) (interface{}, *agent.Error, bool) {
	switch req.Method {
	case "daemon/status":
		return StatusResult{
			Version:   d.version,
			PID:       os.Getpid(),
			Uptime:    time.Since(d.startTime).Truncate(time.Second).String(),
			Socket:    d.socketPath,
			StartTime: d.startTime.Format(time.RFC3339),
			Session:   paths.SessionName(),
		}, nil, true
	case "daemon/shutdown":
		// Asynchronous so this response is written before the socket closes.
		go d.Shutdown()
		return map[string]string{"status": "shutting down"}, nil, true
	}
	return nil, nil, false
}

// StatusResult is returned by daemon/status.
type StatusResult struct {
	Version   string `json:"version"`
	PID       int    `json:"pid"`
	Uptime    string `json:"uptime"`
	Socket    string `json:"socket"`
	StartTime string `json:"startTime"`
	Session   string `json:"session"`
}

// DropLogStreams drops every phone session's log stream (SIGUSR1).
func (d *Daemon) DropLogStreams() int {
	return d.handlers.DropLogStreams()
}
