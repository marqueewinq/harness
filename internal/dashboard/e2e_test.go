package dashboard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildHarnessBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "harness")
	out, err := exec.Command("go", "build", "-o", bin, "../..").CombinedOutput()
	if err != nil {
		t.Fatalf("build harness binary: %s\n%s", err, out)
	}
	return bin
}

func runHarness(t *testing.T, bin, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestE2EDashboardOnce(t *testing.T) {
	bin := buildHarnessBinary(t)
	dir := t.TempDir()
	gitOut(t, dir, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README"), []byte("x\n"), 0644)
	gitOut(t, dir, "add", ".")
	gitOut(t, dir, "commit", "-q", "-m", "init")
	if out, err := runHarness(t, bin, dir, "init"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	gitOut(t, dir, "add", ".")
	gitOut(t, dir, "commit", "-q", "-m", "harness")
	if out, err := runHarness(t, bin, dir, "new", "dashboard demo"); err != nil {
		t.Fatalf("new: %v\n%s", err, out)
	}

	out, err := runHarness(t, bin, dir, "dashboard", "--once")
	if err != nil {
		t.Fatalf("dashboard --once: %v\n%s", err, out)
	}
	head := gitOut(t, dir, "rev-parse", "--short", "main")
	for _, want := range []string{"main @ " + head, "AWAITING REVIEW", "IN FLIGHT", "No epic", "task-001", "IN_PROGRESS", "dashboard demo", "MERGE QUEUE", "RECENT (24h)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("non-terminal output has ANSI escapes:\n%q", out)
	}

	out, err = runHarness(t, bin, dir, "dashboard")
	if err == nil || !strings.Contains(out, "use --once") {
		t.Errorf("interactive dashboard: err=%v out=%s, want non-zero exit pointing at --once", err, out)
	}
}
