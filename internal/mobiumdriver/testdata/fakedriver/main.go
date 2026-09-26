// A driver used to test the external driver protocol from the other side.
//
// It is deliberately written the way a third party would write one: it imports
// nothing from mobium, it knows only what docs/decisions/0003 says, and it
// speaks the protocol by hand. If this file needs anything from the mobium
// module to work, the extension point is not actually open.
//
// Behavior is steered by MOBIUM_FAKE_MODE so one binary can play every part a
// misbehaving driver needs to play in a test.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type request struct {
	ID     int                    `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

// calls records what mobium asked for, so a test can assert on the far side of
// the pipe rather than only on the near side.
var calls []string

func main() {
	mode := os.Getenv("MOBIUM_FAKE_MODE")
	if mode == "exit-at-once" {
		fmt.Fprintln(os.Stderr, "cannot reach the device: no such thing")
		os.Exit(3)
	}
	if mode == "silent" {
		select {} // never answers anything
	}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<24)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for in.Scan() {
		var req request
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			continue
		}
		calls = append(calls, req.Method)

		if mode == "chatty" {
			// The commonest mistake: debug output on stdout. Mobium must skip
			// it and still find the reply.
			fmt.Fprintln(out, "DEBUG handling "+req.Method)
			fmt.Fprintln(out, `{"jsonrpc":"2.0","id":9999,"result":{}}`)
		}

		result, rpcErr := handle(mode, req)
		var reply string
		if rpcErr != "" {
			b, _ := json.Marshal(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]interface{}{"code": -32000, "message": rpcErr},
			})
			reply = string(b)
		} else {
			b, _ := json.Marshal(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID, "result": result,
			})
			reply = string(b)
		}
		fmt.Fprintln(out, reply)
		out.Flush()

		if req.Method == "shutdown" {
			return
		}
	}
}

func handle(mode string, req request) (interface{}, string) {
	switch req.Method {
	case "initialize":
		version := "1"
		if mode == "wrong-version" {
			version = "2"
		}
		caps := []string{"gestures", "text", "apps", "health"}
		if mode == "minimal" {
			caps = nil
		}
		if mode == "unknown-cap" {
			caps = append(caps, "teleportation")
		}
		return map[string]interface{}{
			"protocolVersion": version,
			"name":            "fake/" + orDefault(mode, "plain"),
			"capabilities":    caps,
		}, ""

	case "snapshot":
		if mode == "broken-snapshot" {
			return "not a node at all", ""
		}
		return screen(), ""

	case "screenshot":
		if mode == "bad-png" {
			// A driver reporting an error where the image goes. Base64 of
			// "sorry"; the magic-number check has to catch it.
			return map[string]interface{}{"png": "c29ycnk="}, ""
		}
		return map[string]interface{}{"png": onePixelPNG}, ""

	case "tap", "swipe", "longPress", "setText", "clear",
		"launch", "terminate", "install", "openUrl", "shutdown":
		record(req)
		return map[string]interface{}{}, ""

	case "health":
		return map[string]interface{}{"healthy": mode != "unhealthy"}, ""

	case "uninstall":
		// Something no capability was advertised for. Mobium should never send
		// it; if it does, the test wants to know.
		return nil, "uninstall was called even though inventory was not advertised"
	}
	return nil, "this driver does not implement " + req.Method
}

// record appends the call and its arguments to a file, so a test can read back
// exactly what crossed the pipe.
func record(req request) {
	path := os.Getenv("MOBIUM_FAKE_LOG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	args, _ := json.Marshal(req.Params)
	fmt.Fprintf(f, "%s %s\n", req.Method, args)
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// screen is a small hierarchy in the shape docs/decisions/0003 specifies:
// what the platform said, and nothing derived.
func screen() map[string]interface{} {
	return map[string]interface{}{
		"class":  "Screen",
		"bounds": []int{0, 0, 400, 800},
		"children": []map[string]interface{}{
			{
				"class":  "List",
				"bounds": []int{0, 0, 400, 400},
				"children": []map[string]interface{}{
					{"class": "Row", "text": "first", "bounds": []int{0, 0, 400, 100}, "clickable": true},
					{"class": "Row", "text": "second", "bounds": []int{0, 100, 400, 200}, "clickable": true},
				},
			},
			{
				"class": "Button", "label": "Go", "testid": "go",
				"bounds": []int{0, 700, 400, 800}, "clickable": true,
			},
		},
	}
}

// A 1x1 transparent PNG.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="
