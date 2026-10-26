package internal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunnerTimeoutKillsProcessGroup(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		// The grandchild sleep keeps stdout open; without a group kill and
		// WaitDelay, Run would block until it exits.
		"hang": {Run: "sh -c 'sleep 30' & sleep 30", Timeout: "300ms"},
	}
	runner := NewCheckRunner(checks, t.TempDir())
	start := time.Now()
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("timeout took %s", elapsed)
	}
	if results[0].ExitCode != TimeoutExitCode {
		t.Errorf("exit = %d, want %d", results[0].ExitCode, TimeoutExitCode)
	}
	if !strings.Contains(results[0].Stderr, "timed out") {
		t.Errorf("stderr = %q, want timeout note", results[0].Stderr)
	}
}

func TestRunnerInterruptKillsCheck(t *testing.T) {
	t.Parallel()
	runner := NewCheckRunner(map[string]CheckConfig{
		"hang": {Run: "sh -c 'sleep 30' & sleep 30"},
	}, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	r := runner.runOne(ctx, "hang")
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("interrupt took %s", elapsed)
	}
	if r.ExitCode != InterruptedExitCode {
		t.Errorf("exit = %d, want %d", r.ExitCode, InterruptedExitCode)
	}
}

func TestRunnerBackgroundChildDoesNotFailCheck(t *testing.T) {
	t.Parallel()
	runner := NewCheckRunner(map[string]CheckConfig{
		"bg": {Run: "sleep 30 & echo done"},
	}, t.TempDir())
	r := runner.runOne(context.Background(), "bg")
	if r.ExitCode != 0 {
		t.Errorf("exit = %d, want 0; stderr=%q", r.ExitCode, r.Stderr)
	}
}

func TestRunnerWritesLogs(t *testing.T) {
	t.Parallel()
	logDir := filepath.Join(t.TempDir(), "logs")
	runner := NewCheckRunner(map[string]CheckConfig{
		"lint": {Run: "echo out; echo err >&2"},
	}, t.TempDir())
	runner.LogDir = logDir
	if _, err := runner.RunAll(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(logDir, "lint.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "out") || !strings.Contains(string(data), "err") {
		t.Errorf("log = %q, want stdout and stderr", data)
	}
}

func TestInvalidCheckTimeoutRejected(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".harness.yaml")
	if err := os.WriteFile(path, []byte("checks:\n  test:\n    run: make test\n    timeout: soon\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "checks.test.timeout") {
		t.Errorf("err = %v, want invalid timeout error", err)
	}
}
