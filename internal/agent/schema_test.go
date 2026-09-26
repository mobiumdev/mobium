package agent

import "testing"

// Every argument that names a file on the caller's machine is resolved in the
// caller's directory before the call leaves it; one missing from
// PathArguments would be resolved in the daemon's, which is wherever it
// started — `screenshot -o rel.png` once saved into another directory.
func TestEveryPathArgumentIsResolvedByTheCaller(t *testing.T) {
	pathish := map[string]bool{"path": true, "gpx": true, "file": true, "output": true}
	for _, tool := range GetToolSchemas() {
		props, _ := tool.InputSchema["properties"].(map[string]interface{})
		for name := range props {
			if !pathish[name] {
				continue
			}
			found := false
			for _, p := range PathArguments[tool.Name] {
				found = found || p == name
			}
			if !found {
				t.Errorf("%s.%s names a file and is not in PathArguments", tool.Name, name)
			}
		}
	}
}
