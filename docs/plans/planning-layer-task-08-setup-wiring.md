### Task 8: Skills, Setup, and CLI Wiring

**Files:**
- Modify: `internal/cmd_setup.go`
- Modify: `main.go`

---

- [ ] **Step 1: Add harness-planning skill to cmd_setup.go**

Add a new function `installPlanningSkill` in `internal/cmd_setup.go`, following
the same pattern as `installOpenCodeSkill`:

```go
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
4. ` + "`harness plan \"description\" --epic <id>`" + ` — create planned tasks
5. ` + "`harness dep add <from> <to>`" + ` — add blockers between tasks
6. ` + "`harness ready`" + ` — see what's unblocked and ready to start

## Rules

- Add dependencies when task B cannot start until task A is merged
- Use ` + "`harness ready`" + ` to find the next task to work on

## Handing Off to Execution

` + "`harness start <task-id>`" + ` — creates branch + worktree, then follow harness-workflow

## Commands Reference

| Command | Purpose |
|---|---|
| ` + "`harness project new \"title\"`" + ` | Create a project |
| ` + "`harness project list`" + ` | List projects with progress |
| ` + "`harness project status <id>`" + ` | Project details + epics + tasks |
| ` + "`harness project close <id>`" + ` | Close a project |
| ` + "`harness epic new \"title\" --project <id>`" + ` | Create epic under project |
| ` + "`harness epic list [--project <id>]`" + ` | List epics |
| ` + "`harness epic status <id>`" + ` | Epic details + tasks |
| ` + "`harness epic close <id>`" + ` | Close an epic |
| ` + "`harness plan \"desc\" [--epic <id>]`" + ` | Create planned task |
| ` + "`harness start <task-id> [--base <branch>]`" + ` | Start a planned task |
| ` + "`harness ready`" + ` | List unblocked planned tasks |
| ` + "`harness dep add <from> <to>`" + ` | Add blocker dependency |
| ` + "`harness dep rm <from> <to>`" + ` | Remove dependency |
`

	if err := os.WriteFile(skillPath, []byte(skill), 0644); err != nil {
		return err
	}

	fmt.Printf("Installed skill: %s\n", skillPath)
	return nil
}
```

- [ ] **Step 2: Update the existing harness-workflow skill**

In the `installOpenCodeSkill` function, update the skill content string.
Add to the "At Session Start" section:

```
If told to work on a specific task, run harness start <task-id> to begin.
If the task is already IN_PROGRESS, continue in its worktree.
```

Add `harness start` to the commands table:

```
| harness start <task-id> | Start a planned task (creates branch + worktree) |
```

Also update the rules to allow `git add` and `git commit`:

```
- Use git add and git commit freely inside the worktree
```

- [ ] **Step 3: Call installPlanningSkill from RunSetupOpencode**

Add the call after `installOpenCodeSkill()`:

```go
if err := installPlanningSkill(); err != nil {
	return fmt.Errorf("installing planning skill: %w", err)
}
```

- [ ] **Step 4: Wire new commands in main.go**

Add the new cobra commands to `main.go`:

```go
// In main(), add:
rootCmd.AddCommand(projectCmd())
rootCmd.AddCommand(epicCmd())
rootCmd.AddCommand(planCmd())
rootCmd.AddCommand(startCmd())
rootCmd.AddCommand(readyCmd())
rootCmd.AddCommand(depCmd())

// Add --project and --epic flags to list
```

Implement the command functions:

```go
func projectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	// project new
	newCmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
	}
	var projDesc string
	newCmd.Flags().StringVar(&projDesc, "description", "", "Project description")
	newCmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdProjectNew(args[0], projDesc)
	})

	// project list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdProjectList()
		}),
	}

	// project status
	statusCmd := &cobra.Command{
		Use:   "status [project-id]",
		Short: "Show project status",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdProjectStatus(args[0])
		}),
	}

	// project close
	closeCmd := &cobra.Command{
		Use:   "close [project-id]",
		Short: "Close a project",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdProjectClose(args[0])
		}),
	}

	cmd.AddCommand(newCmd, listCmd, statusCmd, closeCmd)
	return cmd
}

func epicCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "epic",
		Short: "Manage epics",
	}

	// epic new
	newCmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Create a new epic",
		Args:  cobra.ExactArgs(1),
	}
	var epicProject, epicDesc string
	newCmd.Flags().StringVar(&epicProject, "project", "", "Parent project ID (required)")
	newCmd.Flags().StringVar(&epicDesc, "description", "", "Epic description")
	newCmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdEpicNew(args[0], epicDesc, epicProject)
	})

	// epic list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List epics",
	}
	var epicListProject string
	listCmd.Flags().StringVar(&epicListProject, "project", "", "Filter by project")
	listCmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdEpicList(epicListProject)
	})

	// epic status
	statusCmd := &cobra.Command{
		Use:   "status [epic-id]",
		Short: "Show epic status",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdEpicStatus(args[0])
		}),
	}

	// epic close
	closeCmd := &cobra.Command{
		Use:   "close [epic-id]",
		Short: "Close an epic",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdEpicClose(args[0])
		}),
	}

	cmd.AddCommand(newCmd, listCmd, statusCmd, closeCmd)
	return cmd
}

func planCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan [description]",
		Short: "Create a planned task",
		Args:  cobra.ExactArgs(1),
	}
	var epicID string
	cmd.Flags().StringVar(&epicID, "epic", "", "Parent epic ID")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdPlan(args[0], epicID)
	})
	return cmd
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start [task-id]",
		Short: "Start a planned task (creates branch + worktree)",
		Args:  cobra.ExactArgs(1),
	}
	var baseBranch string
	cmd.Flags().StringVar(&baseBranch, "base", "", "Base branch (default: main)")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdStart(args[0], baseBranch)
	})
	return cmd
}

func readyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ready",
		Short: "List planned tasks ready to start",
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			return app.CmdReady()
		}),
	}
}

func depCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dep",
		Short: "Manage task dependencies",
	}

	// dep add
	addCmd := &cobra.Command{
		Use:   "add [from-task] [to-task]",
		Short: "Add dependency (from blocks to)",
		Args:  cobra.ExactArgs(2),
	}
	var depType string
	addCmd.Flags().StringVar(&depType, "type", "blocks", "Dependency type: blocks or related")
	addCmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdDepAdd(args[0], args[1], depType)
	})

	// dep rm
	rmCmd := &cobra.Command{
		Use:   "rm [from-task] [to-task]",
		Short: "Remove dependency",
		Args:  cobra.ExactArgs(2),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdDepRm(args[0], args[1])
		}),
	}

	cmd.AddCommand(addCmd, rmCmd)
	return cmd
}
```

Also update the existing `listCmd` to accept filters:

```go
func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all tasks",
	}
	var projectFilter, epicFilter string
	cmd.Flags().StringVar(&projectFilter, "project", "", "Filter by project")
	cmd.Flags().StringVar(&epicFilter, "epic", "", "Filter by epic")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		if projectFilter != "" || epicFilter != "" {
			return app.CmdListFiltered(projectFilter, epicFilter)
		}
		return app.CmdList()
	})
	return cmd
}
```

- [ ] **Step 5: Verify compilation**

Run: `go build ./...`
Expected: Compiles without errors.

- [ ] **Step 6: Run full test suite**

Run: `go test ./internal/ -v`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/cmd_setup.go main.go
git commit -m "CLI wiring + planning skill"
```
