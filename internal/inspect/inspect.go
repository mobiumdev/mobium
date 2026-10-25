// Package inspect is `mobium inspect`: a page on this machine showing a
// device's screen with every element map finds drawn over it, the locator
// for the one clicked, actions on it, and what was done kept as a test file.
// See docs/guides/inspector.md.
//
// It is a client of the tools, as the test runner is: every answer on the
// page is a tool's answer, and every action is the tool a command would call,
// through the same daemon, so a terminal and the page drive one session.
package inspect

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/codegen"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/testrun"
	"github.com/mobiumdev/mobium/internal/uitree"
)

//go:embed page.html
var page []byte

// Caller calls one tool, as the CLI's own commands do.
type Caller func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error)

// actions are the tools the page may ask for, and nothing else: it cannot
// uninstall an app or run a script in a page by naming the tool.
var actions = map[string]bool{
	"app_tap": true, "app_type": true, "app_fill": true, "app_long_press": true, "app_set": true,
	"app_swipe": true, "app_press": true, "app_scroll_to": true, "app_wait_for": true,
	"app_launch": true, "app_context": true, "app_keyboard": true,
}

// Server is one inspector: its caller, its token, and the steps recorded.
type Server struct {
	call  Caller
	Token string
	// addr is where it listens, which a request's Host must name.
	addr string

	// Out is where the recording is written as it grows, after every step:
	// a *.test.json, or a program in a client's language by its extension.
	// Empty writes nothing; the page's download is always there.
	Out string

	mu    sync.Mutex
	app   string
	steps []map[string]interface{}
	// last is what the page's last map found, which an element with no
	// locator of its own is scoped against before it is recorded.
	last []agent.ElementView
}

// New makes a server for a caller, with a token only its printed URL holds.
func New(call Caller) (*Server, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Server{call: call, Token: hex.EncodeToString(b)}, nil
}

// Listen opens 127.0.0.1:port — any free one for 0 — and returns the address.
func (s *Server) Listen(port string) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, err
	}
	s.addr = ln.Addr().String()
	return ln, nil
}

// URL is the page's address, token included.
func (s *Server) URL() string { return "http://" + s.addr + "/" + s.Token + "/" }

