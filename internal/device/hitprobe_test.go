package device

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
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

// A probe loaded at launch is asked over its socket: the seven fields out,
// its answer back. With nothing listening the app has no probe, which is
// not an error — the action goes on without the question.
func TestLoadedHitTest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a simulator runs only on macOS")
	}
	// A short MOBIUM_HOME, since the socket's path is capped at 103 bytes and
	// a test's own temporary directory is long on macOS.
	home, err := os.MkdirTemp("/tmp", "mh")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(home) }()
	t.Setenv("MOBIUM_HOME", home)
	s := &Simctl{UDID: "457C7DC2-C706-45D9-8D68-1D26953E28B1"}

	if _, loaded, err := s.LoadedHitTest(context.Background(), "dev.mobium.mobiumapp", 1, 2, [4]float64{0, 0, 10, 10}, "x"); loaded || err != nil {
		t.Fatalf("with no probe: loaded %v, err %v", loaded, err)
	}

	sock, err := s.hitProbeSocket("dev.mobium.mobiumapp")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	asked := make(chan string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		line, _ := bufio.NewReader(c).ReadString('\n')
		asked <- line
		_, _ = c.Write([]byte("covered\t1\tRCTView\thidden overlay\t1\t2\t3\t4\n"))
	}()
	h, loaded, err := s.LoadedHitTest(context.Background(), "dev.mobium.mobiumapp", 201, 760.5, [4]float64{16, 736, 370, 48}, "hidden\tTarget")
	if !loaded || err != nil {
		t.Fatalf("loaded %v, err %v", loaded, err)
	}
	if q := <-asked; q != "201\t760.5\t16\t736\t370\t48\thidden Target\n" {
		t.Errorf("asked %q", q)
	}
	if h.Verdict != "covered" || !h.Hidden || h.Label != "hidden overlay" || h.Frame != [4]float64{1, 2, 3, 4} {
		t.Errorf("read %+v", h)
	}
}

// A MOBIUM_HOME too long for a socket is refused with a remedy, rather than
// failing inside the app where nobody sees it.
func TestHitProbeSocketTooLong(t *testing.T) {
	t.Setenv("MOBIUM_HOME", "/"+strings.Repeat("d", 100))
	_, err := (&Simctl{UDID: "u"}).hitProbeSocket("a")
	if err == nil || !strings.Contains(err.Error(), "MOBIUM_HOME") {
		t.Fatalf("got %v", err)
	}
}
