package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// speech is app_speech: what the screen reader says — about the element its
// cursor is on, or walking the screen in its own order, each sentence beside
// the element it was said about (ACCESSIBILITY.md, step 2).
//
// It answers what map cannot: the order a screen reader user meets things in,
// whether a heading or a status line is spoken, what a control's state and
// hint sound like, and everything spoken that no ref reaches.
//
// VoiceOver is read through WebDriverAgent, which moves its cursor and
// answers with the sentence; TalkBack is moved with a keyboard Android makes
// for it and read from its own log (device/talkback.go). Neither is turned on
// here: app_accessibility screen_reader on does that, and puts it back.
func (h *Handlers) speech(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(stringArg(args, "target"))
	if target != "" && s.web != nil {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "a screen reader's cursor is put on a native element; "+
			"switch to NATIVE_APP with app_context first, and the walk reads the page's content too")
	}
	if listen := stringArg(args, "listen"); listen != "" {
		if target != "" || boolArg(args, "walk") {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "listen is on its own: start it, act, and stop it — "+
				"not with a target or a walk")
		}
		return h.speechListen(ctx, s, listen)
	}
	walk := boolArg(args, "walk")
	limit := intArgOr(args, "limit", speechLimit)
	if limit < 1 || limit > speechMaxLimit {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "limit is how many moves a walk makes, from 1 to %d; "+
			"got %d", speechMaxLimit, limit)
	}
	var view *SpeechView
	switch s.backend {
	case BackendUIA2, BackendDump:
		view, err = h.talkBackSpeech(ctx, s, target, walk, limit)
	default:
		view, err = h.voiceOverSpeech(ctx, s, target, walk, limit)
	}
	if err != nil {
		return nil, err
	}
	if target != "" || walk {
		// The cursor's moves scroll a list to keep it in view.
		delete(h.refs, s.dev.Serial)
	}
	return Result(view.text(), view), nil
}

// speechLimit is how many moves a walk makes by default: a screen of
// MobiumApp's took 24 on the iPhone.
const (
	speechLimit    = 40
	speechMaxLimit = 200
)

// SpeechView is the result of app_speech.
type SpeechView struct {
	Device string `json:"device"`
	// ScreenReader is "VoiceOver" or "TalkBack".
	ScreenReader string `json:"screen_reader"`
	// Steps is what was said, in the order it was said: one for the element
	// the cursor is on, or a walk's every move.
	Steps []SpeechStep `json:"steps"`
	// Ended says why a walk stopped: "end", the cursor would go no further;
	// "wrapped", it came back round to the first element; "limit", the moves
	// ran out; "listened", a listen stopped, and Steps is what was said since
	// it started. Absent when nothing walked.
	Ended string `json:"ended,omitempty"`
	// Answered names a notice the call itself raised and answered: TalkBack
	// 17 tells of its new keymap when a keyboard connects.
	Answered string `json:"answered,omitempty"`
	// Listening says the screen reader is being listened to, after a listen
	// start; a stop answers with what it said since, in Steps.
	Listening bool `json:"listening,omitempty"`
	// StillTalking says a listen stopped at its limit while the screen
	// reader was still speaking: what it says next is not in Steps.
	StillTalking bool `json:"still_talking,omitempty"`
}

// SpeechStep is one sentence and what it was said about.
type SpeechStep struct {
	// Said is the screen reader's sentence, as it said it; empty when it
	// said nothing there.
	Said string `json:"said"`
	// Cause is what made it speak, while listening: "focus", the cursor
	// moved; "announcement", the app asked for it to be said; "change",
	// something on the screen changed, as a live region does. Absent for a
	// walk's steps, which are all the cursor's moves.
	Cause string `json:"cause,omitempty"`
	// Element is what the cursor was on. Absent when that cannot be told:
	// on iOS the element is found by the name the sentence starts with, and
	// a name several elements share names none of them.
	Element *SpokenElement `json:"element,omitempty"`
}

// SpokenElement is the element a sentence was said about.
type SpokenElement struct {
	Label   string       `json:"label"`
	Role    string       `json:"role,omitempty"`
	Locator *LocatorView `json:"locator,omitempty"`
	// InMap says map lists it as something to act on. A heading, a caption
	// and a status line are spoken and not listed.
	InMap bool `json:"in_map"`
}

