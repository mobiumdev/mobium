//go:build !windows

package device

import (
	"os/exec"
	"strconv"
	"strings"
)

// processPath is the program a process runs, as ps names it.
func processPath(pid int) (string, error) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	return strings.TrimSpace(string(out)), err
}
