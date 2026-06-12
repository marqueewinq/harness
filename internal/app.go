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
	harnessDir := filepath.Join(repoRoot, ".harness")
	if err := os.MkdirAll(harnessDir, 0755); err != nil {
		return nil, fmt.Errorf("creating .harness/: %w", err)
	}
	dbPath := filepath.Join(harnessDir, "harness.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, err
	}
	git := NewGitClient(repoRoot)

	// Resolve main branch from git if not set in config
	if cfg.MainBranch == "" {
		branch, err := git.DefaultBranch()
		if err != nil {
			cfg.MainBranch = "main" // last resort fallback
		} else {
			cfg.MainBranch = branch
		}
	}

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

func isEpicID(id string) bool {
	return strings.HasPrefix(id, "epic-")
}

var emptyBrackets = regexp.MustCompile(`\[\s*\]\s*`)

// BuildCommitMessage renders a commit message template with task hierarchy IDs.
// Empty variables cause their surrounding [...] blocks to be stripped before
// {{message}} is substituted, so literal [] in user messages are preserved.
func BuildCommitMessage(tmpl, taskID, epicID, projID, message string) string {
	s := tmpl
	s = strings.ReplaceAll(s, "{{task}}", taskID)
	s = strings.ReplaceAll(s, "{{epic}}", epicID)
	s = strings.ReplaceAll(s, "{{proj}}", projID)

	// Strip empty bracket blocks before message substitution
	// so [] in user's message text is preserved.
	s = emptyBrackets.ReplaceAllString(s, "")

	s = strings.ReplaceAll(s, "{{message}}", message)

	return strings.TrimSpace(s)
}

// resolveTaskHierarchy returns epicID and projID for a task.
func (a *App) resolveTaskHierarchy(task *Task) (epicID, projID string) {
	if task.EpicID != "" {
		epicID = task.EpicID
		if epic, err := a.DB.GetEpic(task.EpicID); err == nil {
			projID = epic.ProjectID
		}
	}
	return
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
