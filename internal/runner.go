package internal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
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
	// LogDir, if set, receives <check>.log with live combined output.
	LogDir string
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

// TimeoutExitCode mirrors coreutils timeout(1).
const TimeoutExitCode = 124

func (cr *CheckRunner) runOne(name string) CheckResult {
	check := cr.checks[name]
	timeout := check.TimeoutDuration()
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", check.Run)
	cmd.Dir = cr.dir
	// Checks spawn grandchildren (make -> go test); kill the whole process
	// group on timeout, and stop waiting on pipes they may still hold.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second

	var stdout, stderr bytes.Buffer
	var outW, errW io.Writer = &stdout, &stderr
	var logNote string
	if cr.LogDir != "" {
		if err := os.MkdirAll(cr.LogDir, 0755); err == nil {
			if f, err := os.Create(filepath.Join(cr.LogDir, name+".log")); err == nil {
				defer f.Close()
				outW = io.MultiWriter(&stdout, f)
				errW = io.MultiWriter(&stderr, f)
			} else {
				logNote = fmt.Sprintf("harness: could not create log file: %v\n", err)
			}
		}
	}
	cmd.Stdout = outW
	cmd.Stderr = errW

	err := cmd.Run()
	finish := time.Now()

	exitCode := 0
	if ctx.Err() == context.DeadlineExceeded {
		exitCode = TimeoutExitCode
		fmt.Fprintf(errW, "\nharness: check %q timed out after %s\n", name, timeout)
	} else if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
			fmt.Fprintf(errW, "\nharness: %v\n", err)
		}
	}

	return CheckResult{
		Name:       name,
		ExitCode:   exitCode,
		Stdout:     stdout.String(),
		Stderr:     logNote + stderr.String(),
		StartedAt:  start,
		FinishedAt: finish,
		Duration:   finish.Sub(start),
	}
}