func (v *SpeechView) text() string {
	var b strings.Builder
	if v.Listening {
		return fmt.Sprintf("listening to %s — act, then stop it with listen stop", v.ScreenReader)
	}
	if v.Ended == "listened" {
		if len(v.Steps) == 0 && !v.StillTalking {
			return fmt.Sprintf("%s said nothing since listening started", v.ScreenReader)
		}
		fmt.Fprintf(&b, "%s said, since listening started:", v.ScreenReader)
	} else {
		fmt.Fprintf(&b, "%s says:", v.ScreenReader)
	}
	for i, st := range v.Steps {
		said := fmt.Sprintf("%q", st.Said)
		if st.Said == "" {
			said = "(nothing)"
		}
		if len(v.Steps) > 1 {
			fmt.Fprintf(&b, "\n%3d. %s", i+1, said)
		} else {
			fmt.Fprintf(&b, " %s", said)
		}
		if e := st.Element; e != nil {
			fmt.Fprintf(&b, " — %s", e.Label)
			if e.Role != "" {
				fmt.Fprintf(&b, " (%s)", e.Role)
			}
			if !e.InMap {
				b.WriteString(", not in map")
			}
		}
		if c := causeWords[st.Cause]; c != "" {
			fmt.Fprintf(&b, " [%s]", c)
		}
	}
	if v.Answered != "" {
		fmt.Fprintf(&b, "\n(%s)", v.Answered)
	}
	switch v.Ended {
	case "end":
		b.WriteString("\nended: the cursor would go no further")
	case "wrapped":
		b.WriteString("\nended: the cursor came back round to the first element")
	case "limit":
		b.WriteString("\nended: the moves ran out before the screen did — raise limit to go on")
	}
	if v.StillTalking {
		fmt.Fprintf(&b, "\n(%s was still talking when listening stopped, after %s: what it says next is not here)",
			v.ScreenReader, listenWait)
	}
	return b.String()
}

// passwordSaid stands in for a sentence said about a password field, which
// can hold the password: TalkBack's "Speak passwords" is on by default.
const passwordSaid = "(said about a password field, and not shown)"

// spokenElement is the wire shape of the node a sentence was said about.
func spokenElement(n *uitree.Node, t *uitree.Tree) *SpokenElement {
	if n == nil {
		return nil
	}
	loc := uitree.Derive(n, t)
	return &SpokenElement{
		Label: uitree.Describe(n),
		Role:  uitree.RoleOf(n),
		Locator: &LocatorView{Kind: string(loc.Kind), Value: loc.Value, Exact: loc.Exact,
			Role: loc.Role},
		InMap: uitree.Actionable(n),
	}
}

// spokenStep pairs a sentence with its node, keeping a password out: one
// said about a password field, or one that calls its element VoiceOver's
// secure text field whether or not it was paired — on the iPhone VoiceOver
// said "password Secure text field Is editing" and a bullet a character, a
// field whose name another element shared and that went unpaired.
func spokenStep(said string, n *uitree.Node, t *uitree.Tree) SpeechStep {
	if said != "" && ((n != nil && n.Password) || strings.Contains(wordsOf(said), "secure text field")) {
		said = passwordSaid
	}
	return SpeechStep{Said: said, Element: spokenElement(n, t)}
}

// screenReaderOff is the refusal while the screen reader is off.
func screenReaderOff(name string) error {
	return mobiumerr.New(mobiumerr.DeviceNotReady, "app_speech reads what the screen reader says, and %s is off", name).
		WithRemedy("turn it on with app_accessibility screen_reader on, which the session puts back when it ends")
}

// ---- VoiceOver ---------------------------------------------------------------

// voiceOverStuck is how long a move past the last element takes: VoiceOver
// stays and says the same again after about 2.4 seconds, where a move takes
// 0.58 (the iPhone 15 Plus). A same sentence said quickly is a neighbor
// spoken alike, and the walk goes on.
const voiceOverStuck = 1500 * time.Millisecond

