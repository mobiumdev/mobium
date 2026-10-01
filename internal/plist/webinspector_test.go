package plist

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestDrivesWebinspectord is the only test here whose opinion is final.
//
// Round-tripping through ourselves proves self-consistency, and agreeing with
// Python's plistlib proves we match another reading of the specification. But
// the program this codec exists to talk to is Apple's `webinspectord`, and it
// is the one that decides whether these bytes are a property list. It answers
// unhelpfully and only on a booted simulator, so this test skips unless one is
// there — and says exactly what it needs, because a skip nobody understands is
// a test nobody runs.
//
//	xcrun simctl boot <udid>
//	xcrun simctl openurl <udid> https://example.com
//	go test ./internal/plist/ -run Webinspectord -v
func TestDrivesWebinspectord(t *testing.T) {
	socket := findInspectorSocket(t)

	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", socket, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}

	const cid = "mobium-plist-test"
	send := func(selector string, arg map[string]any) {
		t.Helper()
		body, err := Marshal(map[string]any{"__selector": selector, "__argument": arg})
		if err != nil {
			t.Fatalf("marshal %s: %v", selector, err)
		}
		head := make([]byte, 4)
		binary.BigEndian.PutUint32(head, uint32(len(body)))
		if _, err := conn.Write(append(head, body...)); err != nil {
			t.Fatalf("write %s: %v", selector, err)
		}
	}
	recv := func() (string, map[string]any) {
		t.Helper()
		head := make([]byte, 4)
		if _, err := io.ReadFull(conn, head); err != nil {
			t.Fatalf("read: %v", err)
		}
		body := make([]byte, binary.BigEndian.Uint32(head))
		if _, err := io.ReadFull(conn, body); err != nil {
			t.Fatalf("read body: %v", err)
		}
		v, err := Unmarshal(body)
		if err != nil {
			t.Fatalf("our decoder could not read what webinspectord sent: %v", err)
		}
		m, _ := v.(map[string]any)
		sel, _ := m["__selector"].(string)
		arg, _ := m["__argument"].(map[string]any)
		return sel, arg
	}

	send("_rpc_reportIdentifier:", map[string]any{"WIRConnectionIdentifierKey": cid})

	var app, page any
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		selector, arg := recv()
		switch selector {
		case "_rpc_reportConnectedApplicationList:":
			if app != nil {
				continue
			}
			apps, _ := arg["WIRApplicationDictionaryKey"].(map[string]any)
			for key, v := range apps {
				info, _ := v.(map[string]any)
				if info["WIRApplicationBundleIdentifierKey"] == "com.apple.mobilesafari" {
					app = key
					send("_rpc_forwardGetListing:", map[string]any{
						"WIRConnectionIdentifierKey": cid, "WIRApplicationIdentifierKey": key})
				}
			}
			if app == nil {
				t.Skip("Safari is not running on the simulator; open a page first")
			}

		case "_rpc_applicationSentListing:":
			// The WebContent process answers with an empty listing, so an
			// empty one means keep waiting rather than no pages.
			listing, _ := arg["WIRListingKey"].(map[string]any)
			if page != nil || len(listing) == 0 {
				continue
			}
			for _, v := range listing {
				entry, _ := v.(map[string]any)
				page = entry["WIRPageIdentifierKey"]
				t.Logf("page: %v -> %v", entry["WIRTitleKey"], entry["WIRURLKey"])
				send("_rpc_forwardSocketSetup:", map[string]any{
					"WIRConnectionIdentifierKey": cid, "WIRApplicationIdentifierKey": app,
					"WIRPageIdentifierKey": page, "WIRSenderKey": "mobium-sender",
					"WIRAutomaticallyPause": false})
				break
			}

		case "_rpc_applicationSentData:":
			raw, _ := arg["WIRMessageDataKey"].([]byte)
			if raw == nil {
				continue
			}
			var m map[string]any
			if json.Unmarshal(raw, &m) != nil {
				continue
			}
			switch m["method"] {
			case "Target.targetCreated":
				// WebKit is multi-target: an unwrapped Runtime.evaluate
				// answers "'Runtime' domain was not found", which reads like
				// something quite different.
				params, _ := m["params"].(map[string]any)
				info, _ := params["targetInfo"].(map[string]any)
				inner, _ := json.Marshal(map[string]any{"id": 10, "method": "Runtime.evaluate",
					"params": map[string]any{"expression": "1 + 1"}})
				wrapped, _ := json.Marshal(map[string]any{"id": 2, "method": "Target.sendMessageToTarget",
					"params": map[string]any{"targetId": info["targetId"], "message": string(inner)}})
				send("_rpc_forwardSocketData:", map[string]any{
					"WIRConnectionIdentifierKey": cid, "WIRApplicationIdentifierKey": app,
					"WIRPageIdentifierKey": page, "WIRSenderKey": "mobium-sender",
					"WIRSocketDataKey": wrapped})

			case "Target.dispatchMessageFromTarget":
				params, _ := m["params"].(map[string]any)
				text, _ := params["message"].(string)
				var reply map[string]any
				if json.Unmarshal([]byte(text), &reply) != nil {
					continue
				}
				if id, _ := reply["id"].(float64); id != 10 {
					continue
				}
				result, _ := reply["result"].(map[string]any)
				inner, _ := result["result"].(map[string]any)
				if got := inner["value"]; got != float64(2) {
					t.Fatalf("1 + 1 came back as %v (%T); full reply %v", got, got, reply)
				}
				t.Log("webinspectord accepted our plists and evaluated 1 + 1 = 2")
				return
			}
		}
	}
	t.Fatal("no answer from webinspectord within the deadline")
}

var rwiSocket = regexp.MustCompile(`/private/var/tmp/\S*webinspectord_sim\.socket`)

// findInspectorSocket asks the booted simulator's launchd where its Remote Web
// Inspector socket is. The path changes every boot, so it is discovered.
func findInspectorSocket(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no xcrun, so no simulator")
	}
	booted, err := exec.Command("xcrun", "simctl", "list", "devices", "booted").Output()
	if err != nil || !strings.Contains(string(booted), "Booted") {
		t.Skip("no booted simulator: `xcrun simctl boot <udid>` then open a page in Safari")
	}
	out, err := exec.Command("xcrun", "simctl", "spawn", "booted",
		"launchctl", "print", "system/com.apple.webinspectord").Output()
	if err != nil {
		t.Skipf("could not ask the simulator for its inspector socket: %v", err)
	}
	match := rwiSocket.FindString(string(out))
	if match == "" {
		t.Skip("the simulator published no RWI_LISTEN_SOCKET")
	}
	return match
}
