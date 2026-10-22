package device

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Android's proxy is a device-wide setting, `http_proxy` in the global
// table, and the one in force is not that key but the two Android derives
// from it: writing http_proxy sets global_http_proxy_host and _port, and
// deleting it leaves them — and the proxy — in place. Measured on the
// Android 15 emulator, 2026-10-22. `:0` is Android's "none": it empties
// the host and sets the port to 0, after which the keys can be deleted.
// docs/PROXY.md.
const (
	proxySetting     = "http_proxy"
	proxyHostSetting = "global_http_proxy_host"
	proxyPortSetting = "global_http_proxy_port"
)

// proxySettings are every key a proxy leaves in the global table, deleted
// once it is cleared so an unset device is unset again.
var proxySettings = []string{proxySetting, proxyHostSetting, proxyPortSetting,
	"global_http_proxy_exclusion_list", "global_proxy_pac_url"}

// proxySettle bounds the wait for Android to derive the proxy in force
// from what was written; it took under a second where measured.
const proxySettle = 5 * time.Second

// Proxy reads the proxy in force, as host:port, and "" for none.
func (a *ADB) Proxy(ctx context.Context) (string, error) {
	host, err := a.globalSetting(ctx, proxyHostSetting)
	if err != nil {
		return "", err
	}
	port, err := a.globalSetting(ctx, proxyPortSetting)
	if err != nil {
		return "", err
	}
	if host == "" || port == "" || port == "0" {
		return "", nil
	}
	return net.JoinHostPort(host, port), nil
}

// globalSetting reads a key of the global settings table, "" when unset.
func (a *ADB) globalSetting(ctx context.Context, key string) (string, error) {
	out, err := a.Shell(ctx, "settings", "get", "global", key)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(out))
	if v == "null" {
		return "", nil
	}
	return v, nil
}

// SetProxy points the device at hostport, or takes the proxy off for "",
// and waits for the proxy in force to be what was asked. It writes the
// setting only; a proxy on the device's loopback reaches this machine
// through Reverse.
func (a *ADB) SetProxy(ctx context.Context, hostport string) error {
	if hostport == "" {
		if err := a.settings(ctx, "put", proxySetting, ":0"); err != nil {
			return err
		}
		if err := a.awaitProxy(ctx, ""); err != nil {
			return err
		}
		for _, k := range proxySettings {
			if err := a.settings(ctx, "delete", k); err != nil {
				return err
			}
		}
		return a.awaitProxy(ctx, "")
	}
	if err := a.settings(ctx, "put", proxySetting, hostport); err != nil {
		return err
	}
	return a.awaitProxy(ctx, hostport)
}

// settings writes or deletes a key of the global table. Its stdout is
// ordinary ("Deleted 1 rows"); anything on stderr is a failure, since
// settings exits 0 either way.
func (a *ADB) settings(ctx context.Context, verb string, args ...string) error {
	_, diag, err := a.ShellDiagnostics(ctx, append([]string{"settings", verb, "global"}, args...)...)
	if err != nil {
		return err
	}
	if msg := strings.TrimSpace(string(diag)); msg != "" {
		return mobiumerr.New(mobiumerr.DeviceServer, "settings %s %s failed: %s", verb, args[0], firstLine(msg))
	}
	return nil
}

// awaitProxy waits for the proxy in force to be want.
func (a *ADB) awaitProxy(ctx context.Context, want string) error {
	deadline := time.Now().Add(proxySettle)
	for {
		got, err := a.Proxy(ctx)
		if err != nil {
			return err
		}
		if got == want {
			return nil
		}
		if time.Now().After(deadline) {
			if want == "" {
				return mobiumerr.New(mobiumerr.NotConfirmed, "the proxy was taken off and the device still uses %s after %s", got, proxySettle)
			}
			if got == "" {
				got = "none"
			}
			return mobiumerr.New(mobiumerr.NotConfirmed, "the proxy was set to %s and the device uses %s after %s", want, got, proxySettle)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// IsLoopback reports whether a proxy's host is the device's own loopback,
// which only `adb reverse` makes the Mac.
func IsLoopback(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Reversed reports whether the device's port reaches this machine already.
func (a *ADB) Reversed(ctx context.Context, port int) (bool, error) {
	out, err := a.Run(ctx, "reverse", "--list")
	if err != nil {
		return false, err
	}
	want := fmt.Sprintf("tcp:%d", port)
	for _, line := range strings.Split(string(out), "\n") {
		// "<serial> tcp:8888 tcp:8888", or on older adb "(reverse) tcp:8888 tcp:8888".
		f := strings.Fields(line)
		if len(f) == 3 && f[1] == want {
			return true, nil
		}
	}
	return false, nil
}

// Reverse makes the device's port reach the same port on this machine, and
// says whether it added the reverse: one already there is someone else's.
func (a *ADB) Reverse(ctx context.Context, port int) (added bool, err error) {
	if ok, err := a.Reversed(ctx, port); err != nil || ok {
		return false, err
	}
	spec := fmt.Sprintf("tcp:%d", port)
	if _, err := a.Run(ctx, "reverse", spec, spec); err != nil {
		return false, err
	}
	ok, err := a.Reversed(ctx, port)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, mobiumerr.New(mobiumerr.NotConfirmed, "adb reverse %s was accepted and is not in its list", spec)
	}
	return true, nil
}

// Unreverse removes the device's port's reverse.
func (a *ADB) Unreverse(ctx context.Context, port int) error {
	if ok, err := a.Reversed(ctx, port); err != nil || !ok {
		return err
	}
	_, err := a.Run(ctx, "reverse", "--remove", fmt.Sprintf("tcp:%d", port))
	return err
}

// ProxyPort is the port of a host:port, which ParseProxy has checked.
func ProxyPort(hostport string) int {
	_, p, _ := net.SplitHostPort(hostport)
	n, _ := strconv.Atoi(p)
	return n
}

// ParseProxy checks a proxy as the caller gave it: a host and a port, no
// scheme, no path. Android's setting takes nothing else, and accepts
// anything — a scheme would be written and used as a host name.
func ParseProxy(s string) (string, error) {
	if strings.Contains(s, "://") || strings.Contains(s, "/") {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "proxy is host:port, with no scheme or path: got %q", s)
	}
	host, p, err := net.SplitHostPort(s)
	if err != nil || host == "" {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "proxy is host:port, such as 127.0.0.1:8888: got %q", s)
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "proxy's port must be from 1 to 65535: got %q", p)
	}
	return net.JoinHostPort(host, strconv.Itoa(n)), nil
}
