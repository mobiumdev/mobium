package device

import (
	"bufio"
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// What interrupted an app's audio is in Android's audio service, not in the
// audio: `dumpsys audio` keeps a log of its players — each one's app and what
// it is for, when it started and stopped — and of what it did to them. On an
// incoming call it logs "call: muting piid:N uid:U" for every player then
// playing, and "call: unmuting piid:N" once the call is over; measured the
// same on the Android 15 emulator and on a Pixel 8 Pro on Android 17. The
// mute is the player's own volume, which no capture from the shell hears —
// on the Pixel the app's samples ran on unchanged through a real call — and
// the system's ringtone is kept out of such a capture altogether. So an
// interruption is read here, and it is the same on any device adb reaches.

// AudioInterruption is something that cut across an app's audio while it
// was captured: the app's own player muted, for a call or anything else, or
// another app's player sounding over it — a ringtone, an alarm, a
// notification, another app's media. From and To are since the capture
// began; Open means it had not ended when the capture stopped.
type AudioInterruption struct {
	Kind string `json:"kind"`
	// Reason is why the app was muted — "call", or the audio service's own
	// sources: "streamVolume" is the device's volume at its lowest,
	// "clientVolume" the player's own volume — and Usage what the other
	// player said it was for.
	Reason string        `json:"reason,omitempty"`
	Usage  string        `json:"usage,omitempty"`
	From   time.Duration `json:"from"`
	To     time.Duration `json:"to"`
	Open   bool          `json:"open,omitempty"`
}

// Kinds of interruption: the app muted, or another app's player sounding,
// named by what it is for.
const (
	InterruptMuted        = "muted"
	InterruptRingtone     = "ringtone"
	InterruptAlarm        = "alarm"
	InterruptNotification = "notification"
	InterruptVoiceCall    = "voice_call"
	InterruptAssistant    = "assistant"
	InterruptNavigation   = "navigation"
	InterruptMedia        = "media"
	InterruptSound        = "sound"
)

// interruptingUsage is the kind of interruption another app's player of each
// usage is. A touch's click and the system's other interface sounds are
// none: every tap would be one, and the capture reports them as sound.
func interruptingUsage(usage string) string {
	switch {
	case usage == "USAGE_ASSISTANCE_SONIFICATION" || usage == "USAGE_ASSISTANCE_ACCESSIBILITY":
		return ""
	case usage == "USAGE_NOTIFICATION_RINGTONE":
		return InterruptRingtone
	case usage == "USAGE_ALARM":
		return InterruptAlarm
	case strings.HasPrefix(usage, "USAGE_NOTIFICATION"):
		return InterruptNotification
	case strings.HasPrefix(usage, "USAGE_VOICE_COMMUNICATION"):
		return InterruptVoiceCall
	case usage == "USAGE_ASSISTANT":
		return InterruptAssistant
	case usage == "USAGE_ASSISTANCE_NAVIGATION_GUIDANCE":
		return InterruptNavigation
	case usage == "USAGE_MEDIA" || usage == "USAGE_GAME":
		return InterruptMedia
	}
	return InterruptSound
}

// DeviceClock reads the device's clock, as the audio service's log writes it:
// local time, with no zone.
func (a *ADB) DeviceClock(ctx context.Context) (time.Time, error) {
	out, err := a.Shell(ctx, "date", "+%Y-%m-%dT%H:%M:%S.%N")
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse("2006-01-02T15:04:05.999999999", strings.TrimSpace(string(out)))
	if err != nil {
		return time.Time{}, mobiumerr.New(mobiumerr.DeviceServer, "the device's clock read %q", strings.TrimSpace(string(out)))
	}
	return t, nil
}

// PackageUID is the uid an installed package's processes run as.
func (a *ADB) PackageUID(ctx context.Context, pkg string) (int, error) {
	out, err := a.Shell(ctx, "cmd", "package", "list", "packages", "-U", pkg)
	if err != nil {
		return 0, err
	}
	// The filter matches part of a name, so the line must be the package's own.
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "package:"+pkg && strings.HasPrefix(f[1], "uid:") {
			if uid, err := strconv.Atoi(strings.TrimPrefix(f[1], "uid:")); err == nil {
				return uid, nil
			}
		}
	}
	return 0, mobiumerr.New(mobiumerr.NoSuchElement, "%s is not installed", pkg)
}

