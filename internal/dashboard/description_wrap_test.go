package dashboard

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marqueewinq/harness/internal"
)

const longDescription = "first paragraph alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau upsilon ENDOFLINEONE\n" +
	"second line ENDOFLINETWO\n" +
	"\n" +
	"third block line a\n" +
	"third block line b\n" +
	"third block line c\n" +
	"last line ENDOFDESCRIPTION"

// descriptionModel drives the real loader. Its interval must stay short:
// run executes every command, including the tick, which sleeps the interval.
func descriptionModel(t *testing.T) Model {
	t.Helper()
	app := newRepo(t, humanConfig)
	if _, err := app.DB.CreateTask("described", longDescription, "described", t.TempDir(), "main", "", "", internal.ReviewSetting{}); err != nil {
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

// widestDescription has lines far longer than maxTextWidth.
var widestDescription = strings.Repeat("wordy ", 60) + "ENDWIDE\nshort second line"

func TestUserTextCappedAtMaxTextWidth(t *testing.T) {
	var b doc
	b.add("State: IN_PROGRESS · branch b · base main · %s", strings.Repeat("ok ", 70))
	b.text("", "Description: ", widestDescription)
	verdictLines(&b, []internal.Review{{
		Decision: "request_changes", CommitSHA: "8174267712345", Reviewer: "terminal:someone-with-a-long-name (dashboard)",
		Comment: strings.Repeat("please split ", 30) + "ENDCOMMENT",
	}})
	d := b.detail("task:task-030", "task-030 described")

	for _, tc := range []struct {
		width, max int
	}{{200, maxTextWidth}, {70, 69}} {
		ui := UIState{Selected: 6, DetailOpen: true, Detail: &d, Expanded: true}
		pane := detailLines(t, Render(sampleSnapshot(), ui, tc.width, 50))
		assertNotCut(t, pane, tc.width)
		text := strings.Join(pane, "\n")
		for _, want := range []string{"ENDWIDE", "ENDCOMMENT", "short second line"} {
			if !strings.Contains(text, want) {
				t.Errorf("width %d: %q lost:\n%s", tc.width, want, text)
			}
		}
		var descLines, commentLines int
		for _, l := range pane {
			// Pane lines carry a one-cell gutter before the text.
			body := strings.TrimPrefix(l, " ")
			switch {
			case strings.Contains(l, "wordy"):
				descLines++
			case strings.Contains(l, "please split"), strings.HasPrefix(body, "Latest verdict:"):
				commentLines++
			case strings.HasPrefix(body, "State:"):
				if tc.width == 200 && lipgloss.Width(body) <= maxTextWidth {
					t.Errorf("non-user line wrapped at %d too: %q", maxTextWidth, body)
				}
				continue
			default:
				continue
			}
			if w := lipgloss.Width(body); w > tc.max {
				t.Errorf("width %d: user text line is %d cells, want <= %d: %q", tc.width, w, tc.max, body)
			}
		}
		if descLines < 2 || commentLines < 3 {
			t.Errorf("width %d: user text did not continue on next lines (desc %d, verdict %d):\n%s", tc.width, descLines, commentLines, text)
		}
	}
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
