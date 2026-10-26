package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/marqueewinq/harness/internal"
	"github.com/marqueewinq/harness/internal/dashboard"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func dashboardCmd() *cobra.Command {
	var once, all bool
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Show in-flight tasks, reviews and the merge queue at a glance",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&once, "once", false, "Print one snapshot and exit")
	cmd.Flags().BoolVar(&all, "all", false, "Include PLANNED tasks and all MERGED/CLOSED tasks")
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "Refresh interval of the interactive view")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		if !once {
			return errors.New("interactive dashboard not implemented yet; use --once")
		}
		s, err := dashboard.BuildSnapshot(app, time.Now(), all)
		if err != nil {
			return err
		}
		fd := int(os.Stdout.Fd())
		tty := term.IsTerminal(fd)
		width := 0
		if tty {
			if w, _, err := term.GetSize(fd); err == nil {
				width = w
			}
		}
		fmt.Print(dashboard.RenderPlain(s, width, dashboard.UseColor(tty, os.Getenv("NO_COLOR"))))
		return nil
	})
	return cmd
}
