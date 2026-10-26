package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppFromTaskWorktree(t *testing.T) {
	t.Parallel()
	app, dir, task := setupTask(t)
	defer app.Close()

	wtApp, err := LoadApp(task.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	defer wtApp.Close()

	if wtApp.RepoRoot != dir {
		t.Errorf("RepoRoot = %q, want main root %q", wtApp.RepoRoot, dir)
	}
	if _, err := os.Stat(filepath.Join(task.WorktreePath, ".harness")); !os.IsNotExist(err) {
		t.Errorf("LoadApp created .harness inside the worktree (stat err = %v)", err)
	}
	id, err := wtApp.ResolveTaskID("")
	if err != nil {
		t.Fatal(err)
	}
	if id != task.ID {
		t.Errorf("ResolveTaskID from worktree = %q, want %q", id, task.ID)
	}
}

func TestLoadAppResolvesSymlinks(t *testing.T) {
	t.Parallel()
	app, dir, task := setupTask(t)
	defer app.Close()

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(dir, task.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, wd := range []string{link, filepath.Join(link, rel)} {
		got, err := LoadApp(wd)
		if err != nil {
			t.Fatalf("LoadApp(%s): %v", wd, err)
		}
		got.Close()
		if got.RepoRoot != dir {
			t.Errorf("LoadApp(%s).RepoRoot = %q, want %q", wd, got.RepoRoot, dir)
		}
	}
	if _, err := os.Stat(filepath.Join(task.WorktreePath, ".harness")); !os.IsNotExist(err) {
		t.Errorf("LoadApp via symlink created .harness inside the worktree (stat err = %v)", err)
	}
}

func TestLoadAppFromSubdirKeepsWorkDir(t *testing.T) {
	t.Parallel()
	app, dir := setupApp(t)
	defer app.Close()

	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	got, err := LoadApp(sub)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	if got.RepoRoot != dir || got.WorkDir != sub {
		t.Errorf("RepoRoot, WorkDir = %q, %q; want %q, %q", got.RepoRoot, got.WorkDir, dir, sub)
	}
}

// Not parallel: t.Chdir changes the process cwd.
func TestFindRepoRootFromTaskWorktree(t *testing.T) {
	app, dir, task := setupTask(t)
	defer app.Close()

	sub := filepath.Join(task.WorktreePath, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	root, err := FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Errorf("FindRepoRoot from worktree = %q, want main root %q", root, dir)
	}
	if _, err := os.Stat(filepath.Join(task.WorktreePath, ".harness")); !os.IsNotExist(err) {
		t.Errorf("worktree .harness should not exist (stat err = %v)", err)
	}
}

func TestFindRepoRootFromMainSubdir(t *testing.T) {
	app, dir := setupApp(t)
	defer app.Close()

	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	root, err := FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Errorf("FindRepoRoot from main subdir = %q, want %q", root, dir)
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()
	got := Slugify("Hello World")
	want := "hello-world"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "Hello World", got, want)
	}
}

func TestSlugifyEmpty(t *testing.T) {
	t.Parallel()
	got := Slugify("")
	if got != "" {
		t.Errorf("Slugify(%q) = %q, want %q", "", got, "")
	}
}

func TestSlugifySpecialChars(t *testing.T) {
	t.Parallel()
	got := Slugify("Hello, World! #1")
	want := "hello-world-1"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "Hello, World! #1", got, want)
	}
}

func TestSlugifyLong(t *testing.T) {
	t.Parallel()
	input := "this is a very long string that should definitely be truncated because it exceeds sixty characters in length"
	got := Slugify(input)
	if len(got) > 60 {
		t.Errorf("Slugify long string: len = %d, want <= 60", len(got))
	}
}

func TestSlugifyUnicode(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	got := Slugify("already-slugged")
	want := "already-slugged"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "already-slugged", got, want)
	}
}

func TestSlugifyLeadingTrailingSpecial(t *testing.T) {
	t.Parallel()
	got := Slugify("---hello---")
	want := "hello"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "---hello---", got, want)
	}
}

func TestBuildCommitMessage(t *testing.T) {
	t.Parallel()
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
		{
			name:     "brackets in message preserved",
			template: "[{{proj}}] [{{epic}}] [{{task}}] {{message}}",
			taskID:   "task-003",
			epicID:   "",
			projID:   "",
			message:  "fix empty array [] in parser",
			want:     "[task-003] fix empty array [] in parser",
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
