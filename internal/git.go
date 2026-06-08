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

func (g *GitClient) Checkout(branch string) error {
	_, err := g.run("checkout", branch)
	return err
}

func (g *GitClient) RevParse(ref string) (string, error) {
	return g.run("rev-parse", ref)
}
