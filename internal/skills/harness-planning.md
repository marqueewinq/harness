---
name: harness-planning
description: >
  Use when discussing strategy, decomposing work, or creating projects, epics, and tasks.
  Use at the start of planning sessions.
---

# Harness Planning

Use when decomposing work into projects, epics, and tasks.

## Workflow

1. `harness project list` — see existing projects
2. `harness project new "title"` — create project
3. `harness epic new "title" --project <id>` — create epics
4. `harness task new "title" --epic <id>` — create planned tasks
5. `harness task dep add <from> <to>` — add blockers between tasks
6. `harness task list --ready` — see what is unblocked and ready to start

## Rules

- Add dependencies when task B cannot start until task A is merged
- Use `harness task list --ready` to find the next task to work on

## Handing Off to Execution

`harness task start <task-id>` — creates branch + worktree, then follow harness-workflow

## Commands Reference

| Command | Purpose |
|---|---|
| `harness project new "title"` | Create a project |
| `harness project list` | List projects with progress |
| `harness project view <id>` | Project details + epics + tasks |
| `harness project close <id>` | Close a project |
| `harness epic new "title" --project <id> [--review default\|human\|none]` | Create epic under project; review type is fixed at creation (`none` needs `review.allow_none: true` on main) |
| `harness epic list [--project <id>]` | List epics |
| `harness epic view <id>` | Epic details + tasks |
| `harness epic close <id>` | Close an epic |
| `harness epic sync <id> [--continue\|--abort]` | Rebase the epic branch onto main (then `work sync` its in-flight tasks) |
| `harness task new "title" [--epic <id>] [--description "..."] [--review default\|human\|none]` | Create planned task; review type is fixed at creation |
| `harness task start <task-id> [--base <branch>]` | Start a planned task |
| `harness task list --ready` | List unblocked planned tasks |
| `harness task dep add <from> <to>` | Add blocker dependency |
| `harness task dep rm <from> <to>` | Remove dependency |
