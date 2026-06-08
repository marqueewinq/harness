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
	return &cobra.Command{
		Use:   "new [description]",
		Short: "Create a new task with branch and worktree",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			return app.CmdNew(args[0])
		}),
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tasks",
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			return app.CmdList()
		}),
	}
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
