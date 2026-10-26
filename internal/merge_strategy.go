package internal

import "fmt"

// MergeStrategy defines how a task branch is integrated into the target branch.
type MergeStrategy interface {
	// Name returns the strategy name for display.
	Name() string

	// Prepare runs before the merge on the worktree (e.g., rebase).
	// Returns nil if no preparation is needed.
	Prepare(wtGit *GitClient, targetBranch string) error

	// Merge runs in a worktree that has targetBranch checked out.
	Merge(repoGit *GitClient, taskBranch, targetBranch, description string) error
}

// NewMergeStrategy creates a MergeStrategy from a config string.
func NewMergeStrategy(name string) (MergeStrategy, error) {
	switch name {
	case "merge", "":
		return &RebaseMergeStrategy{}, nil
	case "squash":
		return &SquashMergeStrategy{}, nil
	default:
		return nil, fmt.Errorf("unknown merge strategy %q (supported: merge, squash)", name)
	}
}

// RebaseMergeStrategy rebases the task branch onto the target, then fast-forward merges.
// This preserves individual commits and produces a linear history.
type RebaseMergeStrategy struct{}

func (s *RebaseMergeStrategy) Name() string { return "rebase+ff" }

func (s *RebaseMergeStrategy) Prepare(wtGit *GitClient, targetBranch string) error {
	return wtGit.Rebase(targetBranch)
}

func (s *RebaseMergeStrategy) Merge(repoGit *GitClient, taskBranch, targetBranch, description string) error {
	return repoGit.MergeFF(taskBranch)
}

// SquashMergeStrategy squashes all task commits into a single commit on the target branch.
type SquashMergeStrategy struct{}

func (s *SquashMergeStrategy) Name() string { return "squash" }

func (s *SquashMergeStrategy) Prepare(wtGit *GitClient, targetBranch string) error {
	// No rebase needed for squash -- git merge --squash handles divergence
	return nil
}

func (s *SquashMergeStrategy) Merge(repoGit *GitClient, taskBranch, targetBranch, description string) error {
	return repoGit.MergeSquash(taskBranch, description)
}
