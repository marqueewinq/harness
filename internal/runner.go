package internal

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

type CheckResult struct {
	Name       string
	ExitCode   int
	Stdout     string
	Stderr     string
	Skipped    bool
	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration
}

type CheckRunner struct {
	checks map[string]CheckConfig
	dir    string
}

func NewCheckRunner(checks map[string]CheckConfig, dir string) *CheckRunner {
	return &CheckRunner{checks: checks, dir: dir}
}

func (cr *CheckRunner) RunAll() ([]CheckResult, error) {
	order, err := TopoSort(cr.checks)
	if err != nil {
		return nil, fmt.Errorf("invalid check DAG: %w", err)
	}

	completed := map[string]bool{}
	failed := map[string]bool{}
	results := map[string]CheckResult{}
	var mu sync.Mutex

	for len(completed)+len(failed) < len(order) {
		done := make(map[string]bool, len(completed)+len(failed))
		for k := range completed {
			done[k] = true
		}
		for k := range failed {
			done[k] = true
		}
		ready := ReadyChecks(cr.checks, done)
		if len(ready) == 0 {
			for _, name := range order {
				if !completed[name] && !failed[name] {
					if _, exists := results[name]; !exists {
						results[name] = CheckResult{Name: name, Skipped: true}
						failed[name] = true
					}
				}
			}
			break
		}

		var runnable []string
		for _, name := range ready {
			hasFailed := false
			for _, dep := range cr.checks[name].Needs {
				if failed[dep] {
					hasFailed = true
					break
				}
			}
			if hasFailed {
				results[name] = CheckResult{Name: name, Skipped: true}
				failed[name] = true
			} else {
				runnable = append(runnable, name)
			}
		}

		if len(runnable) == 0 {
			continue
		}

		var wg sync.WaitGroup
		for _, name := range runnable {
			wg.Add(1)
			go func(n string) {
				defer wg.Done()
				result := cr.runOne(n)
				mu.Lock()
				results[n] = result
				if result.ExitCode == 0 {
					completed[n] = true
				} else {
					failed[n] = true
				}
				mu.Unlock()
			}(name)
		}
		wg.Wait()
	}

	var ordered []CheckResult
	for _, name := range order {
		if r, ok := results[name]; ok {
			ordered = append(ordered, r)
		}
	}
	return ordered, nil
}

func (cr *CheckRunner) runOne(name string) CheckResult {
	check := cr.checks[name]
	start := time.Now()

	cmd := exec.Command("sh", "-c", check.Run)
	cmd.Dir = cr.dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	finish := time.Now()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	return CheckResult{
		Name:       name,
		ExitCode:   exitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		StartedAt:  start,
		FinishedAt: finish,
		Duration:   finish.Sub(start),
	}
}
