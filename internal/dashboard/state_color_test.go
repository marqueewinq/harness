package dashboard

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/marqueewinq/harness/internal"
)

func TestApprovedStateIsCyan(t *testing.T) {
	t.Parallel()
	st := NewStyles(true)
	approved := stateStyle(st, internal.StateApproved)
	if got, want := approved.GetForeground(), (lipgloss.AdaptiveColor{Light: "30", Dark: "6"}); got != want {
		t.Errorf("APPROVED foreground = %v, want cyan %v", got, want)
	}
	if approved.Render("X") == stateStyle(st, internal.StateMerged).Render("X") {
		t.Error("APPROVED must be distinguishable from passed/merged green")
	}
}