func (h *Handlers) voiceOverSpeech(ctx context.Context, s *session, target string, walk bool, limit int) (*SpeechView, error) {
	sp, ok := mobiumdriver.AsScreenReaderSpeech(s.driver)
	reader, rok := mobiumdriver.AsScreenReader(s.driver)
	if !ok || !rok {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "the %s driver cannot read what a screen reader says", s.backend)
	}
	on, known := reader.ScreenReaderOn(ctx)
	if !known {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "VoiceOver's speech is read through WebDriverAgent's "+
			"VoiceOver routes, which need a runner built by Xcode 27 on iOS 27 or later. A phone's runner is "+
			"rebuilt to have them; a simulator's is WebDriverAgent's prebuilt release, which has none, and an "+
			"Apple TV's is not measured")
	}
	if !on {
		return nil, screenReaderOff("VoiceOver")
	}
	view := &SpeechView{Device: s.dev.Serial, ScreenReader: "VoiceOver", Steps: []SpeechStep{}}
	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	// name pairs a sentence with its element, reading the screen again once
	// when nothing on it matches: the cursor's move scrolled the list.
	name := func(said string) *uitree.Node {
		if n := spokenNode(tree, said); n != nil {
			return n
		}
		if t, err := s.driver.Snapshot(ctx); err == nil {
			tree = t
		}
		return spokenNode(tree, said)
	}
	var said string
	if target != "" {
		node, t, err := h.resolveNode(ctx, s, target)
		if err != nil {
			return nil, err
		}
		tree = t
		x, y := node.Bounds.Center()
		if said, err = reader.FocusAt(ctx, x, y); err != nil {
			return nil, err
		}
		// The touch put the cursor on the target: it is the element.
		view.Steps = append(view.Steps, spokenStep(said, node, tree))
	} else {
		if said, err = sp.Speech(ctx); err != nil {
			return nil, err
		}
		view.Steps = append(view.Steps, spokenStep(said, name(said), tree))
	}
	if !walk {
		return view, nil
	}
	view.Ended = "limit"
	for i := 0; i < limit; i++ {
		start := time.Now()
		next, err := sp.MoveCursor(ctx, true)
		if err != nil {
			return nil, err
		}
		if next == said && time.Since(start) > voiceOverStuck {
			view.Ended = "end"
			break
		}
		said = next
		view.Steps = append(view.Steps, spokenStep(said, name(said), tree))
	}
	return view, nil
}

// spokenNode is the element a VoiceOver sentence was said about: the shown
// element whose name the sentence starts with, the longest such name — "Form
// Demo Button" is Form Demo's, not the Form heading's. A shorter name followed
// by nothing but a kind is a rival, not a lesser match: VoiceOver drops a
// label's own trailing kind, so a button named "Save button" and one named
// "Save" both say "Save Button" (measured on the iPhone, MobiumApp's Screen
// Reader Demo), and the first version paired the second with the first.
// Rivals name none, unless they are one element nested in itself, as React
// Native repeats one on several views (CHALLENGES 55) and a back button holds
// its own title; then the innermost control.
func spokenNode(t *uitree.Tree, said string) *uitree.Node {
	s := wordsOf(said)
	if t == nil || s == "" {
		return nil
	}
	type candidate struct {
		n    *uitree.Node
		name string
	}
	var cands []candidate
	longest := 0
	t.Walk(func(n *uitree.Node) bool {
		// The application is named for the app and is the whole screen, and
		// the cursor is never on it: a back button VoiceOver calls
		// "MobiumApp Back Button" is labeled Back.
		if !n.Displayed || n.Parent == nil || n.Class == "XCUIElementTypeApplication" {
			return true
		}
		label := n.Label
		if label == "" && !n.Password {
			label = n.Text
		}
		w := wordsOf(label)
		if w == "" || !(s == w || strings.HasPrefix(s, w+" ")) {
			return true
		}
		cands = append(cands, candidate{n, w})
		if len(w) > longest {
			longest = len(w)
		}
		return true
	})
	var rivals []*uitree.Node
	for _, c := range cands {
		rest := strings.Fields(strings.TrimPrefix(s, c.name))
		if len(c.name) == longest || (len(rest) > 0 && voiceOverKinds[rest[0]]) {
			rivals = append(rivals, c.n)
		}
	}
	if len(rivals) == 0 {
		return nil
	}
	// One nest: every rival an ancestor or descendant of the innermost.
	inner := rivals[0]
	for _, n := range rivals[1:] {
		switch {
		case isAncestor(inner, n):
			inner = n
		case isAncestor(n, inner):
		default:
			return nil
		}
	}
	for n := inner; n != nil; n = n.Parent {
		for _, r := range rivals {
			if r == n && uitree.Actionable(n) {
				return n
			}
		}
	}
	return inner
}

