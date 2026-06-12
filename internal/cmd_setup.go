package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type opencodePermissionBash struct {
	Wildcard     string `json:"*"`
	GitStatus    string `json:"git status*"`
	GitLog       string `json:"git log*"`
	GitDiff      string `json:"git diff*"`
	GitBranch    string `json:"git branch*"`
	GitAdd       string `json:"git add*"`
	GitCommit    string `json:"git commit*"`
	GitPush      string `json:"git push*"`
	GitMerge     string `json:"git merge*"`
	GitRebase    string `json:"git rebase*"`
	GitCheckout  string `json:"git checkout*"`
	GitStash     string `json:"git stash*"`
	HarnessAll   string `json:"harness *"`
}

type opencodePermission struct {
	Bash opencodePermissionBash `json:"bash"`
}

type opencodeConfig struct {
	Schema     string             `json:"$schema"`
	Permission opencodePermission `json:"permission"`
}

func RunSetupOpencode(dir string) error {
	configPath := filepath.Join(dir, ".harness.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("no .harness.yaml found — run 'harness init' first")
	}

	ocPath := filepath.Join(dir, "opencode.json")

	cfg := opencodeConfig{
		Schema: "https://opencode.ai/config.json",
		Permission: opencodePermission{
			Bash: opencodePermissionBash{
				Wildcard:    "allow",
				GitStatus:   "allow",
				GitLog:      "allow",
				GitDiff:     "allow",
				GitBranch:   "allow",
				GitAdd:      "allow",
				GitCommit:   "allow",
				GitPush:     "deny",
				GitMerge:    "deny",
				GitRebase:   "deny",
				GitCheckout: "deny",
				GitStash:    "deny",
				HarnessAll:  "allow",
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(ocPath, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing opencode.json: %w", err)
	}

	fmt.Printf("Created %s\n", ocPath)
	fmt.Println()
	fmt.Println("Permissions configured:")
	fmt.Println("  ALLOW: all commands by default")
	fmt.Println("  DENY:  git push, merge, rebase, checkout, stash (use harness instead)")
	fmt.Println()
	fmt.Println("The agent must use 'harness' for all git-mutating operations.")

	// Install skills
	if err := installOpenCodeSkill(); err != nil {
		return fmt.Errorf("installing skill: %w", err)
	}
	if err := installPlanningSkill(); err != nil {
		return fmt.Errorf("installing planning skill: %w", err)
	}

	return nil
}

func installOpenCodeSkill() error {
	// OpenCode reads skills from ~/.claude/skills/ (Claude Code compatibility)
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	skillDir := filepath.Join(home, ".claude", "skills", "harness-workflow")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}

	skillPath := filepath.Join(skillDir, "SKILL.md")

	skill := `---
name: harness-workflow
description: >
  Use when starting any development task, implementing features, fixing bugs, or making
  any code changes. Enforces the harness workflow: create task, work in worktree, run checks,
  request review, merge. Use at the start of every session and before any code changes.
---

# Harness Workflow

All git-mutating operations go through the ` + "`harness`" + ` CLI. Direct git commands
(commit, push, merge, checkout, add) are denied by permission rules.

## At Session Start

Run ` + "`harness status`" + ` or ` + "`harness task list --status=all`" + ` to see current tasks.
If told to work on a specific task, run ` + "`harness task start <task-id>`" + ` to begin.
If the task is already IN_PROGRESS, continue in its worktree.

## Workflow

1. ` + "`harness new \"title\"`" + ` — creates branch + worktree + task
2. Implement changes in the worktree (the path is printed by harness new)
3. ` + "`harness check`" + ` — runs configured checks (tests, build, lint)
4. ` + "`harness review request`" + ` — if review.cmd is configured, launches the
   reviewer automatically and waits for verdict. Otherwise prints diff + context.
5. If changes requested: fix, then go to step 3
6. ` + "`harness work merge`" + ` — rebases, re-runs checks, merges to main, cleans up

## Rules

- NEVER use git commit, git push, git merge, git add, git checkout directly
- ALWAYS work in the worktree created by harness new
- ALWAYS run harness check before requesting review
- ALWAYS request and pass review before merging
- If harness status shows an existing task, continue it instead of creating a new one
- Use git add and git commit freely inside the worktree
- Commit changes in the worktree using git (inside the worktree directory)

## Commands Reference

| Command | Purpose |
|---|---|
| ` + "`harness task list --status=all`" + ` | List all tasks |
| ` + "`harness status [task-id]`" + ` | Show task state and next action |
| ` + "`harness new \"title\"`" + ` | Create task + branch + worktree |
| ` + "`harness check [task-id]`" + ` | Run checks |
| ` + "`harness diff [task-id]`" + ` | Show diff vs main |
| ` + "`harness review request [task-id]`" + ` | Request review |
| ` + "`harness review approve [task-id]`" + ` | Record approval |
| ` + "`harness review reject --comment \"...\" [task-id]`" + ` | Record rejection |
| ` + "`harness review history [task-id]`" + ` | Show past reviews |
| ` + "`harness work merge [task-id]`" + ` | Merge to main |
| ` + "`harness task start <task-id>`" + ` | Start a planned task (creates branch + worktree) |
`

	if runtime.GOOS == "windows" {
		skillDir = filepath.Join(home, ".claude", "skills", "harness-workflow")
	}

	if err := os.WriteFile(skillPath, []byte(skill), 0644); err != nil {
		return err
	}

	fmt.Printf("\nInstalled skill: %s\n", skillPath)
	fmt.Println("The agent will load the harness-workflow skill automatically.")

	return nil
}

func installPlanningSkill() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	skillDir := filepath.Join(home, ".claude", "skills", "harness-planning")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	skill := `---
name: harness-planning
description: >
  Use when discussing strategy, decomposing work, or creating projects, epics, and tasks.
  Use at the start of planning sessions.
---

# Harness Planning

Use when decomposing work into projects, epics, and tasks.

## Workflow

1. ` + "`harness project list`" + ` — see existing projects
2. ` + "`harness project new \"title\"`" + ` — create project
3. ` + "`harness epic new \"title\" --project <id>`" + ` — create epics
4. ` + "`harness task new \"description\" --epic <id>`" + ` — create planned tasks
5. ` + "`harness dep add <from> <to>`" + ` — add blockers between tasks
6. ` + "`harness task list --ready`" + ` — see what is unblocked and ready to start

## Rules

- Add dependencies when task B cannot start until task A is merged
- Use ` + "`harness task list --ready`" + ` to find the next task to work on

## Handing Off to Execution

` + "`harness task start <task-id>`" + ` — creates branch + worktree, then follow harness-workflow

## Commands Reference

| Command | Purpose |
|---|---|
| ` + "`harness project new \"title\"`" + ` | Create a project |
| ` + "`harness project list`" + ` | List projects with progress |
| ` + "`harness project view <id>`" + ` | Project details + epics + tasks |
| ` + "`harness project close <id>`" + ` | Close a project |
| ` + "`harness epic new \"title\" --project <id>`" + ` | Create epic under project |
| ` + "`harness epic list [--project <id>]`" + ` | List epics |
| ` + "`harness epic view <id>`" + ` | Epic details + tasks |
| ` + "`harness epic close <id>`" + ` | Close an epic |
| ` + "`harness task new \"desc\" [--epic <id>]`" + ` | Create planned task |
| ` + "`harness task start <task-id> [--base <branch>]`" + ` | Start a planned task |
| ` + "`harness task list --ready`" + ` | List unblocked planned tasks |
| ` + "`harness dep add <from> <to>`" + ` | Add blocker dependency |
| ` + "`harness dep rm <from> <to>`" + ` | Remove dependency |
`
	if err := os.WriteFile(skillPath, []byte(skill), 0644); err != nil {
		return err
	}
	fmt.Printf("Installed skill: %s\n", skillPath)
	return nil
}
