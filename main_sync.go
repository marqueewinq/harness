package main

import (
	"github.com/marqueewinq/harness/internal"
	"github.com/spf13/cobra"
)

func workSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync [task-id]",
		Short: "Rebase the task branch onto its base branch",
		Long: `Rebase the task branch onto its current base branch inside the task worktree.

On conflicts the rebase is left in progress: resolve the files, git add them,
then run harness work sync --continue (or --abort to give up).
After a successful sync the task returns to IN_PROGRESS and checks must re-run.`,
		Args: cobra.MaximumNArgs(1),
	}
	var cont, abort bool
	cmd.Flags().BoolVar(&cont, "continue", false, "Continue a rebase after resolving conflicts")
	cmd.Flags().BoolVar(&abort, "abort", false, "Abort an in-progress rebase")
	cmd.MarkFlagsMutuallyExclusive("continue", "abort")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdWorkSync(optionalArg(args), cont, abort)
	})
	return cmd
}
