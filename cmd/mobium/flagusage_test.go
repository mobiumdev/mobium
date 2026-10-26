package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cobra takes the first `quoted` word of a flag's usage for its value's
// name, so "the .zip `mobium trace stop` saved" printed as
// "-i, --input mobium trace stop" in check's help, and the documentation
// site read that line as a flag it could not name.
func TestNoFlagUsageQuotesInBackticks(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no Go files read: %v", err)
	}
	flag := regexp.MustCompile(`Flags\(\)\.\w+\(.*"[^"]*` + "`" + `[^"]*"\)`)
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "Flags().") {
				seen++
			}
			if flag.MatchString(line) {
				t.Errorf("%s:%d: a flag's usage has backticks, which cobra prints as its value's name", f, i+1)
			}
		}
	}
	if seen < 50 {
		t.Fatalf("read only %d flag definitions; the pattern no longer finds them", seen)
	}
}
