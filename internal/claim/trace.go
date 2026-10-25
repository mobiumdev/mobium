// Package claim checks a claim about a recorded session: a model with a fresh
// context reads a Mobium trace through read-only tools and returns passed,
// failed or inconclusive, with the observations it rests on.
//
// Mutatis mutandis Vibium's Check (clicker/internal/check): the same
// verifier, instructions and verdict, with the page swapped for a screen. A
// trace here holds each call with its arguments, result or error and the
// point it touched, and after each call the screenshot and the map's
// elements — so the screen is evidence as text, and as an image when the
// text is not enough.
package claim

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Limits on what a trace may hold. A recording is read in place: nothing is
// extracted, run or fetched.
const (
	maxTraceBytes  = 128 << 20
	maxEntries     = 20000
	maxEvents      = 100000
	maxImageBytes  = 4 << 20
	maxResultChars = 4000
)

// Call is one tool call as the trace kept it.
type Call struct {
	ID     string                 `json:"id"`
	Tool   string                 `json:"tool"`
	Title  string                 `json:"title,omitempty"`
	Params map[string]interface{} `json:"params,omitempty"`
	Start  float64                `json:"start_ms"`
	End    float64                `json:"end_ms,omitempty"`
	Result string                 `json:"result,omitempty"`
	Error  string                 `json:"error,omitempty"`
	// Touched is where an action touched, in screenshot pixels.
	Touched *Point `json:"touched,omitempty"`
	// Screen is the map's elements after the call, from its frame snapshot.
	Screen []Element `json:"-"`
	// Image is the resource holding the screenshot after the call.
	Image string `json:"-"`
}

// Point is a touch.
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Element is one element of a map, as a frame snapshot titled its box.
type Element struct {
	Title string `json:"element"`
	Box   string `json:"box"`
}

// Trace is a Mobium recording, opened read-only.
type Trace struct {
	zip   *zip.ReadCloser
	files map[string]*zip.File
	Title string
	Calls []*Call
	byID  map[string]*Call
}

// Open reads a trace zip. Anything that is not a plain archive of a Mobium
// (or Vibium-format) trace is refused before a model sees it.
func Open(ctx context.Context, filename string) (*Trace, error) {
	info, err := os.Stat(filename)
	if err != nil || !info.Mode().IsRegular() {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not a readable file: a recording is the .zip `mobium trace stop` saves", filename)
	}
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not a zip: a recording is the .zip `mobium trace stop` saves", filename)
	}
	t := &Trace{zip: z, files: map[string]*zip.File{}, byID: map[string]*Call{}}
	ok := false
	defer func() {
		if !ok {
			z.Close()
		}
	}()
	if len(z.File) > maxEntries {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the recording holds %d entries, more than %d", len(z.File), maxEntries)
	}
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if path.Clean(f.Name) != f.Name || strings.HasPrefix(f.Name, "/") || strings.HasPrefix(f.Name, "../") ||
			strings.Contains(f.Name, "\\") || f.Mode()&os.ModeSymlink != 0 {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the recording has an unsafe entry, %q", f.Name)
		}
		if t.files[f.Name] != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the recording has %q twice", f.Name)
		}
		t.files[f.Name] = f
	}
	f := t.files["trace.trace"]
	if f == nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the recording has no trace.trace: it is not a trace")
	}
	if f.UncompressedSize64 > maxTraceBytes {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the recording's events exceed %d MiB", maxTraceBytes>>20)
	}
	if err := t.read(ctx, f); err != nil {
		return nil, err
	}
	ok = true
	return t, nil
}

// Close releases the archive.
func (t *Trace) Close() error { return t.zip.Close() }

