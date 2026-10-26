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
	// WorkDir is where the command runs; it may be a task worktree whose
	// branch differs from RepoRoot's.
	WorkDir string
}

// FindRepoRoot resolves the harness repo root from cwd.
func FindRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return resolveRepoRoot(dir)
}

// resolveRepoRoot prefers the main worktree when dir is inside a linked task
// worktree: the worktree has its own checked-out .harness.yaml, but the DB
// lives only under the main root.
func resolveRepoRoot(dir string) (string, error) {
	if root := mainHarnessRoot(dir); root != "" {
		return root, nil
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

func mainHarnessRoot(dir string) string {
	root, err := NewGitClient(dir).MainWorktreeRoot()
	if err != nil || root == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(root, ".harness.yaml")); err != nil {
		return ""
	}
	return root
}

// LoadApp creates an App from the repo root. Given a task worktree, it loads
// the main root's config and DB and keeps the worktree as WorkDir.
func LoadApp(dir string) (*App, error) {
	repoRoot := dir
	if root := mainHarnessRoot(dir); root != "" {
		repoRoot = root
	}
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

	return &App{Config: cfg, DB: db, Git: git, RepoRoot: repoRoot, WorkDir: dir}, nil
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

// resolveBaseBranch determines the base branch for a new task.
func (a *App) resolveBaseBranch(explicit, epicID string) string {
	if explicit != "" {
		return explicit
	}
	if epicID != "" {
		if epic, err := a.DB.GetEpic(epicID); err == nil && epic.Branch != "" {
			return epic.Branch
		}
	}
	return a.Config.MainBranch
}

// createTaskBranch creates a git branch + worktree for a task.
// Returns branch name, worktree path, parent task ID, or an error.
// On failure, cleans up partially created resources.
func (a *App) createTaskBranch(slug, baseBranch string) (branch, worktreePath, parentTaskID string, err error) {
	branch = slug
	worktreePath = filepath.Join(a.RepoRoot, a.Config.WorktreeDir, branch)

	if parentTask, pErr := a.DB.GetTaskByBranch(baseBranch); pErr == nil {
		parentTaskID = parentTask.ID
	}

	if err = a.Git.CreateBranch(branch, baseBranch); err != nil {
		return "", "", "", fmt.Errorf("creating branch: %w", err)
	}
	if err = a.Git.CreateWorktree(branch, worktreePath); err != nil {
		a.Git.DeleteBranch(branch)
		return "", "", "", fmt.Errorf("creating worktree: %w", err)
	}
	return branch, worktreePath, parentTaskID, nil
}

func (a *App) ResolveTaskID(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	git := a.Git
	if a.WorkDir != "" {
		git = NewGitClient(a.WorkDir)
	}
	branch, err := git.CurrentBranch()
	if err != nil {
		return "", fmt.Errorf("cannot detect task: %w", err)
	}
	task, err := a.DB.GetTaskByBranch(branch)
	if err != nil {
		return "", fmt.Errorf("no task found for branch %q — pass task ID explicitly", branch)
	}
	return task.ID, nil
}
