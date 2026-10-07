package device

import (
	"testing"
	"time"
)

// The Pixel 8 Pro's own lines during a real call (Android 17), out of order
// and repeated as the dump's several logs give them, around MobiumApp's
// player 19495 playing a tone: the call mutes it, the system's ringtone
// (19511) plays, the call unmutes it.
const pixelCallDump = `
  10-07 03:49:33:819 new player piid:19495 uid/pid:10368/11872 type:android.media.AudioTrack attr:AudioAttributes: usage=USAGE_MEDIA content=CONTENT_TYPE_SONIFICATION
  10-07 03:50:02:755 new player piid:19511 uid/pid:1000/1932 type:android.media.MediaPlayer attr:AudioAttributes: usage=USAGE_UNKNOWN content=CONTENT_TYPE_UNKNOWN
  10-07 03:50:02:817 player piid:19511 new AudioAttributes:AudioAttributes: usage=USAGE_NOTIFICATION_RINGTONE content=CONTENT_TYPE_SONIFICATION
  10-07 03:50:12:468 releasing player piid:19511, uid:1000
  10-07 03:50:03:079 player piid:19511 event:started
  10-07 03:50:02:846 call: muting piid:19495 uid:10368
  10-07 03:50:02:846 call: muting piid:19495 uid:10368
  10-07 03:50:15:447 call: unmuting piid:19495
  10-07 03:50:03:535 player piid:19511 event:muted updated source:none
  10-07 03:50:02:848 player piid:19495 event:muted updated source:clientVolume
  10-07 03:50:12:443 player piid:19495 event:muted updated source:streamVolume clientVolume
  10-07 03:49:50:000 new player piid:19490 uid/pid:10368/11872 type:android.media.AudioTrack attr:AudioAttributes: usage=USAGE_ASSISTANCE_SONIFICATION content=CONTENT_TYPE_SONIFICATION
  10-07 03:49:50:010 player piid:19490 event:started
`

