package agent

import (
	"os"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// loadLauncher parses the verbatim emulator dump kept in the uitree package,
// so the tool layer is exercised against a real screen rather than a mock.
func loadLauncher(t *testing.T) *uitree.Tree {
	t.Helper()
	data, err := os.ReadFile("../uitree/testdata/launcher.xml")
	if err != nil {
		t.Fatalf("read launcher fixture: %v", err)
	}
	tree, err := uitree.ParseAndroid(data)
	if err != nil {
		t.Fatalf("parse launcher fixture: %v", err)
	}
	return tree
}
