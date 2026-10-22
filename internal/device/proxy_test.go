package device

import "testing"

// A proxy is host:port and nothing else: Android's setting accepts anything
// and would take a scheme as a host name.
func TestParseProxy(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1:8888": "127.0.0.1:8888",
		"localhost:8080": "localhost:8080",
		"10.0.0.5:08080": "10.0.0.5:8080",
		"[::1]:8888":     "[::1]:8888",
		"proxy.lan:3128": "proxy.lan:3128",
	} {
		if got, err := ParseProxy(in); err != nil || got != want {
			t.Errorf("ParseProxy(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "127.0.0.1", ":8888", "http://127.0.0.1:8888", "127.0.0.1:8888/x",
		"127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:port"} {
		if got, err := ParseProxy(in); err == nil {
			t.Errorf("ParseProxy(%q) = %q, accepted", in, got)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	for in, want := range map[string]bool{
		"127.0.0.1:8888": true, "localhost:8888": true, "[::1]:8888": true, "127.1.2.3:80": true,
		"10.0.2.2:8888": false, "192.168.1.4:8888": false, "proxy.lan:3128": false,
	} {
		if got := IsLoopback(in); got != want {
			t.Errorf("IsLoopback(%q) = %v", in, got)
		}
	}
}
