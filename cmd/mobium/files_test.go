package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A path names a file on the caller's disk, so with the daemon on another
// machine every file argument has to travel as the file itself — found by a
// client over SSH whose screenshot, recording and install all named the
// daemon's disk. These hold each conversion, both ways.
func TestFilesTravelAsContent(t *testing.T) {
	dir := t.TempDir()

	// An .apk is a file, and goes as itself.
	apk := filepath.Join(dir, "app.apk")
	os.WriteFile(apk, []byte("apk bytes"), 0o644)
	args := map[string]interface{}{"path": apk}
	finish, err := sendFilesAsContent("app_install", args)
	if err != nil || args["path"] != nil || args["name"] != "app.apk" {
		t.Fatalf("install args %v, %v", args, err)
	}
	if raw, _ := base64.StdEncoding.DecodeString(args["content"].(string)); string(raw) != "apk bytes" {
		t.Errorf("the apk arrived as %q", raw)
	}
	r, _ := finish(agent.Result("installed /daemon/tmp/app.apk", agent.InstallView{Path: "/daemon/tmp/app.apk"}))
	if !strings.Contains(r.Content[0].Text, apk) {
		t.Errorf("the answer names %q, not the caller's path", r.Content[0].Text)
	}

	// A .app is a directory, and goes as an archive the daemon's own
	// extractor unpacks, links included — a bundle's frameworks have them.
	app := filepath.Join(dir, "Demo.app")
	os.MkdirAll(filepath.Join(app, "Frameworks"), 0o755)
	os.WriteFile(filepath.Join(app, "Demo"), []byte("binary"), 0o755)
	os.Symlink("Demo", filepath.Join(app, "Frameworks", "link"))
	args = map[string]interface{}{"path": app}
	if _, err := sendFilesAsContent("app_install", args); err != nil || args["name"] != "Demo.app.tar.gz" {
		t.Fatalf("a .app went as %v, %v", args["name"], err)
	}
	archive := filepath.Join(dir, "sent.tar.gz")
	raw, _ := base64.StdEncoding.DecodeString(args["content"].(string))
	os.WriteFile(archive, raw, 0o600)
	out := filepath.Join(dir, "unpacked")
	if err := device.UntarGz(archive, out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "Demo.app", "Demo")); string(b) != "binary" {
		t.Errorf("the bundle's file came out as %q", b)
	}
	if l, err := os.Readlink(filepath.Join(out, "Demo.app", "Frameworks", "link")); err != nil || l != "Demo" {
		t.Errorf("the bundle's link came out as %q, %v", l, err)
	}

	// A GPX file goes as its text.
	gpx := filepath.Join(dir, "r.gpx")
	os.WriteFile(gpx, []byte("<gpx/>"), 0o644)
	args = map[string]interface{}{"gpx": gpx}
	if _, err := sendFilesAsContent("app_location", args); err != nil || args["gpx"] != nil || args["gpx_data"] != "<gpx/>" {
		t.Errorf("gpx args %v, %v", args, err)
	}

	// A screenshot comes back as an image, saved where the caller said.
	shot := filepath.Join(dir, "out", "s.png")
	args = map[string]interface{}{"path": shot}
	finish, _ = sendFilesAsContent("app_screenshot", args)
	if args["path"] != nil {
		t.Error("the screenshot's path went to the daemon")
	}
	res := &agent.ToolsCallResult{Content: []agent.Content{{Type: "image", Data: base64.StdEncoding.EncodeToString([]byte("png")), MimeType: "image/png"}}}
	if r, err := finish(res); err != nil || !strings.Contains(r.Content[0].Text, shot) {
		t.Errorf("screenshot answer %v, %v", r, err)
	}
	if b, _ := os.ReadFile(shot); string(b) != "png" {
		t.Errorf("the screenshot was saved as %q", b)
	}

	// A recording comes back as data on stop, saved where the caller said;
	// a start is left alone.
	clip := filepath.Join(dir, "c.mp4")
	args = map[string]interface{}{"action": "stop", "path": clip}
	finish, _ = sendFilesAsContent("app_record", args)
	if args["path"] != nil || args["return_data"] != true {
		t.Errorf("record stop args %v", args)
	}
	back := agent.Result("recorded", agent.RecordView{Frames: 3, Data: base64.StdEncoding.EncodeToString([]byte("mp4"))})
	if r, err := finish(back); err != nil || !strings.Contains(r.Content[0].Text, clip) {
		t.Errorf("record answer %v, %v", r, err)
	}
	if b, _ := os.ReadFile(clip); string(b) != "mp4" {
		t.Errorf("the recording was saved as %q", b)
	}
	start := map[string]interface{}{"action": "start"}
	sendFilesAsContent("app_record", start)
	if start["return_data"] != nil {
		t.Error("a start was rewritten")
	}
}

