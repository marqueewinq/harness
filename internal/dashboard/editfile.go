package dashboard

import (
	"fmt"
	"strings"
)

// EditFields are the human-edited fields of a task or epic.
type EditFields struct {
	Title       string
	Description string
	Notes       string
}

// normalized drops what the edit file cannot carry: surrounding spaces of the
// title and trailing blank lines of the texts.
func (f EditFields) normalized() EditFields {
	return EditFields{
		Title:       strings.TrimSpace(f.Title),
		Description: strings.TrimRight(f.Description, " \t\r\n"),
		Notes:       strings.TrimRight(f.Notes, " \t\r\n"),
	}
}

const (
	editTitlePrefix  = "Title:"
	editDescription  = "--- description ---"
	editNotes        = "--- notes ---"
	editFileComments = "# Lines starting with '#' above the Title line are ignored. Save and quit to apply;\n" +
		"# leave the file unchanged or empty the title to cancel. Keep the two '---' lines:\n" +
		"# the description is everything between them, the notes everything after the second.\n"
)

// FormatEditFile renders f for the editor. Comments are only recognised
// above the Title line, so a description or notes line starting with '#'
// (e.g. a markdown heading) survives the round trip.
func FormatEditFile(subject string, f EditFields) string {
	f = f.normalized()
	var b strings.Builder
	b.WriteString(editFileComments)
	fmt.Fprintf(&b, "# Editing %s.\n", subject)
	fmt.Fprintf(&b, "%s %s\n%s\n", editTitlePrefix, f.Title, editDescription)
	if f.Description != "" {
		b.WriteString(f.Description + "\n")
	}
	b.WriteString(editNotes + "\n")
	if f.Notes != "" {
		b.WriteString(f.Notes + "\n")
	}
	return b.String()
}

// ParseEditFile reads an edit file back. The description ends at the first
// notes separator after it; the notes are everything after that.
func ParseEditFile(text string) (EditFields, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && (strings.HasPrefix(lines[i], "#") || strings.TrimSpace(lines[i]) == "") {
		i++
	}
	if i == len(lines) || !strings.HasPrefix(lines[i], editTitlePrefix) {
		return EditFields{}, fmt.Errorf("expected a %q line first (after '#' comments)", editTitlePrefix)
	}
	f := EditFields{Title: strings.TrimSpace(strings.TrimPrefix(lines[i], editTitlePrefix))}
	i++
	if i == len(lines) || strings.TrimSpace(lines[i]) != editDescription {
		return EditFields{}, fmt.Errorf("expected %q right after the title", editDescription)
	}
	i++
	start := i
	for i < len(lines) && strings.TrimSpace(lines[i]) != editNotes {
		i++
	}
	if i == len(lines) {
		return EditFields{}, fmt.Errorf("missing the %q line", editNotes)
	}
	f.Description = strings.Join(lines[start:i], "\n")
	f.Notes = strings.Join(lines[i+1:], "\n")
	return f.normalized(), nil
}

// changedFields lists the fields that differ, ignoring what normalization drops.
func changedFields(old, new EditFields) string {
	old, new = old.normalized(), new.normalized()
	var out []string
	if old.Title != new.Title {
		out = append(out, "title")
	}
	if old.Description != new.Description {
		out = append(out, "description")
	}
	if old.Notes != new.Notes {
		out = append(out, "notes")
	}
	return strings.Join(out, ", ")
}

// splitCommandLine splits an editor setting the way a POSIX shell splits
// words: whitespace separates, single quotes are literal, double quotes allow
// backslash escapes, and a backslash outside quotes escapes the next byte.
// Expansions ($VAR, globs) are not performed.
func splitCommandLine(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, fmt.Errorf("unterminated ' in %q", s)
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
			inWord = true
		case c == '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
					i++
				}
				cur.WriteByte(s[i])
			}
			if i == len(s) {
				return nil, fmt.Errorf("unterminated \" in %q", s)
			}
			inWord = true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out, nil
}

// ResolveEditor picks $VISUAL, then $EDITOR, then nano, then vi, skipping
// any that is not installed. Values may carry quoted arguments
// (e.g. `"/Applications/My Editor.app/bin/edit" --wait`).
func ResolveEditor(getenv func(string) string, lookPath func(string) (string, error)) ([]string, error) {
	var candidates [][]string
	for _, env := range []string{"VISUAL", "EDITOR"} {
		fields, err := splitCommandLine(getenv(env))
		if err != nil {
			return nil, fmt.Errorf("$%s: %w", env, err)
		}
		if len(fields) > 0 {
			candidates = append(candidates, fields)
		}
	}
	candidates = append(candidates, []string{"nano"}, []string{"vi"})
	for _, c := range candidates {
		if path, err := lookPath(c[0]); err == nil {
			return append([]string{path}, c[1:]...), nil
		}
	}
	return nil, fmt.Errorf("no editor found: set VISUAL or EDITOR, or install nano or vi")
}
