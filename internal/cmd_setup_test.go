package internal

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// opencodeAction mirrors opencode's bash permission check: '*' and '?' globs
// over the whole command, last matching rule wins.
func opencodeAction(t *testing.T, rules orderedObject, command string) string {
	t.Helper()
	action := ""
	for _, f := range rules {
		re := "^" + strings.NewReplacer(`\*`, ".*", `\?`, ".").Replace(regexp.QuoteMeta(f.Key)) + "$"
		if regexp.MustCompile("(?s)" + re).MatchString(command) {
			if err := json.Unmarshal(f.Value, &action); err != nil {
				t.Fatal(err)
			}
		}
	}
	return action
}

func generatedBashRules(t *testing.T, existing []byte) (orderedObject, orderedObject) {
	t.Helper()
	data, err := buildOpencodeConfig(existing)
	if err != nil {
		t.Fatal(err)
	}
	top, err := parseOrderedObject(data)
	if err != nil {
		t.Fatalf("generated config is not valid JSON: %v\n%s", err, data)
	}
	perm, err := top.object("permission")
	if err != nil {
		t.Fatal(err)
	}
	bash, err := perm.object("bash")
	if err != nil {
		t.Fatal(err)
	}
	return top, bash
}

func TestOpencodeBashRulesDecisions(t *testing.T) {
	t.Parallel()
	_, bash := generatedBashRules(t, nil)

	cases := map[string]string{
		"ls -la":                                     "allow",
		"git add -A":                                 "allow",
		"git commit -m 'msg'":                        "allow",
		"git status":                                 "allow",
		"harness work check":                         "allow",
		"harness review request task-1":              "allow",
		"harness review history":                     "allow",
		"harness review approve task-1":              "deny",
		"harness review reject --comment x":          "deny",
		"harness queue clear task-1 --force":         "deny",
		"sqlite3 .harness/harness.db 'delete'":       "deny",
		"python3 -c 'open(\".harness/harness.db\")'": "deny",
		"rm -rf .harness":                            "deny",
		"rm .harness.yaml":                           "deny",
		"mv x .harness.yaml":                         "deny",
		"echo 'review: {}' > .harness.yaml":          "deny",
		"echo x >>.harness.yaml":                     "deny",
		"echo x | tee .harness.yaml":                 "deny",
		"git push origin main":                       "deny",
		"git merge feature":                          "deny",
		"git rebase main":                            "deny",
		"git checkout main":                          "deny",
		"git stash":                                  "deny",
		"git replace abc def":                        "deny",
		"git update-ref refs/heads/main abc":         "deny",
		"git tag main abc":                           "deny",
	}
	for cmd, want := range cases {
		if got := opencodeAction(t, bash, cmd); got != want {
			t.Errorf("%q -> %q, want %q", cmd, got, want)
		}
	}
}

func TestBuildOpencodeConfigKeepsUnrelatedKeys(t *testing.T) {
	t.Parallel()
	existing := []byte(`{
  "model": "anthropic/x",
  "permission": {
    "edit": "ask",
    "bash": {
      "*": "ask",
      "npm publish*": "deny",
      "git push*": "allow",
      "harness review approve*": "allow"
    }
  },
  "mcp": {"a": {"type": "local"}}
}`)
	top, bash := generatedBashRules(t, existing)

	var keys []string
	for _, f := range top {
		keys = append(keys, f.Key)
	}
	if got, want := strings.Join(keys, ","), "$schema,model,permission,mcp"; got != want {
		t.Errorf("top-level keys = %s, want %s", got, want)
	}
	if raw, _ := top.get("model"); string(raw) != `"anthropic/x"` {
		t.Errorf("model = %s", raw)
	}
	if raw, ok := top.get("mcp"); !ok || !strings.Contains(string(raw), "local") {
		t.Errorf("mcp lost: %s", raw)
	}
	perm, _ := top.object("permission")
	if raw, _ := perm.get("edit"); string(raw) != `"ask"` {
		t.Errorf("permission.edit = %s, want \"ask\"", raw)
	}
	if bash[0].Key != "*" || bash[1].Key != "npm publish*" {
		t.Errorf("want '*' then user rule first, got %s, %s", bash[0].Key, bash[1].Key)
	}
	for cmd, want := range map[string]string{
		"npm publish":                   "deny",
		"git push":                      "deny",
		"harness review approve task-1": "deny",
		"ls":                            "allow",
	} {
		if got := opencodeAction(t, bash, cmd); got != want {
			t.Errorf("%q -> %q, want %q", cmd, got, want)
		}
	}

	again, err := buildOpencodeConfig(mustBuild(t, existing))
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(mustBuild(t, existing)) {
		t.Errorf("not idempotent:\n%s", again)
	}
}

