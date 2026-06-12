# Planning Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Projects, Epics, planned Tasks, and dependencies to harness so users can decompose work and dispatch agents to individual tasks.

**Architecture:** Extend the existing Task model with a PLANNED state. Add Project and Epic as lightweight planning-only tables. Add a dependency graph between tasks with cycle detection. New CLI commands for planning; existing execution commands unchanged.

**Tech Stack:** Go, cobra (CLI), modernc.org/sqlite, existing harness patterns

**Spec:** `docs/planning-layer-design.md`

---

## File Structure

### New Files

| File | Responsibility |
|---|---|
| `internal/project_repo.go` | Project model + CRUD (Create, Get, List, UpdateStatus) |
| `internal/epic_repo.go` | Epic model + CRUD (Create, Get, List, ListByProject, UpdateStatus) |
| `internal/dep_repo.go` | TaskDependency model + CRUD + cycle detection |
| `internal/cmd_project.go` | CmdProjectNew, CmdProjectList, CmdProjectStatus, CmdProjectClose |
| `internal/cmd_epic.go` | CmdEpicNew, CmdEpicList, CmdEpicStatus, CmdEpicClose |
| `internal/cmd_plan.go` | CmdPlan (create PLANNED task) |
| `internal/cmd_start.go` | CmdStart (PLANNED -> IN_PROGRESS, allocate branch+worktree) |
| `internal/cmd_ready.go` | CmdReady (list unblocked PLANNED tasks) |
| `internal/cmd_dep.go` | CmdDepAdd, CmdDepRm |

### Modified Files

| File | Changes |
|---|---|
| `internal/db.go` | Schema versioning, new tables, task table rebuild migration |
| `internal/state.go` | Add `StatePlanned` + transition |
| `internal/task_repo.go` | Add `EpicID` field, handle nullable `Branch`/`WorktreePath`, `CreatePlannedTask` |
| `internal/cmd_list.go` | 3-level hierarchy display, `--project`/`--epic` filters |
| `internal/cmd_status.go` | Show epic/project context, dependencies |
| `internal/cmd_setup.go` | Install `harness-planning` skill, update `harness-workflow` skill |
| `main.go` | Register new cobra commands |

### Test Files

| File | What's Tested |
|---|---|
| `internal/state_test.go` | PLANNED transitions |
| `internal/dep_repo_test.go` | Cycle detection unit tests |
| `internal/e2e_test.go` | Full planning workflow E2E |

---

## Task Order

Tasks must be implemented in order. Each task builds on the previous.

| Task | File | Description |
|---|---|---|
| 1 | `planning-layer-task-01-schema.md` | Schema versioning + DB migrations |
| 2 | `planning-layer-task-02-state.md` | PLANNED state + task model changes |
| 3 | `planning-layer-task-03-project.md` | Project model + CRUD + CLI |
| 4 | `planning-layer-task-04-epic.md` | Epic model + CRUD + CLI |
| 5 | `planning-layer-task-05-deps.md` | Dependency system + CLI |
| 6 | `planning-layer-task-06-plan-start-ready.md` | plan, start, ready commands |
| 7 | `planning-layer-task-07-list-status.md` | Updated list + status commands |
| 8 | `planning-layer-task-08-setup-wiring.md` | Skills, setup, main.go wiring |
| 9 | `planning-layer-task-09-e2e.md` | E2E tests for planning workflow |
