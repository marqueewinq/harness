package internal

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:embed skills/harness-workflow.md
var harnessWorkflowSkill string

//go:embed skills/harness-planning.md
var harnessPlanningSkill string

type permRule struct {
	Pattern string
	Action  string
}

// opencode applies the last matching bash rule, so every deny must come after
// the broad allows ("*", "harness *") it carves an exception from.
var opencodeBashRules = []permRule{
	{"*", "allow"},
	{"git status*", "allow"},
	{"git log*", "allow"},
	{"git diff*", "allow"},
	{"git branch*", "allow"},
	{"git add*", "allow"},
	{"git commit*", "allow"},
	{"harness *", "allow"},
	{"git push*", "deny"},
	{"git merge*", "deny"},
	{"git rebase*", "deny"},
	{"git checkout*", "deny"},
	{"git stash*", "deny"},
	{"git replace*", "deny"},
	{"git update-ref*", "deny"},
	{"git tag*", "deny"},
	{"harness review approve*", "deny"},
	{"harness review reject*", "deny"},
	{"harness queue clear*", "deny"},
	{"sqlite3*", "deny"},
	{"*harness.db*", "deny"},
	{"rm *.harness*", "deny"},
	{"mv *.harness*", "deny"},
	{"cp *.harness*", "deny"},
	{"*> .harness*", "deny"},
	{"*>.harness*", "deny"},
	{"*tee *.harness*", "deny"},
}

var claudeDenyRules = []string{
	"Bash(harness review approve:*)",
	"Bash(harness review reject:*)",
	"Bash(harness queue clear:*)",
	"Bash(sqlite3:*)",
	"Bash(git push:*)",
	"Bash(git merge:*)",
	"Bash(git rebase:*)",
	"Bash(git checkout:*)",
	"Bash(git stash:*)",
	"Bash(git replace:*)",
	"Bash(git update-ref:*)",
	"Bash(git tag:*)",
}

const reviewerDenyNote = "These denies also bind a reviewer agent launched in this repo by review.cmd:\nit must run with a config that does not load them, or it cannot record a verdict."

func requireHarnessConfig(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".harness.yaml")); os.IsNotExist(err) {
		return fmt.Errorf("no .harness.yaml found — run 'harness init' first")
	}
	return nil
}

func RunSetupOpencode(dir string) error {
	if err := requireHarnessConfig(dir); err != nil {
		return err
	}
	ocPath := filepath.Join(dir, "opencode.json")
	existing, err := readOptional(ocPath)
	if err != nil {
		return err
	}
	data, err := buildOpencodeConfig(existing)
	if err != nil {
		return fmt.Errorf("%s: %w", ocPath, err)
	}
	if err := os.WriteFile(ocPath, data, 0644); err != nil {
		return fmt.Errorf("writing opencode.json: %w", err)
	}

	verb := "Created"
	if existing != nil {
		verb = "Updated (other keys kept)"
	}
	fmt.Printf("%s %s\n\n", verb, ocPath)
	fmt.Println("Bash permissions (last match wins):")
	for _, r := range opencodeBashRules {
		fmt.Printf("  %-5s %s\n", strings.ToUpper(r.Action), r.Pattern)
	}
	fmt.Println()
	fmt.Println("The agent must use 'harness' for all git-mutating operations.")
	fmt.Println(reviewerDenyNote)
	return installSkills(os.Stdout)
}