// PackageInFront is the package of the app in front: the root of the task in
// front, which names the app it belongs to.
func (a *ADB) PackageInFront(ctx context.Context) (string, error) {
	out, err := a.Shell(ctx, "dumpsys", "activity", "activities")
	if err != nil {
		return "", err
	}
	top, root := taskInFront(string(out))
	if root != "" {
		return root, nil
	}
	if top != "" {
		return top, nil
	}
	return "", mobiumerr.New(mobiumerr.DeviceNotReady, "no app is in front to capture the audio of — launch it, or name it with app")
}

// AudioInterruptions reads what interrupted the audio of the app running as
// uid between start and end, both by the device's clock.
func (a *ADB) AudioInterruptions(ctx context.Context, uid int, start, end time.Time) ([]AudioInterruption, error) {
	out, err := a.Shell(ctx, "dumpsys", "audio")
	if err != nil {
		return nil, err
	}
	return parseAudioInterruptions(string(out), uid, start, end), nil
}

var (
	audioLogLineRe = regexp.MustCompile(`^(\d\d)-(\d\d) (\d\d):(\d\d):(\d\d):(\d\d\d) (.*)$`)
	newPlayerRe    = regexp.MustCompile(`new player piid:(\d+) uid/pid:(\d+)/`)
	newAttrsRe     = regexp.MustCompile(`player piid:(\d+) new AudioAttributes:`)
	usageRe        = regexp.MustCompile(`usage=(USAGE_[A-Z_]+)`)
	callMuteRe     = regexp.MustCompile(`call: muting piid:(\d+) uid:(\d+)`)
	callUnmuteRe   = regexp.MustCompile(`call: unmuting piid:(\d+)`)
	mutedRe        = regexp.MustCompile(`player piid:(\d+) event:muted updated source:(.*)$`)
	playerEventRe  = regexp.MustCompile(`player piid:(\d+) event:(started|stopped|paused)`)
	releasingRe    = regexp.MustCompile(`releasing player piid:(\d+)`)
)

type audioLogEvent struct {
	at   time.Time
	text string
}

