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
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(checkCmd())
	rootCmd.AddCommand(diffCmd())
	rootCmd.AddCommand(requestReviewCmd())
	rootCmd.AddCommand(recordReviewCmd())
	rootCmd.AddCommand(reviewHistoryCmd())
	rootCmd.AddCommand(mergeCmd())
	rootCmd.AddCommand(setupCmd())
	rootCmd.AddCommand(closeCmd())
	rootCmd.AddCommand(projectCmd())
	rootCmd.AddCommand(epicCmd())
	rootCmd.AddCommand(planCmd())
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(readyCmd())
	rootCmd.AddCommand(depCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

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

func newCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new [description]",
		Short: "Create a new task with branch and worktree",
		Args:  cobra.ExactArgs(1),
	}
	var baseBranch string
	cmd.Flags().StringVar(&baseBranch, "base", "", "Base branch to fork from and merge into (default: main branch)")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdNew(args[0], baseBranch)
	})
	return cmd
}

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

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [task-id]",
		Short: "Show task status",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdStatus(taskID)
		}),
	}
}

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check [task-id]",
		Short: "Run checks",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdCheck(taskID)
		}),
	}
}

func diffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff [task-id]",
		Short: "Show diff against main",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdDiff(taskID)
		}),
	}
}

func requestReviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "request-review [task-id]",
		Short: "Request review for task",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdRequestReview(taskID)
		}),
	}
}

func recordReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record-review [approve|request-changes] [task-id]",
		Short: "Record a review decision",
		Args:  cobra.RangeArgs(1, 2),
	}
	var comment string
	cmd.Flags().StringVar(&comment, "comment", "", "Review comment (required for request-changes)")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		decision := args[0]
		taskID := ""
		if len(args) > 1 {
			taskID = args[1]
		}
		return app.CmdRecordReview(taskID, decision, comment)
	})
	return cmd
}

func reviewHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "review-history [task-id]",
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

func setupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup [agent]",
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

func closeCmd() *cobra.Command {
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
		return app.CmdClose(taskID, cleanup)
	})
	return cmd
}

func mergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge [task-id]",
		Short: "Merge task branch into main",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdMerge(taskID)
		}),
	}
}

func projectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}
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
	listSubCmd := &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdProjectList()
		}),
	}
	statusSubCmd := &cobra.Command{
		Use:   "status [project-id]",
		Short: "Show project status",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdProjectStatus(args[0])
		}),
	}
	closeSubCmd := &cobra.Command{
		Use:   "close [project-id]",
		Short: "Close a project",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdProjectClose(args[0])
		}),
	}
	cmd.AddCommand(newCmd, listSubCmd, statusSubCmd, closeSubCmd)
	return cmd
}

func epicCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "epic",
		Short: "Manage epics",
	}
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
	listSubCmd := &cobra.Command{
		Use:   "list",
		Short: "List epics",
	}
	var epicListProject string
	listSubCmd.Flags().StringVar(&epicListProject, "project", "", "Filter by project")
	listSubCmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdEpicList(epicListProject)
	})
	statusSubCmd := &cobra.Command{
		Use:   "status [epic-id]",
		Short: "Show epic status",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdEpicStatus(args[0])
		}),
	}
	closeSubCmd := &cobra.Command{
		Use:   "close [epic-id]",
		Short: "Close an epic",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdEpicClose(args[0])
		}),
	}
	cmd.AddCommand(newCmd, listSubCmd, statusSubCmd, closeSubCmd)
	return cmd
}

func planCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Manage planned tasks",
	}

	// plan new
	newCmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Create a planned task",
		Args:  cobra.ExactArgs(1),
	}
	var planEpicID, planDesc string
	newCmd.Flags().StringVar(&planEpicID, "epic", "", "Parent epic ID")
	newCmd.Flags().StringVar(&planDesc, "description", "", "Task description / notes")
	newCmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdPlanNew(args[0], planDesc, planEpicID)
	})

	// plan edit
	editCmd := &cobra.Command{
		Use:   "edit [task-id]",
		Short: "Edit a planned task",
		Args:  cobra.ExactArgs(1),
	}
	var editTitle, editDesc, editEpic string
	editCmd.Flags().StringVar(&editTitle, "title", "", "New title")
	editCmd.Flags().StringVar(&editDesc, "description", "", "New description / notes")
	editCmd.Flags().StringVar(&editEpic, "epic", "", "New epic ID (empty to unset)")
	editCmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdPlanEdit(args[0], editTitle, editDesc, editEpic,
			c.Flags().Changed("title"),
			c.Flags().Changed("description"),
			c.Flags().Changed("epic"))
	})

	// plan delete
	deleteCmd := &cobra.Command{
		Use:   "delete [task-id]",
		Short: "Delete a planned task",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdPlanDelete(args[0])
		}),
	}

	// plan view
	viewCmd := &cobra.Command{
		Use:   "view [task-id]",
		Short: "View planned task details",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdPlanView(args[0])
		}),
	}

	// plan list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List planned tasks",
	}
	var planListEpic, planListProject string
	listCmd.Flags().StringVar(&planListEpic, "epic", "", "Filter by epic")
	listCmd.Flags().StringVar(&planListProject, "project", "", "Filter by project")
	listCmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdPlanList(planListEpic, planListProject)
	})

	cmd.AddCommand(newCmd, editCmd, deleteCmd, viewCmd, listCmd)
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
	cmd := &cobra.Command{
		Use:   "ready",
		Short: "List planned tasks ready to start",
	}
	var readyEpic, readyProject string
	cmd.Flags().StringVar(&readyEpic, "epic", "", "Filter by epic")
	cmd.Flags().StringVar(&readyProject, "project", "", "Filter by project")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdReady(readyEpic, readyProject)
	})
	return cmd
}

func depCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dep",
		Short: "Manage task dependencies",
	}
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
