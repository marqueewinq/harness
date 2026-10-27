package dashboard

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestEditFileRoundTrip(t *testing.T) {
	cases := []EditFields{
		{Title: "Plain", Description: "", Notes: ""},
		{Title: "Multi", Description: "line 1\n\nline 3\n# a markdown heading\n- item", Notes: "note 1\nnote 2"},
		{Title: "Separator in notes", Description: "d", Notes: "--- description ---\n--- notes ---\nstill notes"},
		{Title: "  spaced title  ", Description: "  indented\n\ttabbed", Notes: "trailing\n"},
	}
	for _, f := range cases {
		got, err := ParseEditFile(FormatEditFile("task-030", f))
		if err != nil {
			t.Fatalf("%q: %v", f.Title, err)
		}
		if want := f.normalized(); !reflect.DeepEqual(got, want) {
			t.Errorf("round trip:\n got %+v\nwant %+v", got, want)
		}
	}
}

func TestParseEditFile(t *testing.T) {
	header := "# Editing task-030.\n# another comment\n"
	got, err := ParseEditFile(header + "Title: New title\n--- description ---\nfirst\n# kept: below the title\n--- notes ---\nn1\n\n")
	if err != nil {
		t.Fatal(err)
	}
	want := EditFields{Title: "New title", Description: "first\n# kept: below the title", Notes: "n1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	for name, text := range map[string]string{
		"no title":          "--- description ---\n--- notes ---\n",
		"no description":    "Title: x\n--- notes ---\n",
		"no notes":          "Title: x\n--- description ---\nd\n",
		"text before title": "stray\nTitle: x\n--- description ---\n--- notes ---\n",
	} {
		if _, err := ParseEditFile(text); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
	if f, err := ParseEditFile("Title:   \n--- description ---\nd\n--- notes ---\n"); err != nil || f.Title != "" {
		t.Errorf("empty title: %+v %v", f, err)
	}
}

func TestResolveEditor(t *testing.T) {
	found := map[string]bool{"nano": true, "vi": true, "code": true}
	look := func(name string) (string, error) {
		if found[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	cases := []struct {
		env  map[string]string
		want []string
	}{
		{map[string]string{"VISUAL": "code --wait", "EDITOR": "vi"}, []string{"/usr/bin/code", "--wait"}},
		{map[string]string{"EDITOR": "vi"}, []string{"/usr/bin/vi"}},
		{map[string]string{"VISUAL": "missing-editor"}, []string{"/usr/bin/nano"}},
		{nil, []string{"/usr/bin/nano"}},
	}
	for _, tc := range cases {
		got, err := ResolveEditor(func(k string) string { return tc.env[k] }, look)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("env %v: %v %v, want %v", tc.env, got, err, tc.want)
		}
	}
	delete(found, "nano")
	if got, _ := ResolveEditor(func(string) string { return "" }, look); !reflect.DeepEqual(got, []string{"/usr/bin/vi"}) {
		t.Errorf("fallback to vi: %v", got)
	}
	delete(found, "vi")
	if _, err := ResolveEditor(func(string) string { return "" }, look); err == nil || !strings.Contains(err.Error(), "EDITOR") {
		t.Errorf("no editor: %v", err)
	}
}

func TestSplitCommandLine(t *testing.T) {
	for in, want := range map[string][]string{
		"vim":                                  {"vim"},
		"  code   --wait ":                     {"code", "--wait"},
		`"/opt/My Editor/bin/edit" --wait`:     {"/opt/My Editor/bin/edit", "--wait"},
		`'/opt/it'"'"'s/edit' -f`:              {"/opt/it's/edit", "-f"},
		`/opt/My\ Editor/edit`:                 {"/opt/My Editor/edit"},
		`emacsclient -a "" -t`:                 {"emacsclient", "-a", "", "-t"},
		`sh -c "exec vim \"\$1\""`:             {"sh", "-c", `exec vim "$1"`},
		"":                                     nil,
	} {
		got, err := splitCommandLine(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("split(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{`"open`, `'open`} {
		if _, err := splitCommandLine(bad); err == nil {
			t.Errorf("split(%q): no error", bad)
		}
	}
	look := func(name string) (string, error) { return name, nil }
	got, err := ResolveEditor(func(k string) string {
		if k == "EDITOR" {
			return `"/opt/My Editor/bin/edit" --wait`
		}
		return ""
	}, look)
	if err != nil || !reflect.DeepEqual(got, []string{"/opt/My Editor/bin/edit", "--wait"}) {
		t.Errorf("quoted EDITOR = %q, %v", got, err)
	}
	if _, err := ResolveEditor(func(k string) string { return `"broken` }, look); err == nil || !strings.Contains(err.Error(), "$VISUAL") {
		t.Errorf("unterminated quote: %v", err)
	}
}

func TestChangedFields(t *testing.T) {
	old := EditFields{Title: "a", Description: "d\n", Notes: ""}
	if got := changedFields(old, EditFields{Title: "a", Description: "d", Notes: ""}); got != "" {
		t.Errorf("trailing newline counted as a change: %q", got)
	}
	if got := changedFields(old, EditFields{Title: "b", Description: "d", Notes: "n"}); got != "title, notes" {
		t.Errorf("changed = %q", got)
	}
}
