package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
		Long: `Full-screen view of open reviews, in-flight tasks grouped by epic, the merge
queue and recently finished tasks, refreshed every --interval.

Keys: ↑/↓ or j/k select, enter/tab details, d diff, x expand, f refresh, q quit;
with an open review request: a approve, r reject; on a task or epic: e edit
(title/description/notes in $VISUAL/$EDITOR), p pre-approve
(review type none), t review type. a, r, p, t and e need a terminal and no
HARNESS_REVIEW_TOKEN.
--once prints one plain snapshot and exits (for scripts and agents).`,
		Args: cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&once, "once", false, "Print one snapshot and exit")
	cmd.Flags().BoolVar(&all, "all", false, "Include PLANNED tasks and all MERGED/CLOSED tasks")
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "Refresh interval of the interactive view")
	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		fd := int(os.Stdout.Fd())
		tty := term.IsTerminal(fd)
		if !once {
			if !tty || !term.IsTerminal(int(os.Stdin.Fd())) {
				return errors.New("the interactive dashboard needs a terminal; use --once for plain output")
			}
			if interval <= 0 {
				return fmt.Errorf("--interval must be positive, got %s", interval)
			}
			opts := dashboard.Options{
				Interval:     interval,
				Color:        dashboard.UseColor(tty, os.Getenv("NO_COLOR")),
				AllowActions: os.Getenv(internal.ReviewTokenEnv) == "",
				DenyReason:   internal.ReviewTokenEnv + " is set (a reviewer's environment)",
			}
			m := dashboard.NewModel(dashboard.AppLoader(app, all), opts)
			_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
			return err
		}
		s, err := dashboard.BuildSnapshot(app, time.Now(), all)
		if err != nil {
			return err
		}
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