// voiceOverKinds are the words VoiceOver says after a name for what the
// element is or how it stands — its traits and states, and the start of a
// hint.
var voiceOverKinds = map[string]bool{
	"button": true, "link": true, "heading": true, "image": true, "header": true, "selected": true,
	"dimmed": true, "switch": true, "toggle": true, "adjustable": true, "tab": true, "text": true,
	"secure": true, "search": true, "checkbox": true, "radio": true, "menu": true, "popup": true,
	"slider": true, "double": true, "on": true, "off": true, "checked": true, "unchecked": true,
	"collapsed": true, "expanded": true,
}

func isAncestor(a, n *uitree.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p == a {
			return true
		}
	}
	return false
}

// wordsOf is s lowercased, with everything but letters and digits a single
// space.
func wordsOf(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

// ---- TalkBack ----------------------------------------------------------------

// talkBackFocusReads is how many reads a move's node is given to agree with
// what TalkBack said.
const talkBackFocusReads = 5

// talkBackSpeakWait bounds waiting for TalkBack to say something after a
// move: it logged its sentence about 70ms after the press on the emulator,
// and said nothing at all for an element with nothing to say.
const talkBackSpeakWait = 1500 * time.Millisecond

func (h *Handlers) talkBackSpeech(ctx context.Context, s *session, target string, walk bool, limit int) (*SpeechView, error) {
	adb, err := h.adbFor(ctx, s)
	if err != nil {
		return nil, err
	}
	on, err := adb.TalkBackOn(ctx)
	if err != nil {
		return nil, err
	}
	if !on {
		return nil, screenReaderOff("TalkBack")
	}
	spoken, err := adb.TalkBackSpoken(ctx)
	if err != nil {
		return nil, err
	}
	// heard is every utterance in the log so far; anything else is new.
	heard := map[[3]int64]bool{}
	lastFocus := ""
	for _, sp := range spoken {
		heard[sp.Key()] = true
		if sp.Focus {
			lastFocus = sp.Text
		}
	}
	view := &SpeechView{Device: s.dev.Serial, ScreenReader: "TalkBack", Steps: []SpeechStep{}}
	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if target == "" && !walk {
		if len(spoken) == 0 {
			return nil, device.ErrTalkBackQuiet
		}
		n := readerFocused(tree, lastFocus)
		if n == nil && lastFocus == "" {
			return view, nil // the cursor is on nothing, and nothing was said
		}
		view.Steps = append(view.Steps, spokenStep(lastFocus, n, tree))
		return view, nil
	}

	kb, err := adb.OpenKeyboard(ctx)
	if err != nil {
		return nil, err
	}
	defer kb.Close()
	view.Answered = h.answerKeymapNotice(ctx, s)
	// move presses Alt+Right and reads what TalkBack said and where its
	// cursor went.
	move := func() (string, *uitree.Node, error) {
		if err := kb.Press(device.ModAlt, device.KeyRight); err != nil {
			return "", nil, err
		}
		said := ""
		deadline := time.Now().Add(talkBackSpeakWait)
		for time.Now().Before(deadline) && said == "" {
			if err := sleepCtx(ctx, 60*time.Millisecond); err != nil {
				return "", nil, err
			}
			all, err := adb.TalkBackSpoken(ctx)
			if err != nil {
				return "", nil, err
			}
			for _, sp := range all {
				if !heard[sp.Key()] {
					heard[sp.Key()] = true
					if sp.Focus && said == "" {
						said = sp.Text
					}
				}
			}
		}
		if said == "" && len(heard) == 0 {
			// Nothing in the log before or after a move: TalkBack is not
			// logging what it says.
			return "", nil, device.ErrTalkBackQuiet
		}
		// The tree's cursor can lag TalkBack's: on the Pixel a move that
		// said "Back. Button" read the heading the cursor had left. A node
		// is taken only when the sentence holds its name; a few reads, and
		// then none rather than a guess.
		var at *uitree.Node
		for try := 0; try < talkBackFocusReads; try++ {
			if try > 0 {
				if err := sleepCtx(ctx, 150*time.Millisecond); err != nil {
					return "", nil, err
				}
			}
			t, err := s.driver.Snapshot(ctx)
			if err != nil {
				return "", nil, err
			}
			tree = t
			if at = readerFocused(t, said); at == nil || saysName(said, at) {
				break
			}
			at = nil
		}
		return said, at, nil
	}

	if target != "" {
		// Found again in every read: connecting the keyboard puts the
		// on-screen one away, and the fields move — the Login Demo's
		// password field, typed into first, was never reached at where it
		// had been.
		loc, err := h.locatorFor(s.dev.Serial, target)
		if err != nil {
			return nil, err
		}
		if _, _, err := h.resolveNode(ctx, s, target); err != nil {
			return nil, err
		}
		if t, err := s.driver.Snapshot(ctx); err == nil {
			tree = t
		}
		isTarget := func(n *uitree.Node) bool {
			for _, m := range loc.Resolve(tree) {
				if sameNode(n, m) {
					return true
				}
			}
			return false
		}
		said, at := lastFocus, readerFocused(tree, lastFocus)
		for i := 0; at == nil || !isTarget(at); i++ {
			if i == limit {
				return nil, mobiumerr.New(mobiumerr.NoSuchElement, "TalkBack's cursor did not reach %s in %d moves; "+
					"it moves only to what TalkBack speaks, and a control inside a spoken group is read as part "+
					"of the group", target, limit).WithRemedy("walk the screen with app_speech walk to see where the cursor goes")
			}
			if said, at, err = move(); err != nil {
				return nil, err
			}
		}
		view.Steps = append(view.Steps, spokenStep(said, at, tree))
	}
	if !walk {
		return view, nil
	}
	view.Ended = "limit"
	var first *uitree.Node
	if len(view.Steps) > 0 {
		first = readerFocused(tree, view.Steps[0].Said)
	}
	for i := 0; i < limit; i++ {
		said, at, err := move()
		if err != nil {
			return nil, err
		}
		if first == nil {
			first = at
		} else if at != nil && sameNode(at, first) {
			view.Ended = "wrapped"
			break
		}
		view.Steps = append(view.Steps, spokenStep(said, at, tree))
	}
	return view, nil
}

// readerFocused is the node TalkBack's cursor is on: UiAutomator2 marks it
// a11y-focused. The dump format has no such mark, and there it is found by
// the sentence, as on iOS.
func readerFocused(t *uitree.Tree, said string) *uitree.Node {
	var found *uitree.Node
	t.Walk(func(n *uitree.Node) bool {
		if n.ReaderFocused {
			found = n
		}
		return found == nil
	})
	if found != nil {
		return found
	}
	return spokenNode(t, said)
}

// keymapNotice is TalkBack 17.1's dialog when a keyboard connects, measured
// on the Pixel 8 Pro: "New keymap available", with Learn more, Settings and
// Got it. It is the keyboard's doing, so the call answers it, with Got it,
// which switches nothing — left up, it sits over the app, and on the Pixel it
// blocked the next read. TalkBack on the Android 15 emulator never raised it.
const (
	keymapNotice = "New keymap available"
	keymapGotIt  = "Got it"
	keymapWait   = time.Second
)

// answerKeymapNotice waits a moment for TalkBack's keymap notice and answers
// it, saying so; "" when it did not come up.
func (h *Handlers) answerKeymapNotice(ctx context.Context, s *session) string {
	ctrl, ok := mobiumdriver.AsAlerts(s.driver)
	if !ok {
		return ""
	}
	deadline := time.Now().Add(keymapWait)
	for time.Now().Before(deadline) {
		text, err := ctrl.AlertText(ctx)
		if err == nil && strings.HasPrefix(strings.TrimSpace(text), keymapNotice) {
			tree, err := s.driver.Snapshot(ctx)
			if err != nil {
				return ""
			}
			if _, err := h.pressAlertButton(ctx, s, tree, text, keymapGotIt, AlertView{}); err != nil {
				return ""
			}
			return fmt.Sprintf("TalkBack's %q notice came up for the keyboard, and was answered %q", keymapNotice, keymapGotIt)
		}
		if sleepCtx(ctx, 250*time.Millisecond) != nil {
			return ""
		}
	}
	return ""
}

// saysName says whether a sentence holds the node's name as whole words —
// TalkBack says it among the state and the kind ("not checked. Email me.
// Check box"). A node with no name of its own agrees with anything: an icon
// with none is said as its kind alone.
func saysName(said string, n *uitree.Node) bool {
	name := n.Label
	if name == "" && !n.Password {
		name = n.Text
	}
	w := wordsOf(name)
	if w == "" {
		return true
	}
	return strings.Contains(" "+wordsOf(said)+" ", " "+w+" ")
}

// ---- Listening ---------------------------------------------------------------

// speechListen is a listen's mark. On Android, every utterance in TalkBack's
// log when it started, so anything else is said since. On iOS, which keeps
// no log, a poller asking VoiceOver what it is saying, and what it heard.
type speechListen struct {
	heard map[[3]int64]bool

	mu     sync.Mutex
	said   []string  // each new sentence VoiceOver said, in order
	last   time.Time // when one last came
	cancel context.CancelFunc
	done   chan struct{}
}

// end stops an iOS poller, if there is one.
func (l *speechListen) end() {
	if l.cancel != nil {
		l.cancel()
		<-l.done
	}
}

// voiceOverPoll is how often a listen asks VoiceOver what it is saying, and
// voiceOverListenMax how long a forgotten listen goes on. Measured on the
// iPhone: polled every quarter second through a wrong sign-in, it gave "Log
// In Button", "Signing in… dimmed" and the app's announcement "Incorrect
// username or password." in turn — /currentSpeech is what VoiceOver is
// saying now, an announcement included. A sentence over in less than a poll
// can fall between two.
const (
	voiceOverPoll      = 250 * time.Millisecond
	voiceOverListenMax = 10 * time.Minute
)

// listenVoiceOver starts polling what VoiceOver says, recording each new
// sentence after the one it was saying at the start.
func listenVoiceOver(sp mobiumdriver.ScreenReaderSpeech, first string) *speechListen {
	ctx, cancel := context.WithTimeout(context.Background(), voiceOverListenMax)
	l := &speechListen{cancel: cancel, done: make(chan struct{}), last: time.Now()}
	go func() {
		defer close(l.done)
		prev := first
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(voiceOverPoll):
			}
			said, err := sp.Speech(ctx)
			if err != nil || said == "" || said == prev {
				continue
			}
			prev = said
			l.mu.Lock()
			l.said = append(l.said, said)
			l.last = time.Now()
			l.mu.Unlock()
		}
	}()
	return l
}

