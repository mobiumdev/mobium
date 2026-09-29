package trace

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// A trace is what the viewers read: context-options first, a before/after
// per call with the same id, the input point of an action, a failure as an
// error, and after each call a screencast frame and a frame snapshot the
// after event links to, with the frame's image in resources/.
func TestATraceIsWhatTheViewersRead(t *testing.T) {
	r := New(Options{Name: "login", Device: "emulator-5554", Platform: "android", Version: "test"})
	id := r.Before("app_tap", "tap @e3", map[string]interface{}{"target": "@e3"})
	r.Input(id, 540, 1200, &Box{X1: 440, Y1: 1150, X2: 640, Y2: 1250})
	r.After(id, nil, "tapped @e3")
	r.Frame(id, []byte("\xff\xd8jpeg"), 1080, 2400, []Box{{Ref: "@e1", Label: "Sign in", Role: "button", X1: 0, Y1: 0, X2: 540, Y2: 120}})
	bad := r.Before("app_tap", "tap @e9", nil)
	r.After(bad, errors.New("no element matches @e9"), "")

	raw, err := r.Zip()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = b
	}
	if _, ok := files["trace.network"]; !ok {
		t.Error("no trace.network")
	}
	var events []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(string(files["trace.trace"])), "\n") {
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("a line that is not JSON: %q", line)
		}
		events = append(events, ev)
	}
	if events[0]["type"] != "context-options" || events[0]["title"] != "login" {
		t.Errorf("first event: %v", events[0])
	}
	byType := map[string][]map[string]interface{}{}
	for _, ev := range events {
		byType[ev["type"].(string)] = append(byType[ev["type"].(string)], ev)
	}
	if len(byType["before"]) != 2 || len(byType["after"]) != 2 || len(byType["input"]) != 1 {
		t.Fatalf("events: %v", byType)
	}
	after := byType["after"][0]
	if after["afterSnapshot"] != "after@"+id {
		t.Errorf("the after event does not link its snapshot: %v", after)
	}
	if e, ok := byType["after"][1]["error"].(map[string]interface{}); !ok || e["message"] != "no element matches @e9" {
		t.Errorf("a failure: %v", byType["after"][1])
	}
	frame := byType["screencast-frame"][0]
	if _, ok := files["resources/"+frame["sha1"].(string)]; !ok {
		t.Errorf("the frame's image %v is not in resources/", frame["sha1"])
	}
	snap, _ := json.Marshal(byType["frame-snapshot"][0])
	if !strings.Contains(string(snap), `"title":"@e1 Sign in (button)"`) || !strings.Contains(string(snap), "data:image/jpeg;base64,") {
		t.Errorf("the snapshot lacks the map or the image: %s", snap)
	}
}
