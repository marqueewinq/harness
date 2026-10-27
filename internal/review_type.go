package internal

import (
	"fmt"
	"time"
)

type ReviewType string

const (
	ReviewDefault ReviewType = "default"
	ReviewHuman   ReviewType = "human"
	ReviewNone    ReviewType = "none"
)

const (
	ReviewSetByCreator      = "creator"
	ReviewSetBySelfDeclared = "self-declared (allow_none)"
)

func (r ReviewType) Valid() bool {
	return r == ReviewDefault || r == ReviewHuman || r == ReviewNone
}

// ParseReviewType maps an empty value to ReviewDefault.
func ParseReviewType(s string) (ReviewType, error) {
	if s == "" {
		return ReviewDefault, nil
	}
	if r := ReviewType(s); r.Valid() {
		return r, nil
	}
	return "", fmt.Errorf("invalid review type %q: want default, human or none", s)
}

// FormatReviewType renders a type with its provenance. A default type set at
// creation (or predating review types) is shown bare.
func FormatReviewType(r ReviewType, setBy string, setAt time.Time) string {
	if r == ReviewDefault && (setBy == "" || setBy == ReviewSetByCreator) {
		return string(r)
	}
	s := string(r)
	if setBy != "" {
		s += " · " + setBy
	}
	if !setAt.IsZero() {
		s += " · " + setAt.Local().Format("2006-01-02 15:04")
	}
	return s
}

// SetReviewType is the only writer of a subject's review type. The CLI calls
// it only at creation; later changes belong to the dashboard.
func (db *DB) SetReviewType(subjectID string, r ReviewType, setBy string) error {
	if !r.Valid() {
		return fmt.Errorf("invalid review type %q", r)
	}
	table := "tasks"
	if isEpicID(subjectID) {
		table = "epics"
	}
	res, err := db.conn.Exec(
		fmt.Sprintf(`UPDATE %s SET review_type = ?, review_type_set_by = ?, review_type_set_at = ? WHERE id = ?`, table),
		string(r), setBy, time.Now(), subjectID,
	)
	if err != nil {
		return fmt.Errorf("setting review type of %s: %w", subjectID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%s not found", subjectID)
	}
	return nil
}

// creationReviewType validates --review for a new task or epic and returns
// who to record as having set it. `none` is a self-declared opt-out, so it is
// gated on the config committed on main: a local edit must not enable it.
func (a *App) creationReviewType(flag string) (ReviewType, string, error) {
	r, err := ParseReviewType(flag)
	if err != nil {
		return "", "", fmt.Errorf("--review: %w", err)
	}
	if r != ReviewNone {
		return r, ReviewSetByCreator, nil
	}
	review, err := a.CommittedReviewConfig()
	if err != nil {
		return "", "", fmt.Errorf("--review=none: %w", err)
	}
	if !review.AllowNone {
		return "", "", fmt.Errorf("--review=none is refused: .harness.yaml committed on %s does not set review.allow_none: true.\n"+
			"To allow self-declared no-review tasks and epics, add `allow_none: true` under `review:` and merge it into %s (a local edit does not count).\n"+
			"Otherwise use --review=default or --review=human, or have a human pre-approve it from the dashboard",
			a.Config.MainBranch, a.Config.MainBranch)
	}
	return r, ReviewSetBySelfDeclared, nil
}
