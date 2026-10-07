package device

import "testing"

// As the Android 15 emulator printed it, with the streams around them.
const dumpsysAudioStreams = `Stream volumes (device: index)
- STREAM_VOICE_CALL:
   Muted: false
   Min: 1
   Max: 5
   streamVolume:4
- STREAM_MUSIC:
   Muted: false
   Muted Internally: false
   Min: 0
   Max: 15
   streamVolume:5
   Current: 2 (speaker): 5, 80 (bt_a2dp): 2, 40000000 (default): 5
   Devices: speaker(2)
   Volume Group: AUDIO_STREAM_MUSIC
- STREAM_ALARM:
   Muted: true
   Muted Internally: false
   Min: 1
   Max: 7
   streamVolume:6
   Current: 2 (speaker): 6, 40000000 (default): 6
   Devices: speaker(2)
   Volume Group: AUDIO_STREAM_ALARM
- STREAM_NOTIFICATION:
   Muted: false
   Min: 0
   Max: 7
   streamVolume:5
`

func TestParseStreamVolumes(t *testing.T) {
	got := parseStreamVolumes([]byte(dumpsysAudioStreams))
	want := []StreamVolume{
		{Stream: "media", Index: 5, Min: 0, Max: 15},
		{Stream: "alarm", Index: 6, Min: 1, Max: 7, Muted: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stream %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A dump laid out otherwise gives nothing rather than a zero volume, which
// would read as "turned all the way down".
func TestParseStreamVolumesWithoutAnIndex(t *testing.T) {
	if got := parseStreamVolumes([]byte("- STREAM_MUSIC:\n   Max: 15\n   Current: 5\n")); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
