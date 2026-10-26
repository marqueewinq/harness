package dashboard

import (
	"os"
	"strings"
	"testing"
)

// requiredVersion finds module's version in go.mod's require lines, in or
// outside a require block, ignoring trailing comments.
func requiredVersion(gomod, module string) string {
	for _, line := range strings.Split(gomod, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "require" {
			fields = fields[1:]
		}
		if len(fields) == 2 && fields[0] == module {
			return fields[1]
		}
	}
	return ""
}

func TestRequiredVersion(t *testing.T) {
	gomod := "module x\n\nrequire a/b v1.0.0\n\nrequire (\n\tgithub.com/mattn/go-runewidth v0.0.16 // indirect\n\tc/d v2.0.0\n)\n"
	for module, want := range map[string]string{"a/b": "v1.0.0", "github.com/mattn/go-runewidth": "v0.0.16", "c/d": "v2.0.0", "e/f": ""} {
		if got := requiredVersion(gomod, module); got != want {
			t.Errorf("%s = %q, want %q", module, got, want)
		}
	}
}

// go-runewidth v0.0.27 builds a 2.2MB table at init, ~300ms on every harness
// command; the Charm modules are pinned to Bubble Tea v1.3.10's own versions.
func TestRunewidthPinned(t *testing.T) {
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if got := requiredVersion(string(data), "github.com/mattn/go-runewidth"); got != "v0.0.16" {
		t.Errorf("go-runewidth is %q, want v0.0.16; check harness startup time before upgrading (see README, Development)", got)
	}
}