// A batch's steps are calls too: each step's relative path is made the
// caller's, and with the daemon elsewhere each step's file travels as
// content and comes back saved where that step asked — while a step that
// asked for its image inline keeps it.
func TestBatchStepsFilesAreEachReadied(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MOBIUM_FILES", "content")

	gpx := filepath.Join(dir, "r.gpx")
	os.WriteFile(gpx, []byte("<gpx/>"), 0o644)
	route := map[string]interface{}{"gpx": "r.gpx"}
	saved := map[string]interface{}{"path": "shots/a.png"}
	args := map[string]interface{}{"steps": []interface{}{
		map[string]interface{}{"name": "app_location", "arguments": route},
		map[string]interface{}{"name": "app_screenshot", "arguments": saved},
		map[string]interface{}{"name": "app_screenshot"},
	}}
	finish, err := prepareFiles("app_batch", args)
	if err != nil {
		t.Fatal(err)
	}
	if route["gpx"] != nil || route["gpx_data"] != "<gpx/>" {
		t.Errorf("step 1 was not readied: %v", route)
	}
	if saved["path"] != nil {
		t.Errorf("step 2's path went to the daemon: %v", saved)
	}

	png := func(s string) agent.Content {
		return agent.Content{Type: "image", Data: base64.StdEncoding.EncodeToString([]byte(s)), MimeType: "image/png"}
	}
	back := agent.BatchResult(agent.BatchView{Steps: []agent.BatchStep{
		{Name: "app_location", Text: "following 2 points"},
		{Name: "app_screenshot", Image: true},
		{Name: "app_screenshot", Image: true},
	}}, []agent.Content{png("first"), png("second")})
	r, err := finish(back)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "shots", "a.png")
	if b, _ := os.ReadFile(want); string(b) != "first" {
		t.Errorf("step 2's screenshot was saved as %q", b)
	}
	if len(r.Content) != 2 || r.Content[1].Type != "image" {
		t.Fatalf("content = %+v, want the text and step 3's image", r.Content)
	}
	if b, _ := base64.StdEncoding.DecodeString(r.Content[1].Data); string(b) != "second" {
		t.Errorf("the image kept is %q, want step 3's", b)
	}
	text := r.Content[0].Text
	if !strings.Contains(text, "2. app_screenshot: ") || !strings.Contains(text, want) ||
		!strings.Contains(text, "3. app_screenshot: image 1 below") {
		t.Errorf("text = %q", text)
	}
}

func TestParseAudioExpect(t *testing.T) {
	got, err := parseAudioExpect("440:1.8-2.2, 880, 0:-0.5, 660:2-")
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"hz":440,"max_ms":2200,"min_ms":1800},{"hz":880},{"hz":0,"max_ms":500},{"hz":660,"min_ms":2000}]`
	if b, _ := json.Marshal(got); string(b) != want {
		t.Errorf("got %s", b)
	}
	if got, err := parseAudioExpect("silence"); err != nil || len(got) != 0 || got == nil {
		t.Errorf("silence: %v, %v", got, err)
	}
	for _, bad := range []string{"", "loud", "440:2", "440:a-b", "-5"} {
		if _, err := parseAudioExpect(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// A stop whose expectation failed sends the WAV back with the failure; the
// CLI saves it where it was asked and still reports the failure.
func TestSaveFailedAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "miss.wav")
	failed := agent.ErrorResult(mobiumerr.New(mobiumerr.NotConfirmed, "expected 880 Hz; heard 440 Hz").
		WithDetail("data", base64.StdEncoding.EncodeToString([]byte("RIFFwav"))))
	got, err := saveFailedAudio(&failed, path)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "RIFFwav" {
		t.Fatalf("saved %q, %v", b, err)
	}
	p := got.StructuredContent.(mobiumerr.Payload)
	if !got.IsError || p.Code != mobiumerr.NotConfirmed || p.Details["data"] != nil || p.Details["path"] != path ||
		!strings.Contains(got.Content[0].Text, "saved at "+path) {
		t.Errorf("got %+v", got)
	}
}
