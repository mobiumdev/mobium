package plist

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The values this codec has to carry, drawn from real Remote Web Inspector
// traffic rather than invented: the connection handshake, a page listing, and
// a forwarded inspector message with a JSON payload in a data field.
func realMessages() map[string]any {
	return map[string]any{
		"handshake": map[string]any{
			"__selector": "_rpc_reportIdentifier:",
			"__argument": map[string]any{"WIRConnectionIdentifierKey": "mobium-1"},
		},
		"listing": map[string]any{
			"__selector": "_rpc_applicationSentListing:",
			"__argument": map[string]any{
				"WIRApplicationIdentifierKey": "PID:48408",
				"WIRListingKey": map[string]any{
					"1": map[string]any{
						"WIRPageIdentifierKey": int64(1),
						"WIRTitleKey":          "Example Domain",
						"WIRURLKey":            "https://example.com/",
						"WIRTypeKey":           "WIRTypeWeb",
					},
				},
			},
		},
		"forward": map[string]any{
			"__selector": "_rpc_forwardSocketData:",
			"__argument": map[string]any{
				"WIRConnectionIdentifierKey": "mobium-1",
				"WIRPageIdentifierKey":       int64(30),
				"WIRAutomaticallyPause":      false,
				"WIRSocketDataKey":           []byte(`{"id":1,"method":"Runtime.evaluate"}`),
			},
		},
	}
}

