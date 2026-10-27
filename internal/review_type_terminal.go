package internal

import "fmt"

// Prefixes of review_type_set_by for types a human set in an interactive UI.
const (
	ReviewSetByPreApproved = "pre-approved by "
	ReviewSetByHuman       = "set by "
)

// RecordTerminalReviewType changes a subject's review type on a decision a
// human confirmed in an interactive UI (e.g. the dashboard), authorized like
// RecordTerminalVerdict. It does not need review.allow_none: setting none
// here is a human pre-approval, not a self-declaration. It only records the
// type; stopping an in-flight review is RecordTerminalStop's job.
func (a *App) RecordTerminalReviewType(subject string, r ReviewType, via, reason string) (string, error) {
	if !r.Valid() {
		return "", fmt.Errorf("invalid review type %q: want default, human or none", r)
	}
	who, err := terminalAuth(via)(nil, subject, "review type "+string(r))
	if err != nil {
		return "", err
	}
	setBy := ReviewSetByHuman + who
	if r == ReviewNone {
		setBy = ReviewSetByPreApproved + who
	}
	if reason != "" {
		setBy += ": " + reason
	}
	if err := a.DB.SetReviewType(subject, r, setBy); err != nil {
		return "", err
	}
	return fmt.Sprintf("Review type of %s is now %s (%s).\n", subject, r, setBy), nil
}