// Listening waits for TalkBack to stop talking before it answers: on the
// Android 15 emulator a live region's change was said 0.4 seconds after the
// tap, and the re-render of a tap beside it 1.25 seconds after — but behind a
// paragraph TalkBack was still reading, the polite change waited about five
// seconds, logged only once its turn came. Quiet is listenQuiet with no new
// sentence and no new word begun; it never waits past listenWait.
const (
	listenQuiet = 1500 * time.Millisecond
	listenWait  = 20 * time.Second
)

// causes maps TalkBack's event types to what app_speech calls them.
var causes = map[string]string{
	"TYPE_VIEW_ACCESSIBILITY_FOCUSED": "focus",
	"TYPE_ANNOUNCEMENT":               "announcement",
	"TYPE_WINDOW_CONTENT_CHANGED":     "change",
	"TYPE_WINDOW_STATE_CHANGED":       "change",
}

// causeWords is how the text says a cause.
var causeWords = map[string]string{
	"focus":        "the cursor moved",
	"announcement": "announced by the app",
	"change":       "a change on screen",
}

// cause is what app_speech calls the event behind an utterance.
func cause(event string) string {
	if c, ok := causes[event]; ok {
		return c
	}
	return strings.ToLower(strings.TrimPrefix(event, "TYPE_"))
}