// parseAudioInterruptions reads `dumpsys audio` for what interrupted uid's
// audio between start and end. The dump keeps several logs, each in its own
// order and some repeating a line, so the lines are gathered, deduplicated
// and put in time order first; a player's app and usage are learned from
// every line, whenever it was written, since a player made before the
// capture can still sound during it.
func parseAudioInterruptions(dump string, uid int, start, end time.Time) []AudioInterruption {
	seen := map[string]bool{}
	var events []audioLogEvent
	piidUID := map[string]int{}
	piidUsage := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(dump))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		m := audioLogLineRe.FindStringSubmatch(line)
		if m == nil || seen[line] {
			continue
		}
		seen[line] = true
		text := m[7]
		if p := newPlayerRe.FindStringSubmatch(text); p != nil {
			piidUID[p[1]], _ = strconv.Atoi(p[2])
		}
		if p := callMuteRe.FindStringSubmatch(text); p != nil {
			piidUID[p[1]], _ = strconv.Atoi(p[2])
		}
		if p := newAttrsRe.FindStringSubmatch(text); p != nil {
			if u := usageRe.FindStringSubmatch(text); u != nil {
				piidUsage[p[1]] = u[1]
			}
		} else if p := newPlayerRe.FindStringSubmatch(text); p != nil {
			if u := usageRe.FindStringSubmatch(text); u != nil {
				piidUsage[p[1]] = u[1]
			}
		}
		at, ok := logTime(m, start)
		if !ok {
			continue
		}
		events = append(events, audioLogEvent{at: at, text: text})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.Before(events[j].at) })

	type open struct {
		at     time.Time
		reason string
		usage  string
	}
	mutedNow := map[string]*open{} // the app's players, by piid
	playing := map[string]*open{}  // other apps' players, by piid
	var out []AudioInterruption
	emit := func(kind string, o *open, at time.Time, isOpen bool) {
		if at.Before(start) || o.at.After(end) {
			return
		}
		out = append(out, AudioInterruption{Kind: kind, Reason: o.reason, Usage: o.usage,
			From: clip(o.at, start), To: clip(minTime(at, end), start), Open: isOpen})
	}
	appPlayer := func(piid string) bool { u, ok := piidUID[piid]; return ok && u == uid }
	unmute := func(piid string, at time.Time) {
		if o := mutedNow[piid]; o != nil {
			delete(mutedNow, piid)
			emit(InterruptMuted, o, at, false)
		}
	}
	stopPlaying := func(piid string, at time.Time) {
		if o := playing[piid]; o != nil {
			delete(playing, piid)
			emit(interruptingUsage(o.usage), o, at, false)
		}
	}
	for _, e := range events {
		if e.at.After(end) {
			break
		}
		switch {
		case callMuteRe.MatchString(e.text):
			p := callMuteRe.FindStringSubmatch(e.text)
			if appPlayer(p[1]) {
				if o := mutedNow[p[1]]; o != nil {
					o.reason = "call"
				} else {
					mutedNow[p[1]] = &open{at: e.at, reason: "call"}
				}
			}
		case callUnmuteRe.MatchString(e.text):
			unmute(callUnmuteRe.FindStringSubmatch(e.text)[1], e.at)
		case mutedRe.MatchString(e.text):
			p := mutedRe.FindStringSubmatch(e.text)
			if !appPlayer(p[1]) {
				continue
			}
			sources := strings.TrimSpace(p[2])
			if sources == "none" || sources == "" {
				unmute(p[1], e.at)
			} else if mutedNow[p[1]] == nil {
				mutedNow[p[1]] = &open{at: e.at, reason: strings.Join(strings.Fields(sources), ",")}
			}
		case playerEventRe.MatchString(e.text):
			p := playerEventRe.FindStringSubmatch(e.text)
			if appPlayer(p[1]) || interruptingUsage(piidUsage[p[1]]) == "" {
				continue
			}
			if p[2] == "started" {
				if playing[p[1]] == nil {
					playing[p[1]] = &open{at: e.at, usage: piidUsage[p[1]]}
				}
			} else {
				stopPlaying(p[1], e.at)
			}
		case releasingRe.MatchString(e.text):
			piid := releasingRe.FindStringSubmatch(e.text)[1]
			stopPlaying(piid, e.at)
			unmute(piid, e.at)
		}
	}
	// Still going when the capture stopped.
	for _, o := range mutedNow {
		if !o.at.After(end) {
			out = append(out, AudioInterruption{Kind: InterruptMuted, Reason: o.reason, From: clip(o.at, start), To: end.Sub(start), Open: true})
		}
	}
	for _, o := range playing {
		if !o.at.After(end) {
			out = append(out, AudioInterruption{Kind: interruptingUsage(o.usage), Usage: o.usage, From: clip(o.at, start), To: end.Sub(start), Open: true})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// logTime is a log line's time. The log has no year: it is the capture's,
// and a line that would then fall more than a day after the capture began
// is last year's — a capture across New Year.
func logTime(m []string, start time.Time) (time.Time, bool) {
	var n [6]int
	for i := range n {
		v, err := strconv.Atoi(m[i+1])
		if err != nil {
			return time.Time{}, false
		}
		n[i] = v
	}
	t := time.Date(start.Year(), time.Month(n[0]), n[1], n[2], n[3], n[4], n[5]*int(time.Millisecond), time.UTC)
	if t.After(start.Add(24 * time.Hour)) {
		t = t.AddDate(-1, 0, 0)
	}
	return t, true
}

func clip(t, start time.Time) time.Duration {
	if t.Before(start) {
		return 0
	}
	return t.Sub(start)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// InterruptionReader reads what interrupted the app a capture is for, from
// the capture's start until now.
type InterruptionReader func(ctx context.Context) ([]AudioInterruption, error)

// StartAudioEvents starts a capture that records no sound — a phone's, which
// nothing outside it hears — only the volumes and what interrupted the app.
func StartAudioEvents(ctx context.Context, volumes VolumeReader, interruptions InterruptionReader) AudioRecording {
	r := &eventAudio{started: time.Now(), volumes: volumes, interruptions: interruptions}
	if volumes != nil {
		r.before, _ = volumes(ctx)
	}
	return r
}

type eventAudio struct {
	started       time.Time
	volumes       VolumeReader
	interruptions InterruptionReader
	before        []StreamVolume
}

func (r *eventAudio) Started() time.Time          { return r.started }
func (r *eventAudio) Captures() bool              { return false }
func (r *eventAudio) Discard(ctx context.Context) {}
func (r *eventAudio) Stop(ctx context.Context) (AudioCapture, error) {
	c := AudioCapture{VolumesAtStart: r.before}
	if r.volumes != nil {
		c.VolumesAtEnd, _ = r.volumes(ctx)
	}
	if r.interruptions != nil {
		got, err := r.interruptions(ctx)
		if err != nil {
			return AudioCapture{}, err
		}
		c.Interruptions = got
	}
	return c, nil
}
