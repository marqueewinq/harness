package internal

import (
	"testing"
)

func TestRunnerAllPass(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"step1": {Run: "echo hello"},
		"step2": {Run: "echo world", Needs: []string{"step1"}},
	}
	runner := NewCheckRunner(checks, t.TempDir())
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.ExitCode != 0 {
			t.Errorf("check %q failed: exit %d, stderr: %s", r.Name, r.ExitCode, r.Stderr)
		}
	}
}

func TestRunnerFailure(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"pass": {Run: "echo ok"},
		"fail": {Run: "exit 1", Needs: []string{"pass"}},
		"skip": {Run: "echo should-not-run", Needs: []string{"fail"}},
	}
	runner := NewCheckRunner(checks, t.TempDir())
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}

	resultMap := map[string]CheckResult{}
	for _, r := range results {
		resultMap[r.Name] = r
	}

	if resultMap["pass"].ExitCode != 0 {
		t.Error("pass should have exit 0")
	}
	if resultMap["fail"].ExitCode == 0 {
		t.Error("fail should have non-zero exit")
	}
	if !resultMap["skip"].Skipped {
		t.Error("skip should be skipped")
	}
}

func TestRunnerParallel(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"a": {Run: "echo a"},
		"b": {Run: "echo b"},
	}
	runner := NewCheckRunner(checks, t.TempDir())
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.ExitCode != 0 {
			t.Errorf("check %q failed", r.Name)
		}
	}
}

func TestRunnerEmpty(t *testing.T) {
	t.Parallel()
	runner := NewCheckRunner(map[string]CheckConfig{}, t.TempDir())
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
}

func TestRunnerSingleFail(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"bad": {Run: "exit 42"},
	}
	runner := NewCheckRunner(checks, t.TempDir())
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
}

func TestRunnerDiamond(t *testing.T) {
	t.Parallel()
	// Diamond DAG: a -> b,c -> d
	checks := map[string]CheckConfig{
		"a": {Run: "echo a"},
		"b": {Run: "echo b", Needs: []string{"a"}},
		"c": {Run: "echo c", Needs: []string{"a"}},
		"d": {Run: "echo d", Needs: []string{"b", "c"}},
	}
	runner := NewCheckRunner(checks, t.TempDir())
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("len(results) = %d, want 4", len(results))
	}
	for _, r := range results {
		if r.ExitCode != 0 {
			t.Errorf("check %q failed with exit %d", r.Name, r.ExitCode)
		}
	}
}

func TestRunnerStderrCapture(t *testing.T) {
	t.Parallel()
	checks := map[string]CheckConfig{
		"noisy": {Run: "echo error-output >&2"},
	}
	runner := NewCheckRunner(checks, t.TempDir())
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Stderr == "" {
		t.Error("expected stderr to be captured")
	}
	if results[0].ExitCode != 0 {
		t.Errorf("exit code = %d, want 0 (echo to stderr succeeds)", results[0].ExitCode)
	}
}
