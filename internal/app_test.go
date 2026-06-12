package internal

import "testing"

func TestSlugify(t *testing.T) {
	got := Slugify("Hello World")
	want := "hello-world"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "Hello World", got, want)
	}
}

func TestSlugifyEmpty(t *testing.T) {
	got := Slugify("")
	if got != "" {
		t.Errorf("Slugify(%q) = %q, want %q", "", got, "")
	}
}

func TestSlugifySpecialChars(t *testing.T) {
	got := Slugify("Hello, World! #1")
	want := "hello-world-1"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "Hello, World! #1", got, want)
	}
}

func TestSlugifyLong(t *testing.T) {
	input := "this is a very long string that should definitely be truncated because it exceeds sixty characters in length"
	got := Slugify(input)
	if len(got) > 60 {
		t.Errorf("Slugify long string: len = %d, want <= 60", len(got))
	}
}

func TestSlugifyUnicode(t *testing.T) {
	got := Slugify("café résumé")
	// non-ascii stripped, only a-z0-9 kept; accented chars become separator
	// "café résumé" -> "caf-r-sum"
	if got == "" {
		t.Error("Slugify unicode: got empty string")
	}
	for _, c := range got {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			t.Errorf("Slugify unicode: got invalid char %q in %q", string(c), got)
		}
	}
}

func TestSlugifyAlreadySlugged(t *testing.T) {
	got := Slugify("already-slugged")
	want := "already-slugged"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "already-slugged", got, want)
	}
}

func TestSlugifyLeadingTrailingSpecial(t *testing.T) {
	got := Slugify("---hello---")
	want := "hello"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "---hello---", got, want)
	}
}

func TestBuildCommitMessage(t *testing.T) {
	tests := []struct {
		name     string
		template string
		taskID   string
		epicID   string
		projID   string
		message  string
		want     string
	}{
		{
			name:     "full hierarchy",
			template: "[{{proj}}] [{{epic}}] [{{task}}] {{message}}",
			taskID:   "task-003",
			epicID:   "epic-002",
			projID:   "proj-001",
			message:  "fix auth bug",
			want:     "[proj-001] [epic-002] [task-003] fix auth bug",
		},
		{
			name:     "no project",
			template: "[{{proj}}] [{{epic}}] [{{task}}] {{message}}",
			taskID:   "task-003",
			epicID:   "epic-002",
			projID:   "",
			message:  "fix auth bug",
			want:     "[epic-002] [task-003] fix auth bug",
		},
		{
			name:     "no epic or project",
			template: "[{{proj}}] [{{epic}}] [{{task}}] {{message}}",
			taskID:   "task-003",
			epicID:   "",
			projID:   "",
			message:  "fix auth bug",
			want:     "[task-003] fix auth bug",
		},
		{
			name:     "passthrough default",
			template: "{{message}}",
			taskID:   "task-001",
			epicID:   "",
			projID:   "",
			message:  "just a message",
			want:     "just a message",
		},
		{
			name:     "custom format",
			template: "{{task}}: {{message}}",
			taskID:   "task-005",
			epicID:   "",
			projID:   "",
			message:  "refactor",
			want:     "task-005: refactor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildCommitMessage(tt.template, tt.taskID, tt.epicID, tt.projID, tt.message)
			if got != tt.want {
				t.Errorf("BuildCommitMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