// speechListen starts listening, or stops and answers with what the screen
// reader said in between. It is how a test hears what an action makes it
// say — a live region's new count, an app's announcement, or nothing: a
// change no one is told of is the failure step 2's control is built for.
func (h *Handlers) speechListen(ctx context.Context, s *session, action string) (*ToolsCallResult, error) {
	if s.backend != BackendUIA2 && s.backend != BackendDump {
		return h.voiceOverListen(ctx, s, action)
	}
	adb, err := h.adbFor(ctx, s)
	if err != nil {
		return nil, err
	}
	view := &SpeechView{Device: s.dev.Serial, ScreenReader: "TalkBack", Steps: []SpeechStep{}}
	switch action {
	case "start":
		if s.listening != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "already listening — stop it with listen stop first")
		}
		on, err := adb.TalkBackOn(ctx)
		if err != nil {
			return nil, err
		}
		if !on {
			return nil, screenReaderOff("TalkBack")
		}
		spoken, err := adb.TalkBackSpoken(ctx)
		if err != nil {
			return nil, err
		}
		if len(spoken) == 0 {
			return nil, device.ErrTalkBackQuiet
		}
		mark := &speechListen{heard: map[[3]int64]bool{}}
		for _, sp := range spoken {
			mark.heard[sp.Key()] = true
		}
		s.listening = mark
		view.Listening = true
		return Result(view.text(), view), nil

	case "stop":
		mark := s.listening
		if mark == nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "not listening — start with listen start, act, "+
				"then stop")
		}
		s.listening = nil
		var said []device.Spoken
		start, last := time.Now(), time.Now()
		word := int64(-1)
		for time.Since(last) < listenQuiet && time.Since(start) < listenWait {
			if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
				return nil, err
			}
			spoken, lastWord, err := adb.TalkBackHeard(ctx)
			if err != nil {
				return nil, err
			}
			if word >= 0 && lastWord != word {
				last = time.Now() // a word begun since the last read: still talking
			}
			word = lastWord
			for _, sp := range spoken {
				if !mark.heard[sp.Key()] {
					mark.heard[sp.Key()] = true
					said = append(said, sp)
					last = time.Now()
				}
			}
		}
		if time.Since(last) < listenQuiet {
			view.StillTalking = true
		}
		tree, err := s.driver.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		for _, sp := range said {
			n := readerFocused(tree, sp.Text)
			if !sp.Focus {
				// Not the cursor's: the element whose words were said.
				n = spokenNode(tree, sp.Text)
			} else if n != nil && !saysName(sp.Text, n) {
				n = nil
			}
			st := spokenStep(sp.Text, n, tree)
			st.Cause = cause(sp.Event)
			view.Steps = append(view.Steps, st)
		}
		view.Ended = "listened"
		return Result(view.text(), view), nil
	}
	return nil, mobiumerr.New(mobiumerr.InvalidArgument, "listen is \"start\" or \"stop\", got %q", action)
}

