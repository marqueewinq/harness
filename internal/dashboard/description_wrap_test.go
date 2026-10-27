package dashboard

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const longDescription = "first paragraph alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau upsilon ENDOFLINEONE\n" +
	"second line ENDOFLINETWO\n" +
	"\n" +
	"third block line a\n" +
	"third block line b\n" +
	"third block line c\n" +
	"last line ENDOFDESCRIPTION"

// descriptionModel drives the real loader. The 1ms interval matters: run
// executes every command, and a tea.Tick sleeps its whole interval (the
// hang seen with time.Hour was the test's tick, not a model loop).
func descriptionModel(t *testing.T) Model {
	t.Helper()
	app := newRepo(t, humanConfig)
	if _, err := app.DB.CreateTask("described", longDescription, "described", t.TempDir(), "main", "", ""); err != nil {
		t.Fatal(err)
	}
	m := NewModel(AppLoader(app, false), Options{Interval: time.Millisecond})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 70, Height: 40})
	return run(t, m, m.Init())
}

// detailLines is the detail pane: the lines after its separator, without the key bar.
func detailLines(t *testing.T, view string) []string {
	t.Helper()
	lines := strings.Split(view, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "── ") {
			return lines[i+1 : len(lines)-1]
		}
	}
	t.Fatalf("no detail pane:\n%s", view)
	return nil
}

func assertNotCut(t *testing.T, pane []string, width int) {
	t.Helper()
	for _, l := range pane {
		if strings.HasSuffix(l, "…") {
			t.Errorf("pane line cut at the edge: %q", l)
		}
		if w := lipgloss.Width(l); w > width {
			t.Errorf("pane line wider than %d: %q", width, l)
		}
	}
}

func TestDescriptionWrappedAndCappedBeforeExpand(t *testing.T) {
	t.Parallel()
	m := press(t, descriptionModel(t), "enter")
	pane := detailLines(t, m.View())
	text := strings.Join(pane, "\n")
	assertNotCut(t, pane, 70)
	for _, want := range []string{"ENDOFLINEONE", "ENDOFLINETWO", "upsilon", "x to expand"} {
		if !strings.Contains(text, want) {
			t.Errorf("collapsed pane lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ENDOFDESCRIPTION") {
		t.Errorf("collapsed pane shows the whole description:\n%s", text)
	}
}

func TestDescriptionExpandsToAllLines(t *testing.T) {
	t.Parallel()
	m := press(t, descriptionModel(t), "enter", "x")
	if !m.ui.Expanded {
		t.Fatalf("x did not expand; status %q", m.ui.Status)
	}
	pane := detailLines(t, m.View())
	text := strings.Join(pane, "\n")
	assertNotCut(t, pane, 70)
	for _, want := range []string{"ENDOFLINEONE", "ENDOFLINETWO", "third block line c", "ENDOFDESCRIPTION"} {
		if !strings.Contains(text, want) {
			t.Errorf("expanded pane lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "x to expand") {
		t.Errorf("expanded pane still capped:\n%s", text)
	}
}

func describedDetail() *Detail {
	var b doc
	b.add("State: IN_PROGRESS for 5m · branch described · base main")
	b.add("Worktree: /repo/.worktrees/a-rather-long-worktree-directory-name-that-must-wrap-too")
	b.text("", "Description: ", longDescription)
	b.add("Review: not requested")
	d := b.detail("task:task-030", "task-030 described")
	return &d
}

func TestDescriptionWrapGolden(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		ui := UIState{Selected: 6, DetailOpen: true, Detail: describedDetail(), Expanded: expanded, Actions: DefaultActions(false)}
		out := Render(sampleSnapshot(), ui, 70, 40)
		pane := detailLines(t, out)
		assertNotCut(t, pane, 70)
		checkGolden(t, fmt.Sprintf("tui_description_expanded_%v.golden", expanded), out+"\n")
	}
}
