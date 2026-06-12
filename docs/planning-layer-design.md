# Harness Planning Layer -- Design Spec

## Overview

Add a planning layer to harness: Projects, Epics, and planned Tasks with
dependencies. This lets users brainstorm and decompose work with an agent, then
dispatch separate agents to execute individual tasks through the existing
harness workflow.

**Approach:** Extend the existing Task model (Unified Entity). Projects and
Epics are planning-only containers (no git branches). Tasks gain a `PLANNED`
state before `IN_PROGRESS`. A new `harness start` command allocates git
resources when a planned task begins execution.

## Data Model

### New Tables

```sql
CREATE TABLE projects (
    id TEXT PRIMARY KEY,              -- "proj-001", "proj-002", ...
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open', -- "open" or "closed"
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE epics (
    id TEXT PRIMARY KEY,              -- "epic-001", "epic-002", ...
    project_id TEXT NOT NULL REFERENCES projects(id),
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open', -- "open" or "closed"
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE task_dependencies (
    from_task_id TEXT NOT NULL REFERENCES tasks(id),
    to_task_id TEXT NOT NULL REFERENCES tasks(id),
    dep_type TEXT NOT NULL DEFAULT 'blocks', -- "blocks" or "related"
    PRIMARY KEY (from_task_id, to_task_id)
);
```

### Task Table Migration

New column:

```sql
ALTER TABLE tasks ADD COLUMN epic_id TEXT DEFAULT NULL;
```

Schema change: `branch` and `worktree_path` become nullable. PLANNED tasks
have NULL for both; `harness start` fills them in. Since SQLite cannot alter
column constraints in-place, this requires a table rebuild migration (see
Schema Versioning below).

### Schema Versioning

Add a `schema_version` table to track migrations:

```sql
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
```

The table rebuild migration for making `branch`/`worktree_path` nullable:
1. Create `tasks_new` with nullable `branch` (still UNIQUE) and `worktree_path`
2. INSERT INTO tasks_new SELECT ... FROM tasks
3. DROP TABLE tasks
4. ALTER TABLE tasks_new RENAME TO tasks

SQLite allows multiple NULLs in a UNIQUE column, so multiple PLANNED tasks
(with NULL branch) do not conflict.

### ID Generation

Same pattern as existing tasks: sequential counters per entity type.
- `proj-001`, `proj-002`, ...
- `epic-001`, `epic-002`, ...
- `task-001`, `task-002`, ... (unchanged)

## State Machine

Add `PLANNED` state:

```go
const StatePlanned TaskState = "PLANNED"
```

New transitions:

```go
StatePlanned: {StateInProgress, StateClosed},
```

All other transitions unchanged. `harness plan` creates tasks in `PLANNED`.
`harness start` transitions `PLANNED -> IN_PROGRESS`.

`harness new` still creates tasks directly at `IN_PROGRESS` (unchanged).

## CLI Commands

### New Commands

| Command | Purpose |
|---|---|
| `harness project new "title"` | Create a project |
| `harness project list` | List projects with progress |
| `harness project status <id>` | Project details + epics + tasks |
| `harness project close <id>` | Close project (warns if open tasks remain) |
| `harness epic new "title" --project <id>` | Create epic under a project |
| `harness epic list [--project <id>]` | List epics, optionally filtered |
| `harness epic status <id>` | Epic details + tasks |
| `harness epic close <id>` | Close epic (warns if open tasks remain) |
| `harness plan "description" [--epic <id>]` | Create task in PLANNED state |
| `harness start <task-id> [--base <branch>]` | Allocate branch+worktree, PLANNED -> IN_PROGRESS |
| `harness ready` | List unblocked PLANNED tasks |
| `harness dep add <from> <to> [--type blocks\|related]` | Add dependency |
| `harness dep rm <from> <to>` | Remove dependency |

### Modified Commands

- `harness list` -- gains `--project` and `--epic` filters. Without filters,
  shows 3-level hierarchy (projects > epics > tasks), with non-epic tasks at
  the bottom.
- `harness status <task-id>` -- shows epic/project context and dependency info
  in addition to current output.

### Unchanged Commands

`harness new`, `harness check`, `harness diff`, `harness request-review`,
`harness record-review`, `harness review-history`, `harness merge`,
`harness close`, `harness init` -- all unchanged.

## Dependency System

### Semantics

- `blocks`: `harness dep add A B` means "A blocks B" -- B should not start
  until A is resolved (MERGED or CLOSED).
- `related`: informational link, no blocking behavior.

### Cycle Detection

On `harness dep add`, walk the `blocks` graph from `to_task_id` to check if
`from_task_id` is reachable. If so, reject with:
`"circular dependency detected: A -> B -> ... -> A"`.

Self-dependencies rejected: `"a task cannot depend on itself"`.

Duplicate inserts are idempotent (silently succeed).

### `harness ready`

Computes unblocked PLANNED tasks:
1. Select all tasks where `state = 'PLANNED'`
2. For each, check all `blocks`-type dependencies where this task is
   `to_task_id`
3. A task is ready if all blockers have `state` in (`MERGED`, `CLOSED`)
4. Output: ready tasks first, then blocked tasks with their blockers listed

## Error Handling

| Situation | Behavior |
|---|---|
| `harness plan` without `--epic` | Allowed. Creates orphan planned task. |
| `harness plan --epic <bad-id>` | Error: `"epic not found"` |
| `harness epic new` without `--project` | Error: `"--project is required"` |
| `harness epic new --project <bad-id>` | Error: `"project not found"` |
| `harness start` on non-PLANNED task | Error: `"task is <state>, not PLANNED"` |
| `harness start` on blocked task | Warning + proceeds |
| `harness dep add` creating cycle | Error: `"circular dependency detected"` |
| `harness dep add` self-dependency | Error: `"a task cannot depend on itself"` |
| `harness dep add` duplicate | Silently succeeds (idempotent) |
| `harness project close` with open tasks | Warning + closes |
| `harness epic close` with open tasks | Warning + closes |
| Creating epic under closed project | Warning + allows |
| Creating task under closed epic | Warning + allows |
| `harness close` on PLANNED task | Allowed. PLANNED -> CLOSED. No cleanup. |

## Agent Skills

`harness setup opencode` installs two skills:

### `harness-workflow` (updated)

Add to "At Session Start" section:

> If told to work on a specific task, run `harness start <task-id>` to begin.
> If the task is already IN_PROGRESS, continue in its worktree.

Add `harness start` to the commands table.

### `harness-planning` (new)

Trigger: "Use when discussing strategy, decomposing work, or creating
projects, epics, and tasks. Use at the start of planning sessions."

Content:

```
# Harness Planning

Use when decomposing work into projects, epics, and tasks.

## Workflow

1. harness project list -- see existing projects
2. harness project new "title" -- create project
3. harness epic new "title" --project <id> -- create epics
4. harness plan "description" --epic <id> -- create planned tasks
5. harness dep add <from> <to> -- add blockers
6. harness ready -- see what's unblocked

## Rules

- Add dependencies when task B cannot start until task A is merged
- Use harness ready to find the next task to work on

## Handing Off to Execution

harness start <task-id> -- creates branch+worktree, then follow harness-workflow
```

## Orthogonality

The project/epic hierarchy (`epic_id`) is organizational grouping. The existing
parent-child relationship (`parent_task_id` + `base_branch` via `--base`) is
git branch topology. They are independent:

- A task can belong to an epic AND be a subtask of another task (branching off
  its feature branch)
- A task can belong to an epic with no parent task (branches from main)
- A task can have no epic and no parent (the current `harness new` flow)
