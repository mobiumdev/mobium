package fakecmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The launcher has to carry arguments, output and the exit status across,
// or every fake built on it lies in the same way.
func TestScriptPassesArgumentsOutputAndStatus(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	fake := Script(t, dir, "adb", `echo "$@" >> `+Path(log)+`
echo "out:$2"
echo "err:$1" >&2
exit 3
`)
	cmd := exec.Command(fake, "one", "two words", "it's $HOME 100%")
	var stdout, stderr []byte
	out, err := cmd.Output()
	stdout = out
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("exit = %v, want status 3", err)
	}
	stderr = exit.Stderr
	if string(stdout) != "out:two words\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if string(stderr) != "err:one\n" {
		t.Errorf("stderr = %q", stderr)
	}
	// Spaces, an apostrophe, a dollar and a percent: what adb shell quoting
	// has to survive, and what a command line on Windows is easiest to lose.
	if got, _ := os.ReadFile(log); string(got) != "one two words it's $HOME 100%\n" {
		t.Errorf("arguments arrived as %q", got)
	}
}
