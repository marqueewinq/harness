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

All workflow operations go through the ` + "`harness`" + ` CLI. Direct git push, merge,
rebase, and checkout are denied by permission rules. Git add and commit are
allowed (you need them to commit work in the worktree).

## At Session Start

Run ` + "`harness work status`" + ` or ` + "`harness task list --status=all`" + ` to see current tasks.
If told to work on a specific task, run ` + "`harness task start <task-id>`" + ` to begin.
If the task is already IN_PROGRESS, continue in its worktree.

## Workflow

1. ` + "`harness new \"title\"`" + ` — creates branch + worktree + task
2. Implement changes in the worktree (path printed by harness new)
3. Use git add + git commit freely inside the worktree
4. ` + "`harness work check`" + ` — runs configured checks (tests, build, lint as a DAG)
5. ` + "`harness review request`" + ` — outputs diff + review context
6. Spawn a reviewer subagent; reviewer calls ` + "`harness review approve`" + `
   or ` + "`harness review reject --comment \"...\"`" + `
7. If changes requested: fix, commit, go to step 4
8. ` + "`harness work merge`" + ` — enters merge queue, rebases, re-runs checks, merges, cleans up

## Status and History

` + "`harness work status`" + ` shows:
- Task state, branch, worktree, base branch
- Latest check results with the commit SHA they ran against
- Latest review decision
- Full state transition history with timestamps (e.g. IN_PROGRESS -> CHECKS_RUNNING -> CHECKS_PASSED)
- Merge requirements checklist
- Subtask progress

## Rules

- NEVER use git push, git merge, git rebase, or git checkout directly
- Use git add and git commit freely inside the worktree
- ALWAYS run ` + "`harness work check`" + ` before requesting review
- ALWAYS request and pass review before merging
- Follow ` + "`harness work status`" + ` output — it tells you the next action
- If a task exists for your work, continue it instead of creating a new one

## Commands Reference

| Command | Purpose |
|---|---|
| ` + "`harness task list --status=all`" + ` | List all tasks |
| ` + "`harness work status [task-id]`" + ` | Task state, checks, reviews, history, next action |
| ` + "`harness new \"title\"`" + ` | Create task + branch + worktree |
| ` + "`harness work check [task-id]`" + ` | Run checks (DAG-ordered, parallel) |
| ` + "`harness work diff [task-id]`" + ` | Show diff vs base branch |
| ` + "`harness work commit -m \"msg\"`" + ` | Commit with message template |
| ` + "`harness review request [task-id]`" + ` | Request review (outputs diff + context) |
| ` + "`harness review approve [task-id]`" + ` | Record approval |
| ` + "`harness review reject --comment \"...\" [task-id]`" + ` | Record rejection |
| ` + "`harness review history [task-id]`" + ` | Show past reviews |
| ` + "`harness work merge [task-id]`" + ` | Merge queue -> rebase -> checks -> merge |
| ` + "`harness work close [task-id]`" + ` | Close task without merging |
| ` + "`harness task start <task-id>`" + ` | Start a planned task (creates branch + worktree) |

Task ID is optional when you are inside a task's worktree.
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
