---
name: harness-workflow
description: >
  Use when starting any development task, implementing features, fixing bugs, or making
  any code changes. Enforces the harness workflow: create task, work in worktree, run checks,
  request review, merge. Use at the start of every session and before any code changes.
---

# Harness Workflow

All workflow operations go through the `harness` CLI. Direct git push, merge,
rebase, and checkout are denied by permission rules. Git add and commit are
allowed (you need them to commit work in the worktree).

## At Session Start

Run `harness work status` or `harness task list --status=all` to see current tasks.
If told to work on a specific task, run `harness task start <task-id>` to begin.
If the task is already IN_PROGRESS, continue in its worktree.

## Workflow

1. `harness new "title"` — creates branch + worktree + task
2. Implement changes in the worktree (path printed by harness new)
3. Use git add + git commit freely inside the worktree
4. `harness work check` — runs configured checks (tests, build, lint as a DAG)
5. `harness review request` — pins the reviewed commit and launches the
   configured reviewer (`review.cmd`), which records the verdict. With
   `review.type: human`, or for a task of review type `human`, no reviewer
   is launched and a person records it from an interactive terminal. A task
   of review type `none` has no review step: after step 4, go to step 8.
6. You cannot approve or reject your own task: verdicts need the one-time
   token harness gives only to the reviewer it launches. The reviewer gets
   no terminal (stdin is /dev/null; the prompt is in `{{prompt_file}}`) and is
   killed after `review.timeout` (default 30m). If it exits without a verdict
   (failed, timeout, interrupted, no verdict), `harness review request` fails,
   the request is closed, and the task stays REVIEW_REQUESTED: run
   `harness work check`, then `harness review request` again.
7. If changes requested: fix, commit, go to step 4
8. `harness work merge` — enters merge queue, rebases, re-runs checks, merges, cleans up

## Conflict Recovery

If the merge fails on conflicts (task is MERGE_FAILED), or you want to pick up
new commits from the base branch, run `harness work sync`. It rebases the task
branch onto its base inside the worktree (commit or discard local changes first).

- Clean rebase: the task returns to IN_PROGRESS; run `harness work check`, then
  request review again before merging.
- Conflicts: the rebase stays in progress and the conflicted files are listed.
  Edit them, `git add` them, then run `harness work sync --continue`. Repeat
  until it finishes. To give up, run `harness work sync --abort`.
- Sync is refused in REVIEW_REQUESTED, QUEUED, MERGING, MERGED and CLOSED. In
  APPROVED, run `harness work check` first.

## Status and History

`harness work status` shows:
- Task state, branch, worktree, base branch
- Latest check results with the commit SHA they ran against
- Latest review decision and the latest reviewer run (outcome, pid, exit code; `orphaned` if its `review request` died)
- Full state transition history with timestamps (e.g. IN_PROGRESS -> CHECKS_RUNNING -> CHECKS_PASSED)
- Merge requirements checklist
- Subtask progress

## Rules

- NEVER use git push, git merge, git rebase, or git checkout directly (use `harness work sync` to rebase)
- Use git add and git commit freely inside the worktree
- ALWAYS run `harness work check` before requesting review
- ALWAYS request and pass review before merging
- Follow `harness work status` output — it tells you the next action
- If a task exists for your work, continue it instead of creating a new one

## Commands Reference

| Command | Purpose |
|---|---|
| `harness task list --status=all` | List all tasks |
| `harness work status [task-id]` | Task state, checks, reviews, history, next action |
| `harness new "title" [--review default\|human\|none]` | Create task + branch + worktree; review type is fixed at creation (`none` needs `review.allow_none: true` on main) |
| `harness work check [task-id]` | Run checks (DAG-ordered, parallel) |
| `harness work diff [task-id]` | Show diff vs base branch |
| `harness work commit -m "msg"` | Commit with message template |
| `harness review request [task-id]` | Request review; launches the reviewer |
| `harness review approve [task-id]` | Record approval (reviewer or human terminal only) |
| `harness review reject --comment "..." [task-id]` | Record rejection (reviewer or human terminal only) |
| `harness review history [task-id]` | Show past reviews |
| `harness work merge [task-id]` | Merge queue -> rebase -> checks -> merge |
| `harness work sync [task-id]` | Rebase task branch onto its base (`--continue` / `--abort` after conflicts) |
| `harness work close [task-id]` | Close task without merging |
| `harness task start <task-id>` | Start a planned task (creates branch + worktree) |

Task ID is optional when you are inside a task's worktree.
