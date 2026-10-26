package main

import (
	"github.com/marqueewinq/harness/internal"
	"github.com/spf13/cobra"
)

func queueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Inspect and repair the merge queue",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List merge queue entries and their lease status",
		Args:  cobra.NoArgs,
		RunE: withApp(func(app *internal.App, c *cobra.Command, args []string) error {
			return app.CmdQueueList()
		}),
	})

	clearCmd := &cobra.Command{
		Use:   "clear <task-id>",
		Short: "Remove a stale entry from the merge queue",
		Args:  cobra.ExactArgs(1),
	}
	var force bool
	clearCmd.Flags().BoolVar(&force, "force", false, "Remove even if its owner process is alive")
	clearCmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		return app.CmdQueueClear(args[0], force)
	})
	cmd.AddCommand(clearCmd)
	return cmd
}