func mustBuild(t *testing.T, existing []byte) []byte {
	t.Helper()
	data, err := buildOpencodeConfig(existing)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBuildOpencodeConfigRejectsBadInput(t *testing.T) {
	t.Parallel()
	for _, in := range []string{`{`, `[]`, `{"permission": "allow"}`, `{"permission": {"bash": "ask"}}`, `{} {}`} {
		if _, err := buildOpencodeConfig([]byte(in)); err == nil {
			t.Errorf("buildOpencodeConfig(%s) should fail rather than overwrite", in)
		}
	}
}

func TestCommittedOpencodeJSONMatchesGenerator(t *testing.T) {
	t.Parallel()
	committed, err := os.ReadFile(filepath.Join("..", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want := mustBuild(t, nil); string(committed) != string(want) {
		t.Errorf("opencode.json is stale; want:\n%s", want)
	}
}

func TestBuildClaudeSettingsMergesDeny(t *testing.T) {
	t.Parallel()
	existing := []byte(`{
  "model": "opus",
  "permissions": {
    "allow": ["Bash(ls:*)"],
    "deny": ["Read(./secrets)", "Bash(git push:*)"]
  },
  "hooks": {"Stop": []}
}`)
	data, added, err := buildClaudeSettings(existing)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Model       string
		Hooks       map[string]any
		Permissions struct {
			Allow []string
			Deny  []string
		}
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if got.Model != "opus" || got.Hooks == nil || len(got.Permissions.Allow) != 1 {
		t.Errorf("unrelated keys lost:\n%s", data)
	}
	if got.Permissions.Deny[0] != "Read(./secrets)" {
		t.Errorf("existing deny order changed: %v", got.Permissions.Deny)
	}
	count := map[string]int{}
	for _, d := range got.Permissions.Deny {
		count[d]++
	}
	for _, r := range claudeDenyRules {
		if count[r] != 1 {
			t.Errorf("%s appears %d times", r, count[r])
		}
	}
	for _, a := range added {
		if a == "Bash(git push:*)" {
			t.Error("reported an existing rule as added")
		}
	}
	if len(added) != len(claudeDenyRules)-1 {
		t.Errorf("added %d rules, want %d", len(added), len(claudeDenyRules)-1)
	}

	again, added2, err := buildClaudeSettings(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(added2) != 0 || string(again) != string(data) {
		t.Errorf("second merge changed settings (added %v)", added2)
	}
}

func TestBuildClaudeSettingsFromScratch(t *testing.T) {
	t.Parallel()
	data, added, err := buildClaudeSettings(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != len(claudeDenyRules) {
		t.Errorf("added %d, want %d", len(added), len(claudeDenyRules))
	}
	if _, _, err := buildClaudeSettings([]byte(`{"permissions": {"deny": "x"}}`)); err == nil {
		t.Error("non-array deny should fail")
	}
	if !strings.Contains(string(data), `"Bash(harness review approve:*)"`) {
		t.Errorf("missing approve deny:\n%s", data)
	}
}

func TestInstallSkillNoticesDifference(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "skills", "x", "SKILL.md")
	var out strings.Builder

	if err := installSkill(&out, path, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := installSkill(&out, path, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := installSkill(&out, path, "v2"); err != nil {
		t.Fatal(err)
	}
	log := out.String()
	for _, want := range []string{"Installed skill: " + path, "Skill up to date: " + path, "Replaced skill: " + path} {
		if !strings.Contains(log, want) {
			t.Errorf("output missing %q:\n%s", want, log)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != "v2" {
		t.Errorf("skill = %q, want v2", b)
	}
	if b, _ := os.ReadFile(path + ".bak"); string(b) != "v1" {
		t.Errorf("backup = %q, want v1", b)
	}
}

// Not parallel: sets HOME and redirects stdout.
func TestRunSetupWritesBothConfigs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte("main_branch: main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"model": "m"}`), 0644); err != nil {
		t.Fatal(err)
	}

	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	errOC := RunSetupOpencode(dir)
	errCL := RunSetupClaude(dir)
	w.Close()
	os.Stdout = stdout
	printed, _ := io.ReadAll(r)
	if errOC != nil || errCL != nil {
		t.Fatalf("setup: %v / %v", errOC, errCL)
	}

	oc, _ := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if !strings.Contains(string(oc), `"model": "m"`) || !strings.Contains(string(oc), `"sqlite3*": "deny"`) {
		t.Errorf("opencode.json:\n%s", oc)
	}
	cl, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if !strings.Contains(string(cl), "Bash(sqlite3:*)") {
		t.Errorf("settings.json:\n%s", cl)
	}
	for _, s := range []string{"harness-workflow", "harness-planning"} {
		if _, err := os.Stat(filepath.Join(home, ".claude", "skills", s, "SKILL.md")); err != nil {
			t.Errorf("skill %s not installed: %v", s, err)
		}
	}
	if !strings.Contains(string(printed), "Skill up to date") {
		t.Errorf("second setup should report skills as up to date:\n%s", printed)
	}
}
