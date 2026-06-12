package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type GitClient struct {
	dir string
}

func NewGitClient(dir string) *GitClient {
	return &GitClient{dir: dir}
}

func (g *GitClient) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=harness", "GIT_AUTHOR_EMAIL=harness@local", "GIT_COMMITTER_NAME=harness", "GIT_COMMITTER_EMAIL=harness@local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *GitClient) CreateBranch(name, from string) error {
	_, err := g.run("branch", name, from)
	return err
}

func (g *GitClient) DeleteBranch(name string) error {
	_, err := g.run("branch", "-D", name)
	return err
}

func (g *GitClient) CreateWorktree(branch, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	_, err := g.run("worktree", "add", path, branch)
	return err
}

func (g *GitClient) RemoveWorktree(path string) error {
	_, err := g.run("worktree", "remove", path, "--force")
	return err
}

func (g *GitClient) Diff(base, head string) (string, error) {
	return g.run("diff", base+"..."+head)
}

func (g *GitClient) CurrentBranch() (string, error) {
	return g.run("rev-parse", "--abbrev-ref", "HEAD")
}

func (g *GitClient) Rebase(onto string) error {
	_, err := g.run("rebase", onto)
	return err
}

func (g *GitClient) RebaseAbort() error {
	_, err := g.run("rebase", "--abort")
	return err
}

func (g *GitClient) MergeFF(branch string) error {
	_, err := g.run("merge", "--ff-only", branch)
	return err
}

func (g *GitClient) MergeSquash(branch, message string) error {
	if _, err := g.run("merge", "--squash", branch); err != nil {
		return err
	}
	_, err := g.run("commit", "-m", message)
	return err
}

func (g *GitClient) Commit(message string, extraArgs ...string) error {
	args := append([]string{"commit", "-m", message}, extraArgs...)
	_, err := g.run(args...)
	return err
}

func (g *GitClient) Checkout(branch string) error {
	_, err := g.run("checkout", branch)
	return err
}

func (g *GitClient) RevParse(ref string) (string, error) {
	return g.run("rev-parse", ref)
}

func (g *GitClient) RevParseShort(ref string) (string, error) {
	return g.run("rev-parse", "--short", ref)
}

func (g *GitClient) MergeBase(a, b string) (string, error) {
	return g.run("merge-base", a, b)
}

// DefaultBranch detects the repo's default branch from HEAD of origin,
// or falls back to checking common names, or the current branch.
func (g *GitClient) DefaultBranch() (string, error) {
	// Try origin HEAD (works if a remote is configured)
	if out, err := g.run("symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		// refs/remotes/origin/main -> main
		parts := strings.Split(strings.TrimSpace(out), "/")
		if len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// Try common default branch names
	for _, name := range []string{"main", "master"} {
		if _, err := g.run("rev-parse", "--verify", name); err == nil {
			return name, nil
		}
	}

	// Fall back to current branch
	return g.CurrentBranch()
}
