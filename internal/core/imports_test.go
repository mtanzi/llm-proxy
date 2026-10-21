package core

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportsStandardLibraryOnly fails if core, its tests or anything they
// depend on, directly or transitively, is outside the standard library. It
// asks the go tool, which knows which packages are standard. The coretest
// subpackage is checked by depguard.
func TestImportsStandardLibraryOnly(t *testing.T) {
	const self = "github.com/mtanzi/llm-proxy/internal/core"
	out, err := exec.Command("go", "list", "-deps", "-test", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path, _, _ := strings.Cut(line, " [")
		if path != self && path != self+".test" {
			t.Errorf("core depends on %q: core may import the standard library only", path)
		}
	}
}
