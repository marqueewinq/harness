# Commit Message Template

## Problem

Commits made inside harness worktrees have no link back to the task/epic/project they belong to. Hard to trace work in git log.

## Solution

Configurable commit message template applied via `harness work commit` command.

## Config

New `commit` section in `.harness.yaml`:

```yaml
commit:
  message: "[{{proj}}] [{{epic}}] [{{task}}] {{message}}"
```

### Template variables

| Variable | Resolves to | Example | When missing |
|---|---|---|---|
| `{{proj}}` | Project ID | `proj-001` | empty string |
| `{{epic}}` | Epic ID | `epic-002` | empty string |
| `{{task}}` | Task ID | `task-003` | always present |
| `{{message}}` | User-supplied commit message | `fix auth bug` | required |

### Empty variable cleanup

After template rendering, any `[<whitespace-only>]` blocks are stripped via regex `\[\s*\]\s*`. This handles missing hierarchy levels gracefully.

Examples with `message: "[{{proj}}] [{{epic}}] [{{task}}] {{message}}"`:
- Full hierarchy: `[proj-001] [epic-002] [task-003] fix auth bug`
- No project: `[epic-002] [task-003] fix auth bug`
- No epic/project: `[task-003] fix auth bug`

### Default

When `commit.message` is not configured: `"{{message}}"` (pass-through, backward compatible).

## Command

```
harness work commit [-m "msg"] [task-id]
```

Lives under existing `work` subcommand group alongside `status`, `check`, `diff`, `merge`, `close`.

Behavior:
1. Resolve task from worktree path (or explicit task-id argument)
2. Look up epic via `task.EpicID`, then project via `epic.ProjectID`
3. Render `commit.message` template with resolved IDs
4. Delegate to `git commit -m <rendered>` (with `-m`) or write template to git commit message file and open editor (without `-m`)

## Squash merge

`harness work merge` with `strategy: squash` also renders the commit message through the same template. Currently uses raw `task.Description` as the message -- will pass it through the template instead.

## Changes

1. **`internal/config.go`** -- add `CommitConfig` struct (`Message string`), add `Commit CommitConfig` field to `Config`
2. **New `internal/cmd_commit.go`** -- `CmdWorkCommit` method on `App`. Resolves task, renders template, runs `git commit`
3. **`main.go`** -- register `commit` subcommand under `work` group
4. **`internal/cmd_merge.go`** -- squash merge path uses same template rendering
5. **Skill text** -- update harness-workflow skill to tell agents about `harness work commit`
