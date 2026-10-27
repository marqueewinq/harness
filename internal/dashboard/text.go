package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/marqueewinq/harness/internal"
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

// Span marks Lines[Start:End] as a block the expand key opens. By default it
// is user-written text, which a pane wraps at maxTextWidth and, collapsed,
// caps at its first collapsedTextLines wrapped lines. With Tail > 0 it is a
// log (e.g. state history): collapsed, it keeps its last Tail lines.
type Span struct {
	Start, End int
	Tail       int
}

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
	d.spans = append(d.spans, Span{Start: start, End: len(d.lines)})
}

// historyEntry is one state transition for doc.history.
type historyEntry struct {
	at       time.Time
	from, to string
	note     string
}

// history adds the state history as a log block: oldest first, each entry
// with how long the subject stayed in the state it entered (the last one
// until now), notes on their entry's line; collapsed, only the last
// historyMaxLines show.
func (d *doc) history(entries []historyEntry, now time.Time) {
	if len(entries) == 0 {
		return
	}
	d.add("History:")
	start := len(d.lines)
	times := make([]time.Time, len(entries))
	for i, e := range entries {
		times[i] = e.at
	}
	stays := internal.TimeInStates(times, now)
	for i, e := range entries {
		from := e.from
		if from == "" {
			from = "(created)"
		}
		line := fmt.Sprintf("  %s  %s → %s  %s", e.at.Format("01-02 15:04:05"), from, e.to,
			internal.FormatStay(stays[i], i == len(entries)-1))
		if note := flatten(e.note); note != "" {
			line += " · " + note
		}
		d.lines = append(d.lines, line)
	}
	d.spans = append(d.spans, Span{Start: start, End: len(d.lines), Tail: historyMaxLines})
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
		span := Span{Start: i, End: i + 1}
		user := false
		for _, s := range spans {
			if s.Start == i && s.End > s.Start {
				span, user = s, true
			}
		}
		if user && span.Tail > 0 {
			block := lines[span.Start:span.End]
			if n := len(block) - span.Tail; !expanded && n > 0 {
				hidden += n
				out = append(out, st.Dim.Render(fmt.Sprintf("  … %d earlier — x to expand", n)))
				block = block[n:]
			}
			out = append(out, wrapLines(block, width)...)
			i = span.End
			continue
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