// Handler serves the page and its calls, each refused unless it carries the
// token in its path and names this server as its Host. 127.0.0.1 alone is
// not enough for a page that taps a device: any web page open in the same
// browser can send a request there, and a DNS-rebinding one can make it look
// same-origin. The token is in the URL the command printed and nowhere else.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	p := "/" + s.Token + "/"
	mux.HandleFunc(p, s.index)
	mux.HandleFunc(p+"state", s.state)
	mux.HandleFunc(p+"find", s.find)
	mux.HandleFunc(p+"act", s.act)
	mux.HandleFunc(p+"test", s.test)
	mux.HandleFunc(p+"clear", s.clear)
	mux.HandleFunc(p+"fresh", s.fresh)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.addr != "" && r.Host != s.addr {
			http.Error(w, "wrong host", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(r.URL.Path, p) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/"+s.Token+"/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'self'")
	_, _ = w.Write(page)
}

// Element is a map entry with the locator spelled as a person writes it.
type Element struct {
	agent.ElementView
	Target string `json:"target"`
}

// State is what the page draws: the screen, and what map found on it.
type State struct {
	Screenshot string    `json:"screenshot"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	App        string    `json:"app"`
	Context    string    `json:"context,omitempty"`
	Elements   []Element `json:"elements"`
	Steps      int       `json:"steps"`
	Error      string    `json:"error,omitempty"`
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	var st State
	shot, err := s.call("app_screenshot", map[string]interface{}{})
	if err != nil {
		reply(w, State{Error: err.Error()})
		return
	}
	for _, c := range shot.Content {
		if c.Type == "image" {
			st.Screenshot = c.Data
			if raw, err := base64.StdEncoding.DecodeString(c.Data); err == nil {
				if cfg, err := png.DecodeConfig(bytes.NewReader(raw)); err == nil {
					st.Width, st.Height = cfg.Width, cfg.Height
				}
			}
		}
	}
	if res, err := s.call("app_current", map[string]interface{}{}); err == nil && len(res.Content) > 0 {
		st.App = strings.TrimSpace(res.Content[0].Text)
	}
	if res, err := s.call("app_contexts", map[string]interface{}{}); err == nil {
		var v struct {
			Current string `json:"current"`
		}
		decode(res.StructuredContent, &v)
		if v.Current != "" && v.Current != "NATIVE_APP" {
			st.Context = v.Current
		}
	}
	res, err := s.call("app_map", map[string]interface{}{})
	if err != nil {
		st.Error = err.Error()
	} else {
		var v struct {
			Elements []agent.ElementView `json:"elements"`
		}
		decode(res.StructuredContent, &v)
		for _, e := range v.Elements {
			st.Elements = append(st.Elements, Element{ElementView: e, Target: target(e)})
		}
	}
	s.mu.Lock()
	if s.app == "" && st.Context == "" {
		s.app = st.App
	}
	s.last = nil
	for _, e := range st.Elements {
		s.last = append(s.last, e.ElementView)
	}
	st.Steps = len(s.steps)
	s.mu.Unlock()
	reply(w, st)
}

// target is the locator a test would name an element by: the one map
// derived for it, which survives the next screen, where its ref does not.
func target(e agent.ElementView) string {
	if e.Locator == nil {
		return e.Ref
	}
	// Exact carried over: map derives every locator as the whole value and
	// checks it names one element so, and without it the recording wrote
	// text=Welcome, which "Welcome back" meets too (CHALLENGES 233).
	return uitree.Locator{Kind: uitree.Kind(e.Locator.Kind), Value: e.Locator.Value, Exact: e.Locator.Exact,
		Role: e.Locator.Role}.String()
}

// Found is what a locator typed on the page resolves to.
type Found struct {
	Locator string              `json:"locator"`
	Matches []agent.ElementView `json:"matches"`
	Error   string              `json:"error,omitempty"`
}

func (s *Server) find(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Locator string `json:"locator"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	out := Found{Locator: in.Locator}
	res, err := s.call("app_find", map[string]interface{}{"locator": in.Locator})
	if err != nil {
		out.Error = err.Error()
	} else {
		var v struct {
			Elements []agent.ElementView `json:"elements"`
		}
		decode(res.StructuredContent, &v)
		out.Matches = v.Elements
	}
	reply(w, out)
}

// Acted is an action's answer: what the tool said, or why it refused.
type Acted struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
	Steps int    `json:"steps"`
}

