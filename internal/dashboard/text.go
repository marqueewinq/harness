package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// collapsedTextLines is how many wrapped lines of one piece of user text a
// pane shows before the expand key.
const collapsedTextLines = 6

// flatten makes user text safe for one list line: no escapes or line breaks.
func flatten(s string) string {
	return strings.Join(strings.Fields(ansi.Strip(s)), " ")
}

// textLines keeps user text's line structure but strips escapes, so it can be
// shown without corrupting the screen.
func textLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(s, "\r\n"), "\n") {
		l = strings.ReplaceAll(ansi.Strip(strings.TrimRight(l, "\r")), "\t", "    ")
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// maxTextWidth bounds the line length of user text (descriptions, verdicts,
// review comments) on wide terminals, for readability.
const maxTextWidth = 150

// Span marks Lines[Start:End] as user-written text, which a pane wraps at
// maxTextWidth and, collapsed, caps at collapsedTextLines wrapped lines.
type Span struct{ Start, End int }

type doc struct {
	lines []string
	spans []Span
}

func (d *doc) add(format string, args ...interface{}) {
	d.lines = append(d.lines, fmt.Sprintf(format, args...))
}

// text adds user-written text under its label, one indented line per line
// of the text.
func (d *doc) text(indent, label, s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	if label != "" {
		d.lines = append(d.lines, indent+strings.TrimRight(label, " "))
	}
	d.textUnder(len(d.lines), indent, s)
}

// textUnder appends s's lines and marks them, from line start on, as one span.
func (d *doc) textUnder(start int, indent, s string) {
	if strings.TrimSpace(s) != "" {
		for _, l := range textLines(s) {
			d.lines = append(d.lines, indent+"  "+l)
		}
	}
	d.spans = append(d.spans, Span{start, len(d.lines)})
}

// wrapLines wraps each line to width display cells, keeping its indentation
// on continuation lines.
func wrapLines(lines []string, width int) []string {
	var out []string
	for _, l := range lines {
		indent := l[:len(l)-len(strings.TrimLeft(l, " "))]
		room := width - len(indent)
		if ansi.StringWidth(l) <= width {
			out = append(out, l)
			continue
		}
		if room < 10 {
			indent, room = "", width
		}
		for _, w := range strings.Split(ansi.Wrap(strings.TrimLeft(l, " "), room, ""), "\n") {
			out = append(out, indent+w)
		}
	}
	return out
}

// paneLines wraps lines to width, and spans to at most maxTextWidth; unless
// expanded, each span keeps at most collapsedTextLines wrapped lines and ends
// with a hint. hidden counts the wrapped lines left out.
func paneLines(st Styles, lines []string, spans []Span, width int, expanded bool) (out []string, hidden int) {
	for i := 0; i < len(lines); {
		span := Span{i, i + 1}
		user := false
		for _, s := range spans {
			if s.Start == i && s.End > s.Start {
				span, user = s, true
			}
		}
		w := width
		if user {
			w = min(width, maxTextWidth)
		}
		wrapped := wrapLines(lines[span.Start:span.End], w)
		if user && !expanded && len(wrapped) > collapsedTextLines {
			n := len(wrapped) - collapsedTextLines
			hidden += n
			indent := lines[span.Start][:len(lines[span.Start])-len(strings.TrimLeft(lines[span.Start], " "))]
			wrapped = append(wrapped[:collapsedTextLines:collapsedTextLines],
				st.Dim.Render(fmt.Sprintf("%s… %d more lines — x to expand", indent, n)))
		}
		out = append(out, wrapped...)
		i = span.End
	}
	return out, hidden
}
