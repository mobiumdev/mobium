// Package trace records a session as Vibium records one: a zip in the
// Playwright trace format, which trace.playwright.dev and player.vibium.dev
// both open. Every tool call is a before/after pair, an action on an element
// adds the point it touched, and after each call the screen is kept twice —
// as a screencast frame for the film strip, and as a frame snapshot, where
// Vibium keeps the page's DOM and Mobium keeps the screenshot with the map's
// elements drawn over it, each box titled with its ref and label.
//
// Mutatis mutandis: Vibium's format, with the page swapped for a screen. See
// ~/Projects/vibium/docs/reference/recording-format.md.
package trace

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Box is one element of a map, in screenshot pixels.
type Box struct {
	Ref, Label, Role string
	X1, Y1, X2, Y2   int
}

// Recorder is one trace in progress.
type Recorder struct {
	mu        sync.Mutex
	start     time.Time
	name      string
	pageID    string
	events    []map[string]interface{}
	resources map[string][]byte
	calls     int
	frames    int
}

// Options are what start asked for.
type Options struct {
	Name, Device, Platform, Version string
}

// New starts a trace. The first event is context-options, which both
// viewers require.
func New(o Options) *Recorder {
	now := time.Now()
	r := &Recorder{start: now, name: o.Name, resources: map[string][]byte{},
		pageID: fmt.Sprintf("page@%x", now.UnixNano())}
	title := o.Name
	if title == "" {
		title = "mobium " + o.Device
	}
	r.events = append(r.events, map[string]interface{}{
		"type": "context-options", "version": 8, "origin": "library",
		"libraryName": "mobium", "libraryVersion": o.Version,
		"browserName": o.Platform, "platform": runtime.GOOS, "sdkLanguage": "javascript",
		"wallTime": float64(now.UnixMilli()), "monotonicTime": float64(0),
		"title": title, "contextId": fmt.Sprintf("context@%x", now.UnixNano()),
		"options": map[string]interface{}{},
	})
	return r
}

func (r *Recorder) now() float64 { return float64(time.Since(r.start).Milliseconds()) }

// Before opens a call and returns its id.
func (r *Recorder) Before(tool, title string, params map[string]interface{}) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	id := fmt.Sprintf("call@%d", r.calls)
	if params == nil {
		params = map[string]interface{}{}
	}
	r.events = append(r.events, map[string]interface{}{
		"type": "before", "callId": id, "startTime": r.now(), "class": "Device",
		"method": "mobium:" + tool, "pageId": r.pageID, "params": params, "title": title,
	})
	return id
}

// Input records where an action touched: the point, and the element's box.
func (r *Recorder) Input(id string, x, y int, box *Box) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev := map[string]interface{}{"type": "input", "callId": id, "point": map[string]int{"x": x, "y": y}}
	if box != nil {
		ev["box"] = map[string]int{"x": box.X1, "y": box.Y1, "width": box.X2 - box.X1, "height": box.Y2 - box.Y1}
	}
	r.events = append(r.events, ev)
}

// After closes a call. A failure is recorded as Playwright records one, so
// the viewers mark the step red and show why.
func (r *Recorder) After(id string, err error, result string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev := map[string]interface{}{"type": "after", "callId": id, "endTime": r.now()}
	if err != nil {
		ev["error"] = map[string]interface{}{"message": err.Error()}
	} else if result != "" {
		ev["result"] = result
	}
	r.events = append(r.events, ev)
}

// Frame keeps the screen after call id: a screencast frame for the film
// strip, and a frame snapshot of the screenshot with the map drawn over it,
// linked from the call's after event. jpeg is the screenshot, w by h pixels.
func (r *Recorder) Frame(id string, jpeg []byte, w, h int, boxes []Box) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames++
	name := fmt.Sprintf("%s-%d-%d.jpeg", r.pageID, r.start.UnixMilli()+int64(r.now()), r.frames)
	r.resources[name] = jpeg
	t := r.now()
	r.events = append(r.events, map[string]interface{}{
		"type": "screencast-frame", "pageId": r.pageID, "sha1": name, "width": w, "height": h, "timestamp": t,
	})
	src := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpeg)
	snap := "after@" + id
	r.events = append(r.events, map[string]interface{}{
		"type": "frame-snapshot",
		"snapshot": map[string]interface{}{
			"callId": id, "snapshotName": snap, "pageId": r.pageID, "frameId": r.pageID,
			"frameUrl": "mobium://screen", "doctype": "html", "html": snapshotHTML(src, w, h, boxes),
			"viewport": map[string]int{"width": w, "height": h}, "timestamp": t, "wallTime": t,
			"resourceOverrides": []interface{}{map[string]interface{}{"url": src, "sha1": name}},
			"isMainFrame":       true,
		},
	})
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i]["type"] == "after" && r.events[i]["callId"] == id {
			r.events[i]["afterSnapshot"] = snap
			break
		}
	}
}

// snapshotHTML is the screen as the snapshot tab shows it: the screenshot,
// and each element of the map as a box over it, titled with its ref and
// label so hovering one says what `map` called it. Positions are fractions
// of the image, so the boxes stay on their elements at any size.
func snapshotHTML(src string, w, h int, boxes []Box) []interface{} {
	body := []interface{}{"BODY", map[string]interface{}{"style": "margin:0;overflow:hidden"}}
	stage := []interface{}{"DIV", map[string]interface{}{"style": "position:relative;width:100%"},
		[]interface{}{"IMG", map[string]interface{}{"src": src, "style": "width:100%;display:block"}}}
	if w > 0 && h > 0 {
		for _, b := range boxes {
			pct := func(v, of int) string { return fmt.Sprintf("%.3f%%", float64(v)*100/float64(of)) }
			title := strings.TrimSpace(b.Ref + " " + b.Label)
			if b.Role != "" {
				title += " (" + b.Role + ")"
			}
			stage = append(stage, []interface{}{"DIV", map[string]interface{}{
				"title": title,
				"style": fmt.Sprintf("position:absolute;left:%s;top:%s;width:%s;height:%s;"+
					"box-sizing:border-box;border:2px solid rgba(255,64,129,.8)",
					pct(b.X1, w), pct(b.Y1, h), pct(b.X2-b.X1, w), pct(b.Y2-b.Y1, h)),
			}})
		}
	}
	body = append(body, stage)
	return []interface{}{"HTML", map[string]interface{}{},
		[]interface{}{"HEAD", map[string]interface{}{}}, body}
}

// Calls is how many calls the trace holds.
func (r *Recorder) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// Zip is the trace as the viewers read it: trace.trace, an empty
// trace.network, and resources/ with the frames.
func (r *Recorder) Zip() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	now := time.Now()
	entry := func(name string) (interface{ Write([]byte) (int, error) }, error) {
		return zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
	}
	tw, err := entry("trace.trace")
	if err != nil {
		return nil, err
	}
	for _, ev := range r.events {
		b, err := json.Marshal(ev)
		if err != nil {
			return nil, err
		}
		if _, err := tw.Write(append(b, '\n')); err != nil {
			return nil, err
		}
	}
	if _, err := entry("trace.network"); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(r.resources))
	for n := range r.resources {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		// Stored, not deflated: a JPEG does not compress.
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "resources/" + n, Method: zip.Store, Modified: now})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(r.resources[n]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
