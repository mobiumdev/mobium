// Package webview attaches to the Chrome DevTools Protocol endpoints Android
// WebViews expose, so a hybrid app's web content can be mapped and acted on
// with the same refs as its native shell.
//
// The original plan was to hand a WebView to vibium and reuse its element
// commands unchanged. That does not work: vibium speaks WebDriver BiDi and
// nothing else, while an Android WebView exposes CDP 1.3 and has no BiDi
// endpoint at all (`GET /session` on the devtools socket returns 404). So the
// small slice of CDP that mobium needs — evaluate an expression, read the
// layout metrics — is implemented here instead.
//
// Only reading is done over CDP. Taps still go through the native driver,
// which already knows how to touch a point, so none of CDP's input domain is
// needed and a web element is acted on exactly like a native one.
package webview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Socket is a devtools endpoint published by an app on the device.
type Socket struct {
	// Name is the abstract socket name, e.g. "webview_devtools_remote_1508".
	Name string
	// Package is the owning app when the socket name reveals it.
	Package string
	// PID is the owning process when the socket name carries it.
	PID string
}

// Context is one attachable web target inside an app.
type Context struct {
	// ID is the name an agent switches to, e.g. "WEBVIEW_com.example".
	ID     string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Socket string `json:"-"`
	// TargetID is the page's CDP target id, which CloseTarget takes. Stable
	// for the page's life, where ID is a position in the listing.
	TargetID string `json:"-"`
	// WSURL is the CDP WebSocket for this target.
	WSURL string `json:"-"`
}

// NativeContext is the name of the native, non-web context.
const NativeContext = "NATIVE_APP"

var socketRe = regexp.MustCompile(`@(webview_devtools_remote_(\S+)|chrome_devtools_remote(?:_(\S+))?)`)

// Sockets lists the devtools sockets currently open on the device.
//
// /proc/net/unix is the only place these are enumerable; there is no adb
// command for it.
func Sockets(ctx context.Context, adb *device.ADB) ([]Socket, error) {
	out, err := adb.Shell(ctx, "cat", "/proc/net/unix")
	if err != nil {
		return nil, fmt.Errorf("list devtools sockets: %w", err)
	}

	seen := map[string]bool{}
	var sockets []Socket
	for _, m := range socketRe.FindAllStringSubmatch(string(out), -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true

		s := Socket{Name: name}
		// A WebView socket is suffixed with the owning pid; Chrome's is not.
		if suffix := m[2]; suffix != "" {
			s.PID = suffix
		}
		sockets = append(sockets, s)
	}
	sort.Slice(sockets, func(i, j int) bool { return sockets[i].Name < sockets[j].Name })
	return sockets, nil
}

// listTimeout bounds the HTTP calls to a forwarded devtools socket.
const listTimeout = 10 * time.Second

// target is one entry of /json/list.
type target struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// version is /json/version, which names the owning app.
type version struct {
	AndroidPackage string `json:"Android-Package"`
	Browser        string `json:"Browser"`
}

// Contexts enumerates every attachable web context on the device.
//
// Each socket is forwarded, queried and unforwarded in turn: leaving a forward
// per socket in place would leak a host port for every WebView the device ever
// opens.
func Contexts(ctx context.Context, adb *device.ADB) ([]Context, error) {
	sockets, err := Sockets(ctx, adb)
	if err != nil {
		return nil, err
	}

	var out []Context
	for _, s := range sockets {
		found, err := contextsOn(ctx, adb, s)
		if err != nil {
			// One unreachable socket must not hide the others: a WebView can
			// disappear between listing and querying.
			continue
		}
		out = append(out, found...)
	}
	return out, nil
}

func contextsOn(ctx context.Context, adb *device.ADB, s Socket) ([]Context, error) {
	port, err := adb.ForwardAbstract(ctx, s.Name)
	if err != nil {
		return nil, err
	}
	defer adb.RemoveForward(ctx, port)

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	pkg := s.Package
	if v, err := getJSON[version](ctx, base+"/json/version"); err == nil && v.AndroidPackage != "" {
		pkg = v.AndroidPackage
	}

	targets, err := getJSON[[]target](ctx, base+"/json/list")
	if err != nil {
		return nil, err
	}

	name := pkg
	if name == "" {
		name = strings.TrimPrefix(s.Name, "webview_devtools_remote_")
	}

	var out []Context
	for i, t := range *targets {
		if t.Type != "page" || t.WebSocketDebuggerURL == "" {
			continue
		}
		id := "WEBVIEW_" + name
		if i > 0 {
			id = fmt.Sprintf("%s_%d", id, i)
		}
		out = append(out, Context{
			ID:       id,
			Title:    t.Title,
			URL:      t.URL,
			Socket:   s.Name,
			TargetID: t.ID,
			// The URL embeds the port this forward used, which is torn down
			// on return; Attach re-forwards and rewrites it.
			WSURL: t.WebSocketDebuggerURL,
		})
	}
	return out, nil
}

// CloseTarget closes one page — a browser tab — by its target id, through
// the devtools endpoint that published it. Chrome answers /json/close with
// "Target is closing"; an id it does not know is an error, so a tab already
// gone says so rather than passing silently.
func CloseTarget(ctx context.Context, adb *device.ADB, socket, targetID string) error {
	port, err := adb.ForwardAbstract(ctx, socket)
	if err != nil {
		return err
	}
	defer adb.RemoveForward(ctx, port)
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/json/close/%s", port, targetID), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return mobiumerr.New(mobiumerr.DeviceServer, "closing tab %s: %s %s", targetID, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func getJSON[T any](ctx context.Context, url string) (*T, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "GET %s: %s", url, resp.Status)
	}

	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	return &v, nil
}
