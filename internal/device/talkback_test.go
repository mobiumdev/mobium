package device

import (
	"strings"
	"testing"
)

// Lines as the Android 15 emulator logged them, with TalkBack's log output
// level at VERBOSE, while a keyboard walked MobiumApp's Form Demo; the hint
// is made up, to hold an utterance said in two fragments.
const talkBackLog = `         1792599100.668  3147  3147 V talkback: SpeechControllerImpl: Speaking fragment text="Form", utteranceId=talkback_4, spans=null, event=type:EVENT_TYPE_ACCESSIBILITY subtype:TYPE_VIEW_ACCESSIBILITY_FOCUSED displayId:0 time:188164
         1792599101.944  3147  3147 V talkback: SpeechControllerImpl: Speaking fragment text="not checked. Email me. Check box", utteranceId=talkback_5, spans=null, event=type:EVENT_TYPE_ACCESSIBILITY subtype:TYPE_VIEW_ACCESSIBILITY_FOCUSED displayId:0 time:189452
         1792599101.945  3147  3147 V talkback: SpeechControllerImpl: Speaking fragment text="Double-tap to toggle", utteranceId=talkback_5, spans=null, event=type:EVENT_TYPE_ACCESSIBILITY subtype:TYPE_VIEW_ACCESSIBILITY_FOCUSED displayId:0 time:189452
         1792599103.264  3147  3147 V talkback: SpeechControllerImpl: Speaking fragment text="Saved", utteranceId=talkback_6, spans=null, event=type:EVENT_TYPE_ACCESSIBILITY subtype:TYPE_ANNOUNCEMENT displayId:0 time:190772
         1792599120.333  3147  3147 V talkback: SpeechControllerImpl: Speaking fragment text="TalkBack off", utteranceId=talkback_17, spans=null, event=null
--------- beginning of main
         1792599509.528  3147  3147 V talkback: SpeechControllerImpl: Speaking fragment text="Form", utteranceId=talkback_4, spans=null, event=type:EVENT_TYPE_ACCESSIBILITY subtype:TYPE_VIEW_ACCESSIBILITY_FOCUSED displayId:0 time:288164
`

func TestTalkBackSpokenReadsEachUtterance(t *testing.T) {
	got, err := parseTalkBackSpoken([]byte(strings.ReplaceAll(talkBackLog, "\n", "\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("%d utterances, want 5: %+v", len(got), got)
	}
	if got[1].Text != "not checked. Email me. Check box Double-tap to toggle" || !got[1].Focus || got[1].ID != 5 {
		t.Errorf("an utterance's fragments are not joined: %+v", got[1])
	}
	if got[2].Focus {
		t.Errorf("an announcement read as a move of the cursor: %+v", got[2])
	}
	if got[3].Focus || got[3].ID != 17 {
		t.Errorf("a line with no event: %+v", got[3])
	}
	// TalkBack turned on again numbers from 1 in the same process, 3147: its
	// talkback_4 is a new utterance, not the last time's "Form".
	if got[4].Key() == got[0].Key() {
		t.Errorf("a restarted TalkBack's utterance was taken for the last one's: %+v and %+v", got[4], got[0])
	}
}

// At the default log level TalkBack logs no speech, and logcat's -e leaves
// nothing: none read, which the tool takes for TalkBack being quiet.
func TestTalkBackSpokenEmptyIsNone(t *testing.T) {
	got, err := parseTalkBackSpoken([]byte("--------- beginning of main\n"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want none", got, err)
	}
}
