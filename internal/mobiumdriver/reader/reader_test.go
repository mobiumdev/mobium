package reader

import (
	"errors"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

func TestDexMatchesItsPinnedChecksum(t *testing.T) {
	b, err := Dex()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 1000 || string(b[:4]) != "dex\n" {
		t.Fatalf("reader.dex is not a dex file: %d bytes, starts %q", len(b), b[:4])
	}
}

// A Fire TV's libraries print a line on stdout before anything else.
func TestHierarchyIgnoresWhatCameBefore(t *testing.T) {
	out := "Init wrapper sys mutex successful. Pid:1029\n" + begin + "\n<hierarchy rotation=\"0\"></hierarchy>\n" + end + "\n"
	got, err := Hierarchy([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `<hierarchy rotation="0"></hierarchy>` {
		t.Fatalf("got %q", got)
	}
}

func TestHierarchyReportsTheReadersError(t *testing.T) {
	_, err := Hierarchy([]byte(errorMark + "IllegalStateException: no active window from UiAutomation in 10s\n"))
	var me *mobiumerr.Error
	if !errors.As(err, &me) || me.Code != mobiumerr.DeviceServer {
		t.Fatalf("want device_server, got %v", err)
	}
	if !strings.Contains(err.Error(), "no active window") {
		t.Fatalf("the reader's reason is lost: %v", err)
	}
}

func TestHierarchyRefusesOutputWithoutMarkers(t *testing.T) {
	for _, out := range []string{"", "Segmentation fault\n", begin + "\n", begin + "\n\n" + end} {
		if _, err := Hierarchy([]byte(out)); err == nil {
			t.Errorf("%q was taken as a hierarchy", out)
		}
	}
}
