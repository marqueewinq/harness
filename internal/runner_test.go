package internal

import (
	"testing"
)

func TestRunnerAllPass(t *testing.T) {
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
