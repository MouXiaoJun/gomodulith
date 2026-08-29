package modulith

import (
	"os"
	"path/filepath"
)

// readGoMod returns the contents of the nearest go.mod file by walking up from
// dir, or "" when none is found.
func readGoMod(dir string) string {
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			return string(data)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
