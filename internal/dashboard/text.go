package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// flatten makes user text safe for one pane line: no escapes or line breaks.
func flatten(s string) string {
	return strings.Join(strings.Fields(ansi.Strip(s)), " ")
}

// textLines keeps user text's line structure but strips escapes, so it can be
// shown expanded without corrupting the screen.
func textLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(s, "\r\n"), "\n") {
		l = strings.ReplaceAll(ansi.Strip(strings.TrimRight(l, "\r")), "\t", "    ")
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// doc builds a pane twice: collapsed, with user text flattened to one line,
// and full, with user text kept multi-line for the expand key.
type doc struct {
	lines      []string
	full       []string
	expandable bool
}

func (d *doc) add(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	d.lines = append(d.lines, s)
	d.full = append(d.full, s)
}

// text adds user-written text. Collapsed it is label + flattened text on one
// line; expanded the label stands alone and each line follows indented.
func (d *doc) text(indent, label, s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	d.expandable = true
	d.lines = append(d.lines, indent+label+flatten(s))
	if label != "" {
		d.full = append(d.full, indent+strings.TrimRight(label, " "))
	}
	for _, l := range textLines(s) {
		d.full = append(d.full, indent+"  "+l)
	}
}

// wrapLines wraps each line to width display cells, keeping its indentation
// on continuation lines.
func wrapLines(lines []string, width int) []string {
	var out []string
	for _, l := range lines {
		indent := l[:len(l)-len(strings.TrimLeft(l, " "))]
		room := width - len(indent)
		if ansi.StringWidth(l) <= width || room < 10 {
			out = append(out, l)
			continue
		}
		for _, w := range strings.Split(ansi.Wrap(l[len(indent):], room, ""), "\n") {
			out = append(out, indent+w)
		}
	}
	return out
}
