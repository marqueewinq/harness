package dashboard

import (
	"os"
	"strings"
	"testing"
)

// go-runewidth v0.0.27 builds a 2.2MB table at init, ~300ms on every harness
// command; the Charm modules are pinned to Bubble Tea v1.3.10's own versions.
func TestRunewidthPinned(t *testing.T) {
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "github.com/mattn/go-runewidth v0.0.16 ") {
		t.Error("go-runewidth moved off v0.0.16; check harness startup time before upgrading (see README, Development)")
	}
}
