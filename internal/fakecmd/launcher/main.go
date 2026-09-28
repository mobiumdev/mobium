// Command launcher is fakecmd's stand-in executable on Windows: it runs the
// .sh file that shares its name under sh, passing on its arguments, streams
// and exit status.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecmd launcher:", err)
		os.Exit(127)
	}
	script := filepath.ToSlash(strings.TrimSuffix(self, filepath.Ext(self)) + ".sh")
	cmd := exec.Command("sh", append([]string{script}, os.Args[1:]...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "fakecmd launcher:", err)
		os.Exit(127)
	}
}
