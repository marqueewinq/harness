package dashboard

import (
	"reflect"
	"testing"

	"github.com/marqueewinq/harness/internal"
)

// "Don't ask again" on one type change must not cover a switch to none,
// which can approve the commit under review.
func TestSkipKeyScopesTypeChangesByTarget(t *testing.T) {
	t.Parallel()
	human := skipKey(Modal{Decision: DecisionType, Choice: internal.ReviewHuman})
	none := skipKey(Modal{Decision: DecisionType, Choice: internal.ReviewNone})
	if human == none {
		t.Fatalf("type changes share a skip key: %q", human)
	}
	if got := skipKey(Modal{Decision: "approve"}); got != "approve" {
		t.Errorf("approve skip key = %q", got)
	}
	labels := autoConfirmLabels(map[string]bool{human: true})
	if want := []string{"type → human"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("labels = %v, want %v", labels, want)
	}
}