func (s *Server) act(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Tool      string                 `json:"tool"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !actions[in.Tool] {
		http.Error(w, in.Tool+" is not an action the inspector takes", http.StatusBadRequest)
		return
	}
	if in.Arguments == nil {
		in.Arguments = map[string]interface{}{}
	}
	// Recorded as asked, before the call can add a device or a driver to
	// the arguments: a test names neither. A ref lasts one map, and a path
	// moves with every row a list adds or reuses, so an element map named by
	// either is recorded by a locator scoped to it, found before the action
	// moves the screen.
	recorded := copyArgs(in.Arguments)
	scoped := ""
	if t, _ := recorded["target"].(string); strings.HasPrefix(t, "@") || strings.HasPrefix(t, "path=") {
		if loc := s.scope(t); loc != "" {
			recorded["target"], scoped = loc, loc
		}
	}
	step := testrun.Shorthand(in.Tool, recorded)
	res, err := s.call(in.Tool, in.Arguments)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		reply(w, Acted{Error: err.Error(), Code: string(mobiumerr.CodeOf(err)), Steps: len(s.steps)})
		return
	}
	// Every action is a step — a context switch too: a step after it runs
	// in the page it switched to.
	s.steps = append(s.steps, step)
	out := Acted{Steps: len(s.steps)}
	if len(res.Content) > 0 {
		out.Text = res.Content[0].Text
	}
	if scoped != "" {
		out.Text += " — recorded as " + scoped
	}
	if err := s.write(); err != nil {
		out.Error = err.Error()
	}
	reply(w, out)
}

// scope finds a locator that names the element a ref or a path points at
// and nothing else, for an element map could name no better — one of
// several look-alikes, every row's "More". As a person writing the test
// would: its name within the nearest element that has a locator of its own,
// or within a text on the screen — a row's title is no target, so map does
// not list it — then its name and its place among the look-alikes. Each is
// checked with app_find, by the rectangle of the match; "" when none names
// it alone, and it is recorded as map named it.
func (s *Server) scope(t string) string {
	s.mu.Lock()
	last := append([]agent.ElementView{}, s.last...)
	s.mu.Unlock()
	var el *agent.ElementView
	for i := range last {
		if last[i].Ref == t || target(last[i]) == t {
			el = &last[i]
		}
	}
	if el == nil || el.Label == "" || el.Context != "" {
		return ""
	}
	names := s.lookalikes(*el)
	if len(names) == 0 {
		return ""
	}
	base := names[0]
	// within the nearest anchors first: a row's own title is closer than a
	// heading above the list.
	anchors := []agent.ElementView{}
	for _, a := range last {
		if a.Ref != el.Ref && a.Locator != nil && a.Locator.Kind != "path" {
			anchors = append(anchors, a)
		}
	}
	cx, cy := center(el.Bounds)
	sort.SliceStable(anchors, func(i, j int) bool {
		return distance(anchors[i].Bounds, cx, cy) < distance(anchors[j].Bounds, cx, cy)
	})
	for i, a := range anchors {
		if i == maxAnchors {
			break
		}
		if loc := base + " within " + target(a); s.namesOnly(loc, el.Bounds) {
			return loc
		}
	}
	for i, line := range s.lines(el.Label) {
		if i == maxLines {
			break
		}
		anchor := uitree.Locator{Kind: "text", Value: line, Exact: true}.String()
		if loc := base + " within " + anchor; s.namesOnly(loc, el.Bounds) {
			return loc
		}
	}
	for _, name := range names {
		matches := s.matches(name)
		for i, m := range matches {
			if m.Bounds == el.Bounds {
				if loc := name + ",nth=" + strconv.Itoa(i+1); s.namesOnly(loc, el.Bounds) {
					return loc
				}
			}
		}
	}
	return ""
}

// maxAnchors and maxLines bound how many neighbors and texts scope tries
// within, each a read of the screen.
const (
	maxAnchors = 5
	maxLines   = 40
)

// lines are the screen's texts, once each, but the element's own.
func (s *Server) lines(own string) []string {
	res, err := s.call("app_text", map[string]interface{}{})
	if err != nil || len(res.Content) == 0 {
		return nil
	}
	seen := map[string]bool{own: true, "": true}
	var out []string
	for _, l := range strings.Split(res.Content[0].Text, "\n") {
		if l = strings.TrimSpace(l); !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

// lookalikes are the locators that name an element by what it says — its
// label, then its text, with its role — each matching it among others.
func (s *Server) lookalikes(el agent.ElementView) []string {
	var out []string
	for _, kind := range []uitree.Kind{"label", "text"} {
		loc := uitree.Locator{Kind: kind, Value: el.Label, Exact: true, Role: el.Role}.String()
		for _, m := range s.matches(loc) {
			if m.Bounds == el.Bounds {
				out = append(out, loc)
				break
			}
		}
	}
	return out
}

// matches is app_find's answer for a locator, none when it fails.
func (s *Server) matches(loc string) []agent.ElementView {
	res, err := s.call("app_find", map[string]interface{}{"locator": loc})
	if err != nil {
		return nil
	}
	var v struct {
		Elements []agent.ElementView `json:"elements"`
	}
	decode(res.StructuredContent, &v)
	return v.Elements
}

// namesOnly reports whether loc matches one element, the one at bounds.
func (s *Server) namesOnly(loc string, bounds agent.BoundsView) bool {
	m := s.matches(loc)
	return len(m) == 1 && m[0].Bounds == bounds
}

func center(b agent.BoundsView) (int, int) { return (b.X1 + b.X2) / 2, (b.Y1 + b.Y2) / 2 }

func distance(b agent.BoundsView, x, y int) int {
	bx, by := center(b)
	dx, dy := bx-x, by-y
	return dx*dx + dy*dy
}

// write saves the recording to Out, when it is set, as the page's download
// would give it.
func (s *Server) write() error {
	if s.Out == "" {
		return nil
	}
	body, err := s.recording(languageOf(s.Out))
	if err != nil {
		return mobiumerr.New(mobiumerr.Unsupported, "%s was not written: %w", s.Out, err)
	}
	if dir := filepath.Dir(s.Out); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return mobiumerr.New(mobiumerr.Unsupported, "%s was not written: %w", s.Out, err)
		}
	}
	if err := os.WriteFile(s.Out, body, 0o644); err != nil {
		return mobiumerr.New(mobiumerr.Unsupported, "%s was not written: %w", s.Out, err)
	}
	return nil
}

// languageOf is the client language a path's extension names, "" for a
// test file.
func languageOf(path string) string {
	for lang, ext := range codegen.Extension {
		if strings.HasSuffix(path, ext) {
			return lang
		}
	}
	return ""
}

// recording is what was done, as a test file for "" and as a program in a
// client's language otherwise. Called with s.mu held.
func (s *Server) recording(lang string) ([]byte, error) {
	if lang != "" {
		raw, _ := json.Marshal(s.steps)
		var steps []testrun.Step
		if err := json.Unmarshal(raw, &steps); err != nil {
			return nil, err
		}
		src, err := codegen.Program(lang, s.app, steps)
		return []byte(src), err
	}
	file := map[string]interface{}{
		"description": "Recorded with mobium inspect.",
		"tests": []interface{}{map[string]interface{}{
			"name":  "recorded",
			"steps": append([]map[string]interface{}{}, s.steps...),
		}},
	}
	if s.app != "" {
		file["app"] = s.app
	}
	b, _ := json.MarshalIndent(file, "", "  ")
	return append(b, '\n'), nil
}

// test is what was done, as a test file `mobium test` runs, or with
// ?lang= as a program in that client's language.
func (s *Server) test(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lang := r.URL.Query().Get("lang")
	body, err := s.recording(lang)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	name, kind := "recorded.test.json", "application/json"
	if lang != "" {
		name, kind = "recorded"+codegen.Extension[lang], "text/plain; charset=utf-8"
		if lang == "java" {
			name = "Recorded.java" // the class it declares
		}
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Type", kind)
	_, _ = w.Write(body)
}

func (s *Server) clear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	s.steps, s.app = nil, ""
	err := s.write()
	s.mu.Unlock()
	reply(w, Acted{Error: errString(err)})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// fresh starts a recording where a test starts: the app in front stopped and
// launched again, as `mobium test` does before each test, and nothing
// recorded yet. Recording from wherever the screen happened to be wrote a
// test that could not replay — its first steps had been taken before.
func (s *Server) fresh(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if !readJSON(w, r, &in) {
		return
	}
	res, err := s.call("app_current", map[string]interface{}{})
	if err != nil || len(res.Content) == 0 {
		reply(w, Acted{Error: "cannot tell which app is in front: " + errText(err)})
		return
	}
	app := strings.TrimSpace(res.Content[0].Text)
	_, _ = s.call("app_terminate", map[string]interface{}{"app": app})
	if _, err := s.call("app_launch", map[string]interface{}{"app": app}); err != nil {
		reply(w, Acted{Error: err.Error(), Code: string(mobiumerr.CodeOf(err))})
		return
	}
	s.mu.Lock()
	s.steps, s.app = nil, app
	err = s.write()
	s.mu.Unlock()
	reply(w, Acted{Text: "started " + app + " fresh; recording from here", Error: errString(err)})
}

func errText(err error) string {
	if err == nil {
		return "no answer"
	}
	return err.Error()
}

func copyArgs(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// readJSON reads a POST's body, refusing anything else: a state-changing
// call is never a GET a link could make.
func readJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return false
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "application/json", http.StatusUnsupportedMediaType)
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func reply(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func decode(from interface{}, to interface{}) {
	b, err := json.Marshal(from)
	if err == nil {
		_ = json.Unmarshal(b, to)
	}
}
