package main

import (
	"fmt"
	"os"

	"github.com/marqueewinq/harness/internal"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "harness",
		Short: "Local workflow enforcement for AI coding agents",
	}

	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(newCmd())
	rootCmd.AddCommand(workCmd())
	rootCmd.AddCommand(reviewCmd())
	rootCmd.AddCommand(taskCmd())
	rootCmd.AddCommand(projectCmd())
	rootCmd.AddCommand(epicCmd())
	rootCmd.AddCommand(depCmd())
	rootCmd.AddCommand(setupCmd())

	// Top-level aliases (hidden)
	rootCmd.AddCommand(aliasCheckCmd())
	rootCmd.AddCommand(aliasDiffCmd())
	rootCmd.AddCommand(aliasStatusCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func withApp(fn func(*internal.App, *cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		root, err := internal.FindRepoRoot()
		if err != nil {
			return err
		}
		app, err := internal.LoadApp(root)
		if err != nil {
			return err
		}
		defer app.Close()
		return fn(app, cmd, args)
	}
}

// --- init ---

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize harness in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _ := os.Getwd()
			return internal.RunInit(dir)
		},
	}
}

// --- new ---

func newCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Create a new task with branch and worktree",
		Args:  cobra.ExactArgs(1),
	}
	var baseBranch, epicID, description string
	cmd.Flags().StringVar(&baseBranch, "base", "", "Base branch to fork from and merge into (default: main branch)")
	cmd.Flags().StringVar(&epicID, "epic", "", "Parent epic ID")
	cmd.Flags().StringVar(&description, "description", "", "Task description / notes")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdNew(args[0], baseBranch, epicID, description)
	})
	return cmd
}

// --- work group ---

func workCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "work",
		Short: "Work on tasks",
	}
	cmd.AddCommand(workStatusCmd())
	cmd.AddCommand(workCheckCmd())
	cmd.AddCommand(workDiffCmd())
	cmd.AddCommand(workMergeCmd())
	cmd.AddCommand(workCloseCmd())
	return cmd
}

func workStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [task-id]",
		Short: "Show task status",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdWorkStatus(taskID)
		}),
	}
}

func workCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check [task-id]",
		Short: "Run checks",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdWorkCheck(taskID)
		}),
	}
}

func workDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff [task-id]",
		Short: "Show diff against main",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdWorkDiff(taskID)
		}),
	}
}

func workMergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge [task-id]",
		Short: "Merge task branch into main",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdWorkMerge(taskID)
		}),
	}
}

func workCloseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close [task-id]",
		Short: "Close a task without merging",
		Args:  cobra.MaximumNArgs(1),
	}
	var cleanup bool
	cmd.Flags().BoolVar(&cleanup, "cleanup", false, "Remove worktree and delete branch")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		taskID := ""
		if len(args) > 0 {
			taskID = args[0]
		}
		return app.CmdWorkClose(taskID, cleanup)
	})
	return cmd
}

// --- review group ---

func reviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Manage reviews",
	}
	cmd.AddCommand(reviewRequestCmd())
	cmd.AddCommand(reviewApproveCmd())
	cmd.AddCommand(reviewRejectCmd())
	cmd.AddCommand(reviewHistoryCmd())
	return cmd
}

func reviewRequestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "request [task-id]",
		Short: "Request review for task",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdReviewRequest(taskID)
		}),
	}
}

func reviewApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve [task-id]",
		Short: "Approve a task",
		Args:  cobra.MaximumNArgs(1),
	}
	var comment string
	cmd.Flags().StringVar(&comment, "comment", "", "Review comment")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		taskID := ""
		if len(args) > 0 {
			taskID = args[0]
		}
		return app.CmdReviewApprove(taskID, comment)
	})
	return cmd
}

func reviewRejectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reject [task-id]",
		Short: "Request changes on a task",
		Args:  cobra.MaximumNArgs(1),
	}
	var comment string
	cmd.Flags().StringVar(&comment, "comment", "", "Review comment")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		taskID := ""
		if len(args) > 0 {
			taskID = args[0]
		}
		return app.CmdReviewReject(taskID, comment)
	})
	return cmd
}

func reviewHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history [task-id]",
		Short: "Show review history",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdReviewHistory(taskID)
		}),
	}
}

// --- task group ---

func taskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
	}
	cmd.AddCommand(taskNewCmd())
	cmd.AddCommand(taskEditCmd())
	cmd.AddCommand(taskDeleteCmd())
	cmd.AddCommand(taskCloseCmd())
	cmd.AddCommand(taskViewCmd())
	cmd.AddCommand(taskListCmd())
	cmd.AddCommand(taskStartCmd())
	return cmd
}

func taskNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Create a new task",
		Args:  cobra.ExactArgs(1),
	}
	var epicID, description string
	cmd.Flags().StringVar(&epicID, "epic", "", "Parent epic ID")
	cmd.Flags().StringVar(&description, "description", "", "Task description / notes")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdTaskNew(args[0], description, epicID)
	})
	return cmd
}

func taskEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit [task-id]",
		Short: "Edit a task",
		Args:  cobra.ExactArgs(1),
	}
	var editTitle, editDesc, editEpic string
	cmd.Flags().StringVar(&editTitle, "title", "", "New title")
	cmd.Flags().StringVar(&editDesc, "description", "", "New description / notes")
	cmd.Flags().StringVar(&editEpic, "epic", "", "New epic ID (empty to unset)")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdTaskEdit(args[0], editTitle, editDesc, editEpic,
			c.Flags().Changed("title"),
			c.Flags().Changed("description"),
			c.Flags().Changed("epic"))
	})
	return cmd
}

func taskDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [task-id]",
		Short: "Delete a task",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdTaskDelete(args[0])
		}),
	}
}

func taskCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close [task-id]",
		Short: "Close a task",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdTaskClose(args[0])
		}),
	}
}

func taskViewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "view [task-id]",
		Short: "View task details",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdTaskView(args[0])
		}),
	}
}

func taskListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
	}
	var epicFilter, projectFilter, statusFilter string
	var readyOnly bool
	cmd.Flags().StringVar(&epicFilter, "epic", "", "Filter by epic")
	cmd.Flags().StringVar(&projectFilter, "project", "", "Filter by project")
	cmd.Flags().StringVar(&statusFilter, "status", "", "Filter by status")
	cmd.Flags().BoolVar(&readyOnly, "ready", false, "Only show tasks ready to start")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdTaskList(epicFilter, projectFilter, statusFilter, readyOnly)
	})
	return cmd
}

func taskStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start [task-id]",
		Short: "Start a task (creates branch + worktree)",
		Args:  cobra.ExactArgs(1),
	}
	var baseBranch string
	cmd.Flags().StringVar(&baseBranch, "base", "", "Base branch (default: main)")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdTaskStart(args[0], baseBranch)
	})
	return cmd
}

// --- project group ---

func projectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}
	cmd.AddCommand(projectNewCmd())
	cmd.AddCommand(projectEditCmd())
	cmd.AddCommand(projectListCmd())
	cmd.AddCommand(projectViewCmd())
	cmd.AddCommand(projectCloseCmd())
	return cmd
}

func projectNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
	}
	var projDesc string
	cmd.Flags().StringVar(&projDesc, "description", "", "Project description")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdProjectNew(args[0], projDesc)
	})
	return cmd
}

func projectEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit [project-id]",
		Short: "Edit a project",
		Args:  cobra.ExactArgs(1),
	}
	var editTitle, editDesc string
	cmd.Flags().StringVar(&editTitle, "title", "", "New title")
	cmd.Flags().StringVar(&editDesc, "description", "", "New description")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdProjectEdit(args[0], editTitle, editDesc,
			c.Flags().Changed("title"),
			c.Flags().Changed("description"))
	})
	return cmd
}

func projectListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdProjectList()
		}),
	}
}

func projectViewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "view [project-id]",
		Short: "Show project details",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdProjectView(args[0])
		}),
	}
}

func projectCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close [project-id]",
		Short: "Close a project",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdProjectClose(args[0])
		}),
	}
}

// --- epic group ---

func epicCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "epic",
		Short: "Manage epics",
	}
	cmd.AddCommand(epicNewCmd())
	cmd.AddCommand(epicEditCmd())
	cmd.AddCommand(epicListCmd())
	cmd.AddCommand(epicViewCmd())
	cmd.AddCommand(epicCloseCmd())
	return cmd
}

func epicNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Create a new epic",
		Args:  cobra.ExactArgs(1),
	}
	var epicDesc, epicProject string
	cmd.Flags().StringVar(&epicProject, "project", "", "Parent project ID")
	cmd.Flags().StringVar(&epicDesc, "description", "", "Epic description")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdEpicNew(args[0], epicDesc, epicProject)
	})
	return cmd
}

func epicEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit [epic-id]",
		Short: "Edit an epic",
		Args:  cobra.ExactArgs(1),
	}
	var editTitle, editDesc, editProject string
	cmd.Flags().StringVar(&editTitle, "title", "", "New title")
	cmd.Flags().StringVar(&editDesc, "description", "", "New description")
	cmd.Flags().StringVar(&editProject, "project", "", "New project ID (empty to unset)")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdEpicEdit(args[0], editTitle, editDesc, editProject,
			c.Flags().Changed("title"),
			c.Flags().Changed("description"),
			c.Flags().Changed("project"))
	})
	return cmd
}

func epicListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List epics",
	}
	var epicListProject string
	cmd.Flags().StringVar(&epicListProject, "project", "", "Filter by project")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdEpicList(epicListProject)
	})
	return cmd
}

func epicViewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "view [epic-id]",
		Short: "Show epic details",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdEpicView(args[0])
		}),
	}
}

func epicCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close [epic-id]",
		Short: "Close an epic",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdEpicClose(args[0])
		}),
	}
}

// --- dep group ---

func depCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dep",
		Short: "Manage task dependencies",
	}
	addCmd := &cobra.Command{
		Use:   "add [from] [to]",
		Short: "Add dependency (from blocks to)",
		Args:  cobra.ExactArgs(2),
	}
	var depType string
	addCmd.Flags().StringVar(&depType, "type", "blocks", "Dependency type: blocks or related")
	addCmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdDepAdd(args[0], args[1], depType)
	})
	rmCmd := &cobra.Command{
		Use:   "rm [from] [to]",
		Short: "Remove dependency",
		Args:  cobra.ExactArgs(2),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdDepRm(args[0], args[1])
		}),
	}
	cmd.AddCommand(addCmd, rmCmd)
	return cmd
}

// --- setup group ---

func setupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure agent permissions for harness",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "opencode",
		Short: "Generate opencode.json with harness permissions",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _ := os.Getwd()
			return internal.RunSetupOpencode(dir)
		},
	})
	return cmd
}

// --- top-level aliases (hidden) ---

func aliasCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "check [task-id]",
		Short:  "Run checks (alias for work check)",
		Args:   cobra.MaximumNArgs(1),
		Hidden: true,
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdWorkCheck(taskID)
		}),
	}
}

func aliasDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "diff [task-id]",
		Short:  "Show diff against main (alias for work diff)",
		Args:   cobra.MaximumNArgs(1),
		Hidden: true,
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdWorkDiff(taskID)
		}),
	}
}

func aliasStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "status [task-id]",
		Short:  "Show task status (alias for work status)",
		Args:   cobra.MaximumNArgs(1),
		Hidden: true,
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdWorkStatus(taskID)
		}),
	}
}
