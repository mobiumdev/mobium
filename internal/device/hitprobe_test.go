package device

import (
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// lldb's own output around the probe's answer, as it printed it on an iPhone
// 17 Pro simulator, with the answer swapped for each verdict.
func lldbSaid(answer string) []byte {
	return []byte("Executing commands in '...'.\n" +
		"(lldb) expr void *$mobium_probe = (void *)dlopen(\"/x/probe.dylib\", 2)\n" +
		"(lldb) expr -- (const char *)((...)dlsym($mobium_probe, \"mobium_hit_0\"))(201, 760, 16, 736, 370, 48, \"hiddenTarget\")\n" +
		"(const char *) $0 = 0x0000000118c16ea0 " + answer + "\n" +
		"(lldb) detach\nProcess 24456 detached\n")
}

func TestParseHitAnswer(t *testing.T) {
	h, err := parseHitAnswer(lldbSaid(`"covered\t1\tRCTParagraphComponentView\thidden overlay\t156.7\t752.0\t88.3\t15.7"`))
	if err != nil || h.Verdict != "covered" || !h.Hidden || h.Label != "hidden overlay" || h.Frame[2] != 88.3 {
		t.Errorf("covered: %+v, %v", h, err)
	}
	if h, err := parseHitAnswer(lldbSaid(`"reaches"`)); err != nil || h.Verdict != "reaches" {
		t.Errorf("reaches: %+v, %v", h, err)
	}
	if h, err := parseHitAnswer(lldbSaid(`"unknown\tno view in the app is that element"`)); err != nil || h.Reason == "" {
		t.Errorf("unknown: %+v, %v", h, err)
	}
	// A label with a quote in it arrives escaped.
	h, err = parseHitAnswer(lldbSaid(`"covered\t0\tUILabel\tsay \"hi\"\t0\t0\t1\t1"`))
	if err != nil || h.Label != `say "hi"` {
		t.Errorf("escaped: %+v, %v", h, err)
	}
}

// No answer is an error that quotes lldb's, never an empty verdict.
func TestParseHitAnswerWithoutOne(t *testing.T) {
	_, err := parseHitAnswer([]byte("error: attach failed: Operation not permitted\n"))
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceServer || !strings.Contains(err.Error(), "Operation not permitted") {
		t.Errorf("no answer: %v", err)
	}
	if _, err := parseHitAnswer(lldbSaid(`"covered\t1"`)); err == nil {
		t.Error("a short answer was taken for one")
	}
}

// The probe's symbol changes with its source, so a library an older mobium
// loaded into an app is never the one called.
func TestHitProbeSymbolFollowsTheSource(t *testing.T) {
	was := hitProbeSymbol()
	saved := hitProbeSource
	defer func() { hitProbeSource = saved }()
	hitProbeSource = append(append([]byte{}, saved...), '\n')
	if hitProbeSymbol() == was {
		t.Error("a changed probe kept its symbol")
	}
	if !strings.HasPrefix(was, "mobium_hit_") {
		t.Errorf("symbol %q", was)
	}
}

func TestAppPIDFromLaunchctl(t *testing.T) {
	list := "PID\tStatus\tLabel\n" +
		"24400\t0\tUIKitApplication:dev.mobium.mobiumappx[1a2b][rb-legacy]\n" +
		"24456\t0\tUIKitApplication:dev.mobium.mobiumapp[b1f2][rb-legacy]\n"
	m := appPIDRe("dev.mobium.mobiumapp").FindStringSubmatch(list)
	if m == nil || m[1] != "24456" {
		t.Errorf("pid: %v", m)
	}
}