func RunSetupClaude(dir string) error {
	if err := requireHarnessConfig(dir); err != nil {
		return err
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	existing, err := readOptional(settingsPath)
	if err != nil {
		return err
	}
	data, added, err := buildClaudeSettings(existing)
	if err != nil {
		return fmt.Errorf("%s: %w", settingsPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", settingsPath, err)
	}

	fmt.Printf("Wrote %s (other keys kept)\n\n", settingsPath)
	fmt.Println("permissions.deny:")
	addedSet := map[string]bool{}
	for _, r := range added {
		addedSet[r] = true
	}
	for _, r := range claudeDenyRules {
		status := "already present"
		if addedSet[r] {
			status = "added"
		}
		fmt.Printf("  %s (%s)\n", r, status)
	}
	fmt.Println()
	fmt.Println(reviewerDenyNote)
	fmt.Println("For a claude reviewer, e.g.: claude -p --setting-sources user ...")
	return installSkills(os.Stdout)
}

func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// buildOpencodeConfig merges harness's bash rules into an existing
// opencode.json (nil for none). Unrelated keys and the user's own bash
// patterns are kept; harness-managed patterns are overwritten.
func buildOpencodeConfig(existing []byte) ([]byte, error) {
	top, err := parseOrderedObject(existing)
	if err != nil {
		return nil, err
	}
	if _, ok := top.get("$schema"); !ok {
		top = append(orderedObject{{"$schema", mustJSON("https://opencode.ai/config.json")}}, top...)
	}
	perm, err := top.object("permission")
	if err != nil {
		return nil, err
	}
	oldBash, err := perm.object("bash")
	if err != nil {
		return nil, err
	}

	managed := map[string]bool{}
	for _, r := range opencodeBashRules {
		managed[r.Pattern] = true
	}
	// "*" stays first so the user's own patterns still override it.
	bash := orderedObject{{opencodeBashRules[0].Pattern, mustJSON(opencodeBashRules[0].Action)}}
	for _, f := range oldBash {
		if !managed[f.Key] {
			bash = append(bash, f)
		}
	}
	for _, r := range opencodeBashRules[1:] {
		bash = append(bash, jsonField{r.Pattern, mustJSON(r.Action)})
	}

	if err := perm.setValue("bash", bash); err != nil {
		return nil, err
	}
	if err := top.setValue("permission", perm); err != nil {
		return nil, err
	}
	return encodeJSON(top)
}

// buildClaudeSettings adds missing harness rules to permissions.deny of an
// existing .claude/settings.json (nil for none), keeping everything else.
func buildClaudeSettings(existing []byte) (data []byte, added []string, err error) {
	top, err := parseOrderedObject(existing)
	if err != nil {
		return nil, nil, err
	}
	perms, err := top.object("permissions")
	if err != nil {
		return nil, nil, err
	}
	var deny []json.RawMessage
	if raw, ok := perms.get("deny"); ok {
		if err := json.Unmarshal(raw, &deny); err != nil {
			return nil, nil, fmt.Errorf("permissions.deny is not an array: %w", err)
		}
	}
	present := map[string]bool{}
	for _, raw := range deny {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			present[s] = true
		}
	}
	for _, r := range claudeDenyRules {
		if !present[r] {
			deny = append(deny, mustJSON(r))
			added = append(added, r)
		}
	}
	if deny == nil {
		deny = []json.RawMessage{}
	}
	if err := perms.setValue("deny", deny); err != nil {
		return nil, nil, err
	}
	if err := top.setValue("permissions", perms); err != nil {
		return nil, nil, err
	}
	data, err = encodeJSON(top)
	return data, added, err
}

type jsonField struct {
	Key   string
	Value json.RawMessage
}

// orderedObject keeps key order: opencode evaluates bash rules in file order,
// and rewriting a user's file should not reshuffle it.
type orderedObject []jsonField

func parseOrderedObject(data []byte) (orderedObject, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return orderedObject{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected a JSON object")
	}
	obj := orderedObject{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("invalid JSON at %q: %w", key, err)
		}
		obj = append(obj, jsonField{key, raw})
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("invalid JSON: trailing data after object")
	}
	return obj, nil
}

func (o orderedObject) get(key string) (json.RawMessage, bool) {
	for _, f := range o {
		if f.Key == key {
			return f.Value, true
		}
	}
	return nil, false
}

// object returns the nested object at key, or an empty one when absent.
func (o orderedObject) object(key string) (orderedObject, error) {
	raw, ok := o.get(key)
	if !ok {
		return orderedObject{}, nil
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return nil, fmt.Errorf("%q is not a JSON object; fix it by hand", key)
	}
	return parseOrderedObject(raw)
}

func (o *orderedObject) setValue(key string, v any) error {
	raw, err := marshalRaw(v)
	if err != nil {
		return err
	}
	for i := range *o {
		if (*o)[i].Key == key {
			(*o)[i].Value = raw
			return nil
		}
	}
	*o = append(*o, jsonField{key, raw})
	return nil
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := marshalRaw(f.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(f.Value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Encoding keeps '>' and '&' literal (json.Marshal would escape them): they
// appear in shell patterns that people read and edit.
func encodeJSON(v any) ([]byte, error) {
	return encodeWith(v, "  ")
}

func marshalRaw(v any) (json.RawMessage, error) {
	b, err := encodeWith(v, "")
	return bytes.TrimSuffix(b, []byte("\n")), err
}

func encodeWith(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func mustJSON(s string) json.RawMessage {
	raw, _ := marshalRaw(s)
	return raw
}

func installSkills(w io.Writer) error {
	// OpenCode reads skills from ~/.claude/skills/ (Claude Code compatibility).
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	fmt.Fprintln(w)
	for _, s := range []struct{ name, content string }{
		{"harness-workflow", harnessWorkflowSkill},
		{"harness-planning", harnessPlanningSkill},
	} {
		path := filepath.Join(home, ".claude", "skills", s.name, "SKILL.md")
		if err := installSkill(w, path, s.content); err != nil {
			return fmt.Errorf("installing skill %s: %w", s.name, err)
		}
	}
	return nil
}

// installSkill never replaces a differing skill silently: the previous
// version is kept as SKILL.md.bak and the replacement is announced.
func installSkill(w io.Writer, path, content string) error {
	old, err := readOptional(path)
	if err != nil {
		return err
	}
	switch {
	case old == nil:
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		fmt.Fprintf(w, "Installed skill: %s\n", path)
	case string(old) == content:
		fmt.Fprintf(w, "Skill up to date: %s\n", path)
		return nil
	default:
		if err := os.WriteFile(path+".bak", old, 0644); err != nil {
			return err
		}
		fmt.Fprintf(w, "Replaced skill: %s (it differed; previous version saved to %s.bak)\n", path, path)
	}
	return os.WriteFile(path, []byte(content), 0644)
}