func at(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05.000", "2026-"+s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseAudioInterruptionsACall(t *testing.T) {
	start, end := at("10-07 03:49:40.000"), at("10-07 03:50:35.000")
	got := parseAudioInterruptions(pixelCallDump, 10368, start, end)
	want := []AudioInterruption{
		{Kind: InterruptMuted, Reason: "call", From: 22846 * time.Millisecond, To: 35447 * time.Millisecond},
		{Kind: InterruptRingtone, Usage: "USAGE_NOTIFICATION_RINGTONE", From: 23079 * time.Millisecond, To: 32468 * time.Millisecond},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A capture that stops while the phone still rings says it was still going.
func TestParseAudioInterruptionsStillGoing(t *testing.T) {
	start, end := at("10-07 03:49:40.000"), at("10-07 03:50:10.000")
	got := parseAudioInterruptions(pixelCallDump, 10368, start, end)
	if len(got) != 2 || !got[0].Open || !got[1].Open || got[0].To != 30*time.Second {
		t.Fatalf("got %+v", got)
	}
}

// Another app's call mute is not this app's interruption, and a capture that
// ended before the call heard none of it.
func TestParseAudioInterruptionsOtherAppAndOtherTime(t *testing.T) {
	start, end := at("10-07 03:49:40.000"), at("10-07 03:50:35.000")
	got := parseAudioInterruptions(pixelCallDump, 10999, start, end)
	if len(got) != 1 || got[0].Kind != InterruptRingtone {
		t.Fatalf("another app: got %+v", got)
	}
	if got := parseAudioInterruptions(pixelCallDump, 10368, at("10-07 03:40:00.000"), at("10-07 03:45:00.000")); len(got) != 0 {
		t.Fatalf("before the call: got %+v", got)
	}
}

// The log has no year; a line from late December read in a capture of early
// January is last year's, not next December's.
func TestLogTimeAcrossNewYear(t *testing.T) {
	m := audioLogLineRe.FindStringSubmatch("12-31 23:59:59:500 call: unmuting piid:1")
	got, ok := logTime(m, time.Date(2027, 1, 1, 0, 0, 5, 0, time.UTC))
	if !ok || got.Year() != 2026 {
		t.Fatalf("got %v", got)
	}
}

// The emulator's lines when a Clock timer rang over MobiumApp's tone
// (Android 15): an alarm from another app, which mutes nothing.
const emulatorAlarmDump = `
10-07 03:59:51:306 new player piid:615 uid/pid:10218/8827 type:android.media.AudioTrack attr:AudioAttributes: usage=USAGE_MEDIA content=CONTENT_TYPE_SONIFICATION
10-07 03:59:51:307 player piid:615 event:started
10-07 03:59:55:404 new player piid:631 uid/pid:10151/8911 type:android.media.MediaPlayer attr:AudioAttributes: usage=USAGE_UNKNOWN content=CONTENT_TYPE_UNKNOWN
10-07 03:59:55:414 player piid:631 new AudioAttributes:AudioAttributes: usage=USAGE_ALARM content=CONTENT_TYPE_SONIFICATION
10-07 03:59:55:430 player piid:631 event:started
10-07 04:00:01:348 focus requester:android.media.AudioManager@4f0d1 in uid:10151 pack:com.google.android.deskclock died
10-07 04:00:01:350 releasing player piid:631, uid:10151
10-07 04:00:04:091 releasing player piid:615, uid:10218
`

func TestParseAudioInterruptionsAnAlarm(t *testing.T) {
	got := parseAudioInterruptions(emulatorAlarmDump, 10218, at("10-07 03:59:51.000"), at("10-07 04:00:05.000"))
	want := AudioInterruption{Kind: InterruptAlarm, Usage: "USAGE_ALARM", From: 4430 * time.Millisecond, To: 10350 * time.Millisecond}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Any other app's sound is an interruption, named by its usage; a touch's
// click is not; and the app muted for any reason is, with the reason.
func TestParseAudioInterruptionsAnySound(t *testing.T) {
	dump := `
10-07 05:00:00:000 new player piid:1 uid/pid:10218/1 type:android.media.AudioTrack attr:AudioAttributes: usage=USAGE_MEDIA content=CONTENT_TYPE_MUSIC
10-07 05:00:00:500 new player piid:2 uid/pid:10300/2 type:android.media.AudioTrack attr:AudioAttributes: usage=USAGE_MEDIA content=CONTENT_TYPE_MUSIC
10-07 05:00:00:600 new player piid:3 uid/pid:1000/3 type:android.media.SoundPool attr:AudioAttributes: usage=USAGE_ASSISTANCE_SONIFICATION content=CONTENT_TYPE_SONIFICATION
10-07 05:00:00:700 new player piid:4 uid/pid:10400/4 type:android.media.AudioTrack attr:AudioAttributes: usage=USAGE_ASSISTANCE_NAVIGATION_GUIDANCE content=CONTENT_TYPE_SPEECH
10-07 05:00:01:000 player piid:2 event:started
10-07 05:00:01:100 player piid:3 event:started
10-07 05:00:02:000 player piid:2 event:paused
10-07 05:00:03:000 player piid:1 event:muted updated source:streamVolume
10-07 05:00:04:000 player piid:1 event:muted updated source:none
10-07 05:00:04:500 player piid:4 event:started
`
	got := parseAudioInterruptions(dump, 10218, at("10-07 05:00:00.000"), at("10-07 05:00:05.000"))
	want := []AudioInterruption{
		{Kind: InterruptMedia, Usage: "USAGE_MEDIA", From: time.Second, To: 2 * time.Second},
		{Kind: InterruptMuted, Reason: "streamVolume", From: 3 * time.Second, To: 4 * time.Second},
		{Kind: InterruptNavigation, Usage: "USAGE_ASSISTANCE_NAVIGATION_GUIDANCE", From: 4500 * time.Millisecond, To: 5 * time.Second, Open: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
