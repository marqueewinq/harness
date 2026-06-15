package internal

import (
	"fmt"
	"strings"
)

// CountDone counts tasks in terminal states (MERGED or CLOSED).
func CountDone(tasks []Task) (done, total int) {
	total = len(tasks)
	for _, t := range tasks {
		if t.State == StateMerged || t.State == StateClosed {
			done++
		}
	}
	return
}

// PrintCheckResults prints check results and returns whether all passed.
func PrintCheckResults(results []CheckResult) bool {
	allPassed := true
	for _, r := range results {
		if r.Skipped {
			fmt.Printf("  %-12s SKIP\n", r.Name)
		} else if r.ExitCode == 0 {
			fmt.Printf("  %-12s PASS  (%.1fs)\n", r.Name, r.Duration.Seconds())
		} else {
			fmt.Printf("  %-12s FAIL  (%.1fs)\n", r.Name, r.Duration.Seconds())
			allPassed = false
		}
	}
	return allPassed
}

// FormatPreviousComments formats review history into a string for prompts.
func FormatPreviousComments(reviews []Review) string {
	var parts []string
	for _, r := range reviews {
		if r.Comment != "" {
			parts = append(parts, fmt.Sprintf("[%s] %s", r.Decision, r.Comment))
		}
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, "\n  ")
}
