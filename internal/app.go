package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// App holds all dependencies for command execution.
type App struct {
	Config   *Config
	DB       *DB
	Git      *GitClient
	RepoRoot string
}

// FindRepoRoot walks up from cwd to find .harness.yaml.
func FindRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".harness.yaml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .harness.yaml found (walked up from cwd)")
		}
		dir = parent
	}
}

// LoadApp creates an App from the repo root.
func LoadApp(repoRoot string) (*App, error) {
	cfg, err := LoadConfig(filepath.Join(repoRoot, ".harness.yaml"))
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(repoRoot, ".harness", "harness.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, err
	}
	git := NewGitClient(repoRoot)
	return &App{Config: cfg, DB: db, Git: git, RepoRoot: repoRoot}, nil
}

func (a *App) Close() {
	if a.DB != nil {
		a.DB.Close()
	}
}

func Slugify(s string) string {
	s = strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

func (a *App) ResolveTaskID(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	branch, err := a.Git.CurrentBranch()
	if err != nil {
		return "", fmt.Errorf("cannot detect task: %w", err)
	}
	task, err := a.DB.GetTaskByBranch(branch)
	if err != nil {
		return "", fmt.Errorf("no task found for branch %q — pass task ID explicitly", branch)
	}
	return task.ID, nil
}