func TestRoundTrip(t *testing.T) {
	for name, msg := range realMessages() {
		t.Run(name, func(t *testing.T) {
			raw, err := Marshal(msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			back, err := Unmarshal(raw)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(msg, back) {
				t.Errorf("round trip changed the value:\n want %#v\n got  %#v", msg, back)
			}
		})
	}
}

// TestAgreesWithApple is the test that matters. A codec that round-trips
// through itself proves only that it is self-consistent; it can be
// self-consistently wrong, and webinspectord would be the one to tell us —
// unhelpfully, and only on a booted simulator. Python's plistlib is Apple's
// format implemented by someone else, and it is already installed anywhere
// simctl is.
func TestAgreesWithApple(t *testing.T) {
	python := lookPython(t)
	dir := t.TempDir()

	for name, msg := range realMessages() {
		t.Run(name+"/we write, python reads", func(t *testing.T) {
			raw, err := Marshal(msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			path := filepath.Join(dir, name+".plist")
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			out := run(t, python, "-c", `
import plistlib, sys, json
with open(sys.argv[1], "rb") as f:
    v = plistlib.load(f, fmt=plistlib.FMT_BINARY)
def norm(x):
    if isinstance(x, bytes):  return {"__data__": x.decode()}
    if isinstance(x, dict):   return {k: norm(v) for k, v in sorted(x.items())}
    if isinstance(x, list):   return [norm(i) for i in x]
    return x
print(json.dumps(norm(v), sort_keys=True))`, path)
			want := run(t, python, "-c", `
import json, sys
print(json.dumps(json.loads(sys.argv[1]), sort_keys=True))`, expectedJSON(msg))
			if strings.TrimSpace(out) != strings.TrimSpace(want) {
				t.Errorf("Apple's reader saw something else:\n python %s\n want   %s", out, want)
			}
		})

		t.Run(name+"/python writes, we read", func(t *testing.T) {
			path := filepath.Join(dir, name+"-py.plist")
			run(t, python, "-c", `
import plistlib, sys, json
def denorm(x):
    if isinstance(x, dict) and set(x) == {"__data__"}: return x["__data__"].encode()
    if isinstance(x, dict):  return {k: denorm(v) for k, v in x.items()}
    if isinstance(x, list):  return [denorm(i) for i in x]
    return x
v = denorm(json.loads(sys.argv[1]))
with open(sys.argv[2], "wb") as f:
    plistlib.dump(v, f, fmt=plistlib.FMT_BINARY)`, expectedJSON(msg), path)

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Unmarshal(raw)
			if err != nil {
				t.Fatalf("unmarshal Apple's bytes: %v", err)
			}
			if !reflect.DeepEqual(msg, got) {
				t.Errorf("we read Apple's bytes differently:\n want %#v\n got  %#v", msg, got)
			}
		})
	}
}

// TestAwkwardValues covers what the happy path does not.
func TestAwkwardValues(t *testing.T) {
	python := lookPython(t)
	cases := map[string]any{
		"empty dict":     map[string]any{},
		"empty string":   map[string]any{"k": ""},
		"empty data":     map[string]any{"k": []byte{}},
		"empty array":    map[string]any{"k": []any{}},
		"negative":       map[string]any{"k": int64(-1)},
		"large int":      map[string]any{"k": int64(1) << 40},
		"boundary 255":   map[string]any{"k": int64(255)},
		"boundary 256":   map[string]any{"k": int64(256)},
		"boundary 65535": map[string]any{"k": int64(65535)},
		"boundary 65536": map[string]any{"k": int64(65536)},
		"true and false": map[string]any{"t": true, "f": false},
		"non-ascii":      map[string]any{"k": "Wi‑Fi"},
		"emoji":          map[string]any{"k": "a 🐛 b"},
		"nested":         map[string]any{"a": map[string]any{"b": []any{"c", int64(1)}}},
		"long string":    map[string]any{"k": strings.Repeat("x", 300)},
		"many keys":      manyKeys(40),
		"binary data":    map[string]any{"k": []byte{0x00, 0xFF, 0x10, 0x7F, 0x80}},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := Marshal(v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			back, err := Unmarshal(raw)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(v, back) {
				t.Fatalf("round trip changed it:\n want %#v\n got  %#v", v, back)
			}
			// And Apple has to agree it is a valid plist at all.
			path := filepath.Join(t.TempDir(), "v.plist")
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			run(t, python, "-c",
				"import plistlib,sys;plistlib.load(open(sys.argv[1],'rb'),fmt=plistlib.FMT_BINARY)", path)
		})
	}
}

func TestRefusesWhatItCannotEncode(t *testing.T) {
	// Silently dropping a value would produce a plist that webinspectord
	// rejects with no clue why.
	if _, err := Marshal(map[string]any{"k": struct{ A int }{1}}); err == nil {
		t.Error("a struct was accepted")
	}
	if _, err := Marshal(map[string]any{"k": make(chan int)}); err == nil {
		t.Error("a channel was accepted")
	}
}

func TestRejectsMalformedInput(t *testing.T) {
	good, err := Marshal(map[string]any{"a": "b"})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"empty":          {},
		"header only":    []byte("bplist00"),
		"wrong magic":    append([]byte("plist000"), good[8:]...),
		"truncated body": good[:len(good)-10],
		"truncated head": good[:12],
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Unmarshal(b); err == nil {
				t.Error("accepted malformed input")
			}
		})
	}
}

func TestMarshalIsDeterministic(t *testing.T) {
	// Same input, same bytes: a failure has to be reproducible.
	v := manyKeys(20)
	first, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("the same value encoded to different bytes")
		}
	}
}

func manyKeys(n int) map[string]any {
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		m[string(rune('a'+i%26))+strings.Repeat("k", i/26+1)] = int64(i)
	}
	return m
}

func lookPython(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3, so Apple's own reader cannot be consulted")
	}
	return p
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, errb.String())
	}
	return out.String()
}

// expectedJSON renders a Go value the way the Python side of these tests
// expects it: byte slices become {"__data__": "..."} because JSON has no
// binary type, and everything else maps directly.
func expectedJSON(v any) string {
	b, err := json.Marshal(forJSON(v))
	if err != nil {
		panic(err)
	}
	return string(b)
}

func forJSON(v any) any {
	switch t := v.(type) {
	case []byte:
		return map[string]any{"__data__": string(t)}
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = forJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = forJSON(val)
		}
		return out
	}
	return v
}