func (t *Trace) read(ctx context.Context, f *zip.File) error {
	r, err := f.Open()
	if err != nil {
		return mobiumerr.New(mobiumerr.InvalidArgument, "cannot read trace.trace: %w", err)
	}
	defer r.Close()
	sc := bufio.NewScanner(io.LimitReader(r, maxTraceBytes+1))
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	n, versioned := 0, false
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if n++; n > maxEvents {
			return mobiumerr.New(mobiumerr.InvalidArgument, "the recording holds more than %d events", maxEvents)
		}
		var e map[string]interface{}
		if json.Unmarshal([]byte(line), &e) != nil || e == nil {
			return mobiumerr.New(mobiumerr.InvalidArgument, "trace.trace line %d is not a JSON event", n)
		}
		switch str(e, "type") {
		case "context-options":
			if v, _ := e["version"].(float64); v != 8 {
				return mobiumerr.New(mobiumerr.InvalidArgument, "the recording is trace version %v; this reads version 8", e["version"])
			}
			versioned = true
			t.Title = str(e, "title")
		case "before":
			c := &Call{ID: str(e, "callId"), Tool: strings.TrimPrefix(str(e, "method"), "mobium:"), Title: str(e, "title")}
			c.Start, _ = e["startTime"].(float64)
			c.Params, _ = e["params"].(map[string]interface{})
			if c.ID != "" && t.byID[c.ID] == nil {
				t.byID[c.ID] = c
				t.Calls = append(t.Calls, c)
			}
		case "input":
			if c := t.byID[str(e, "callId")]; c != nil {
				if p, ok := e["point"].(map[string]interface{}); ok {
					x, _ := p["x"].(float64)
					y, _ := p["y"].(float64)
					c.Touched = &Point{int(x), int(y)}
				}
			}
		case "after":
			if c := t.byID[str(e, "callId")]; c != nil {
				c.End, _ = e["endTime"].(float64)
				c.Result = clip(str(e, "result"), maxResultChars)
				if er, ok := e["error"].(map[string]interface{}); ok {
					c.Error = clip(str(er, "message"), maxResultChars)
				}
			}
		case "frame-snapshot":
			s, _ := e["snapshot"].(map[string]interface{})
			if c := t.byID[str(s, "callId")]; c != nil {
				c.Screen = elements(s["html"])
				if ro, ok := s["resourceOverrides"].([]interface{}); ok && len(ro) > 0 {
					if m, ok := ro[0].(map[string]interface{}); ok {
						c.Image = str(m, "sha1")
					}
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return mobiumerr.New(mobiumerr.InvalidArgument, "cannot read trace.trace: %w", err)
	}
	if !versioned {
		return mobiumerr.New(mobiumerr.InvalidArgument, "trace.trace has no context-options event: it is not a trace")
	}
	return nil
}

// elements reads a frame snapshot's boxes: each DIV with a title is one
// element of the map, titled with its ref, label and role.
func elements(html interface{}) []Element {
	var out []Element
	var walk func(interface{})
	walk = func(n interface{}) {
		list, ok := n.([]interface{})
		if !ok || len(list) < 2 {
			return
		}
		if tag, _ := list[0].(string); tag == "DIV" {
			if attrs, ok := list[1].(map[string]interface{}); ok {
				if title := str(attrs, "title"); title != "" {
					out = append(out, Element{Title: title, Box: boxOf(str(attrs, "style"))})
				}
			}
		}
		for _, child := range list[2:] {
			walk(child)
		}
	}
	walk(html)
	return out
}

// boxOf is where a box sits, as fractions of the screen: "left 12.3% top
// 40.0% width 76.0% height 6.5%".
func boxOf(style string) string {
	var parts []string
	for _, k := range []string{"left", "top", "width", "height"} {
		if i := strings.Index(style, k+":"); i >= 0 {
			v := style[i+len(k)+1:]
			if j := strings.Index(v, ";"); j >= 0 {
				v = v[:j]
			}
			parts = append(parts, k+" "+v)
		}
	}
	return strings.Join(parts, " ")
}

// Image is the screenshot after call id, or nil.
func (t *Trace) Image(id string) ([]byte, error) {
	c := t.byID[id]
	if c == nil || c.Image == "" {
		return nil, nil
	}
	f := t.files["resources/"+c.Image]
	if f == nil {
		return nil, nil
	}
	if f.UncompressedSize64 > maxImageBytes {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "the screenshot after %s is over %d MiB", id, maxImageBytes>>20)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, maxImageBytes))
}

// Call is the call of id, or nil.
func (t *Trace) Call(id string) *Call { return t.byID[id] }

// Tools is how often each tool was called, most first.
func (t *Trace) Tools() []string {
	count := map[string]int{}
	for _, c := range t.Calls {
		count[c.Tool]++
	}
	names := make([]string, 0, len(count))
	for n := range count {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if count[names[i]] != count[names[j]] {
			return count[names[i]] > count[names[j]]
		}
		return names[i] < names[j]
	})
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = fmt.Sprintf("%s ×%d", n, count[n])
	}
	return out
}

func str(m map[string]interface{}, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