// voiceOverListen is listening on an iPhone: what VoiceOver says, polled.
// WebDriverAgent does not say why it spoke, so the steps carry no cause.
func (h *Handlers) voiceOverListen(ctx context.Context, s *session, action string) (*ToolsCallResult, error) {
	view := &SpeechView{Device: s.dev.Serial, ScreenReader: "VoiceOver", Steps: []SpeechStep{}}
	switch action {
	case "start":
		if s.listening != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "already listening — stop it with listen stop first")
		}
		sp, ok := mobiumdriver.AsScreenReaderSpeech(s.driver)
		reader, rok := mobiumdriver.AsScreenReader(s.driver)
		if !ok || !rok {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "the %s driver cannot read what a screen reader says", s.backend)
		}
		on, known := reader.ScreenReaderOn(ctx)
		if !known {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "VoiceOver's speech is read through WebDriverAgent's "+
				"VoiceOver routes, which need a runner built by Xcode 27 on iOS 27 or later; a simulator's runner "+
				"is WebDriverAgent's prebuilt release, which has none")
		}
		if !on {
			return nil, screenReaderOff("VoiceOver")
		}
		first, err := sp.Speech(ctx)
		if err != nil {
			return nil, err
		}
		s.listening = listenVoiceOver(sp, first)
		view.Listening = true
		return Result(view.text(), view), nil

	case "stop":
		l := s.listening
		if l == nil || l.cancel == nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "not listening — start with listen start, act, "+
				"then stop")
		}
		start := time.Now()
		for {
			l.mu.Lock()
			quiet := time.Since(l.last) >= listenQuiet && time.Since(start) >= listenQuiet
			l.mu.Unlock()
			if quiet || time.Since(start) >= listenWait {
				break
			}
			if err := sleepCtx(ctx, voiceOverPoll); err != nil {
				return nil, err
			}
		}
		l.end()
		s.listening = nil
		l.mu.Lock()
		said := append([]string(nil), l.said...)
		view.StillTalking = time.Since(l.last) < listenQuiet
		l.mu.Unlock()
		tree, err := s.driver.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		for _, text := range said {
			view.Steps = append(view.Steps, spokenStep(text, spokenNode(tree, text), tree))
		}
		view.Ended = "listened"
		return Result(view.text(), view), nil
	}
	return nil, mobiumerr.New(mobiumerr.InvalidArgument, "listen is \"start\" or \"stop\", got %q", action)
}
