# Harness — Local Workflow Enforcement for AI Coding Agents

## Problem

AI coding agents are forgetful. They skip tests, commit to main, forget code reviews, and claim work is done without verification. Soft enforcement (instructions, skills, CLAUDE.md) doesn't reliably prevent this. There is no existing tool that provides deterministic, local workflow enforcement with task tracking.

## Solution

A Go CLI binary (`harness`) that enforces a declarative development workflow. Agents interact with git exclusively through `harness`. The harness manages task lifecycle, runs checks as a DAG, gates merges on passing checks and approved reviews. Configuration is YAML, committed to the repo. State lives in sqlite, gitignored.

## Core Principles

- **External and deterministic**: The harness is the authority, not the agent. The agent's `git` access is denied at the permission level; all git operations go through `harness`.
- **Guardrails, not security**: We guard against forgetful agents, not malicious ones. No cryptographic signing or tamper-proofing needed.
- **Declarative configuration**: Everything the harness does is configured in `.harness.yaml`, committed to the repo. Same model as GitHub Actions or GitLab CI.
- **Structured interaction**: All agent-harness communication happens through CLI tool calls. No text parsing.
- **Parallel-safe**: Multiple agents can work on separate tasks in the same repo simultaneously.

## Agent Permission Model

The agent's tool permissions must be configured to deny direct git and allow harness:

```
deny:  Bash(git:*)
allow: Bash(harness:*)
```

This is the hard enforcement boundary. The harness binary calls `git` internally as subprocesses, which is allowed because the permission system only blocks the agent's top-level commands.

## Configuration

### `.harness.yaml` (committed to repo)

```yaml
main_branch: main
worktree_dir: .worktrees

# Checks form a DAG. Independent checks run in parallel.
# Each check runs as a separate OS process.
checks:
  install:
    run: npm install

  lint:
    run: npm run lint
    needs: [install]

  test:
    run: make test
    needs: [install]

  build:
    run: make build
    needs: [test, lint]

# Merge requirements
merge:
  require:
    checks: [build]         # terminal DAG node — implies full dependency chain
    review: approved
  strategy: merge            # merge | squash | rebase
  rerun_checks: true         # re-run checks at merge time before merging

# Review configuration
review:
  type: agent                # agent | human
  prompt: |
    Review this change for: correctness, test coverage, project conventions.
    You have access to the full codebase via Read/Grep/Glob tools.

    Previous review comments:
    {{previous_comments}}

    Task description:
    {{task_description}}

    After reviewing, call one of:
      harness record-review approve
      harness record-review request-changes --comment "your comments here"
```

### `.harness/` directory (gitignored)

```
.harness/
  harness.db        # sqlite database (tasks, check results, reviews)
```

## Storage

Sqlite database in WAL mode for concurrent access from parallel agents.

### Tables

**tasks**
| Column | Type | Description |
|--------|------|-------------|
| id | TEXT PK | Auto-generated short ID (e.g., `task-001`) |
| description | TEXT | Human-readable task description |
| branch | TEXT | Git branch name |
| worktree_path | TEXT | Path to the worktree directory |
| state | TEXT | Current state in the state machine |
| created_at | TIMESTAMP | When the task was created |
| updated_at | TIMESTAMP | Last state transition |

**check_runs**
| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-increment |
| task_id | TEXT FK | References tasks.id |
| check_name | TEXT | Name from the checks DAG |
| exit_code | INTEGER | Process exit code |
| stdout | TEXT | Captured stdout |
| stderr | TEXT | Captured stderr |
| started_at | TIMESTAMP | When the check started |
| finished_at | TIMESTAMP | When the check completed |
| run_group | INTEGER | Groups checks from the same `harness check` invocation |

**reviews**
| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER PK | Auto-increment |
| task_id | TEXT FK | References tasks.id |
| decision | TEXT | `approve` or `request_changes` |
| comment | TEXT | Reviewer comments (required for request_changes) |
| created_at | TIMESTAMP | When the review was recorded |

**merge_queue**
| Column | Type | Description |
|--------|------|-------------|
| position | INTEGER PK | Auto-increment, determines queue order |
| task_id | TEXT FK UNIQUE | References tasks.id |
| queued_at | TIMESTAMP | When the task entered the queue |

## Merge Queue

When multiple agents work in parallel, their approved tasks may be ready to merge at the same time. Uncoordinated merges would cause races: agent A merges, main moves forward, agent B's checks are now stale against the new main.

The merge queue serializes merges. Only one task merges at a time. Each task rebases against the current main and re-runs checks before merging, so every merge is verified against the latest state.

### How it works

1. Agent calls `harness merge` -- task enters `QUEUED` state, inserted into `merge_queue`
2. `harness merge` blocks, polling the queue
3. When this task is first in the queue, it acquires the merge lock (sqlite `BEGIN EXCLUSIVE` transaction):
   a. Rebases the task branch onto current `main_branch`
   b. If `merge.rerun_checks` is true, re-runs the full check DAG in the worktree
   c. If rebase conflict or checks fail: removes from queue, transitions to `MERGE_FAILED`, returns
   d. If all pass: performs the merge, transitions to `MERGED`, removes from queue, cleans up worktree and branch
4. Lock is released. Next queued task proceeds.

From the agent's perspective, `harness merge` is a blocking call that returns either success or failure. The agent doesn't manage the queue -- it just waits.

### Queue observability

`harness list` and `harness status` show the queue:

```
$ harness list
ID        State     Queue  Description
task-001  MERGED    —      add login page
task-002  QUEUED    #1     fix database migration
task-003  QUEUED    #2     add user authentication
```

```
$ harness status task-003
Task: task-003 "add user authentication"
State: QUEUED (position #2 in merge queue)

Waiting for:
  #1  task-002 "fix database migration" (MERGING)

harness merge is blocking. This task will be processed automatically.
```

## Task State Machine

```
NEW ──> IN_PROGRESS ──> CHECKS_RUNNING ──> CHECKS_PASSED ──> REVIEW_REQUESTED
                          ^    |                                      |
                          |    v                           APPROVED ──┤
                          +─ CHECKS_FAILED                            |
                          |                                           v
                          +─ REQUEST_CHANGES                       QUEUED
                          |                                           |
                          +─ MERGE_FAILED                          MERGING
                                                                   |     |
                                                            MERGE_FAILED MERGED
```

### State Transitions

| From | To | Trigger | Condition |
|------|----|---------|-----------|
| NEW | IN_PROGRESS | `harness new` | Worktree and branch created |
| IN_PROGRESS | CHECKS_RUNNING | `harness check` | — |
| CHECKS_RUNNING | CHECKS_PASSED | All checks pass | All required checks exit 0 |
| CHECKS_RUNNING | CHECKS_FAILED | Any check fails | Any required check exits non-0 |
| CHECKS_FAILED | CHECKS_RUNNING | `harness check` | Agent fixes code, re-runs checks |
| CHECKS_PASSED | REVIEW_REQUESTED | `harness request-review` | Checks have passed |
| REVIEW_REQUESTED | APPROVED | `harness record-review approve` | — |
| REVIEW_REQUESTED | REQUEST_CHANGES | `harness record-review request-changes` | — |
| REQUEST_CHANGES | CHECKS_RUNNING | `harness check` | Agent fixes code based on review comments, re-runs checks |
| APPROVED | QUEUED | `harness merge` | Enters the merge queue |
| QUEUED | MERGING | Queue position reached | This task is first in queue, merge lock acquired |
| MERGING | MERGED | Merge succeeds | Rebase clean, checks re-run pass (if configured), git merge succeeds |
| MERGING | MERGE_FAILED | Merge fails | Rebase conflict, check re-run failure, or merge conflict |
| MERGE_FAILED | CHECKS_RUNNING | `harness check` | Agent fixes code, re-runs checks |

`CHECKS_FAILED`, `REQUEST_CHANGES`, and `MERGE_FAILED` are stable states. The agent stays in them until it fixes the code and calls `harness check` to retry. `harness status` shows the failure details in each state.

`QUEUED` is a transient state managed by `harness merge` (which blocks). The agent does not interact with the queue directly.

## Commands

### `harness init`

Creates `.harness.yaml` with a scaffold configuration and `.harness/` directory with an empty sqlite database. Adds `.harness/` to `.gitignore`.

### `harness new <description>`

Creates a new task:
1. Generates a task ID (monotonic counter: `task-001`, `task-002`, ...)
2. Derives a branch name from the description (slugified)
3. Creates a git branch from the configured `main_branch`
4. Creates a git worktree in the configured `worktree_dir`
5. Inserts a task record in state `IN_PROGRESS`
6. Prints the worktree path and next steps

Output:
```
Created task task-003: "add user authentication"
Branch: add-user-authentication
Worktree: .worktrees/add-user-authentication

Next: implement the changes in .worktrees/add-user-authentication/
      then run: harness check
```

### `harness list`

Lists all tasks and their states. Useful when multiple agents are working in parallel.

Output:
```
ID        State              Description
task-001  MERGED             add login page
task-002  REVIEW_REQUESTED   fix database migration
task-003  IN_PROGRESS        add user authentication
```

### `harness status [task-id]`

Shows detailed state for a task. If `task-id` is omitted, detects the current task from the working directory (which worktree we're in).

Output:
```
Task: task-003 "add user authentication"
Branch: add-user-authentication
Worktree: .worktrees/add-user-authentication
State: CHECKS_FAILED

Last check run:
  install    PASS  (0.4s)
  lint       PASS  (1.2s)
  test       FAIL  (3.1s)  — exit code 1
  build      SKIP  (depends on: test)

  test stderr:
    AssertionError: expected 3 to equal 5
    at tests/test_auth.js:42

Review: not requested

Merge requirements:
  [x] Work in worktree
  [ ] All checks pass          <-- BLOCKING
  [ ] Review approved

Next: fix the failing test, then run: harness check
```

### `harness check [task-id]`

Precondition: task state must be `IN_PROGRESS`, `CHECKS_FAILED`, `REQUEST_CHANGES`, or `MERGE_FAILED`.

Runs all configured checks as a DAG. Working directory for each check process is the task's worktree.

1. Transitions task state to `CHECKS_RUNNING`
2. Topologically sorts the check DAG
3. Runs checks with no unmet dependencies first, in parallel (each as a separate OS process)
4. When a check completes, starts any checks whose dependencies are now all met
5. If a check fails, skips all downstream checks
6. Captures stdout/stderr for each check
7. Records all results in `check_runs` table
8. Transitions task state to `CHECKS_PASSED` or `CHECKS_FAILED`

Output: real-time progress, then summary:
```
Running checks...
  install  ✓  (0.4s)
  lint     ✓  (1.2s)   [parallel with test]
  test     ✓  (3.1s)   [parallel with lint]
  build    ✓  (2.0s)

All checks passed.
Next: run harness request-review
```

### `harness request-review [task-id]`

Precondition: task state must be `CHECKS_PASSED`.

Transitions task state to `REVIEW_REQUESTED`.

Output includes everything the reviewer needs:
```
Task task-003 is now REVIEW_REQUESTED.

Diff (main..add-user-authentication):
  <full diff output>

Task description: add user authentication

Previous review comments:
  (none)

Review prompt (from .harness.yaml):
  Review this change for: correctness, test coverage, project conventions.
  ...

---
For agent review: spawn a reviewer subagent with the above context.
  The reviewer must call: harness record-review approve
  or: harness record-review request-changes --comment "..."

For human review: in another terminal, run:
  harness diff task-003
  harness record-review approve task-003
```

### `harness record-review approve [task-id]`

Precondition: task state must be `REVIEW_REQUESTED`.

Records an approval in the `reviews` table. Transitions task to `APPROVED`.

### `harness record-review request-changes --comment <text> [task-id]`

Precondition: task state must be `REVIEW_REQUESTED`.

Records a rejection with comments. Transitions task to `REQUEST_CHANGES`. Agent sees the comments via `harness status` and fixes the code.

### `harness review-history [task-id]`

Shows all previous reviews for a task. Useful for the reviewer subagent to see prior feedback.

Output:
```
Review #1 (2025-06-08 14:30):
  Decision: REQUEST_CHANGES
  Comment: The auth middleware doesn't handle expired tokens. Add a test for token expiry.

Review #2 (2025-06-08 15:15):
  Decision: APPROVE
```

### `harness merge [task-id]`

Precondition: task state must be `APPROVED`.

This command **blocks** until the merge completes or fails.

1. Transitions task state to `QUEUED`, inserts into `merge_queue`
2. Polls the queue until this task is first in line
3. Acquires the merge lock (`BEGIN EXCLUSIVE` transaction)
4. Transitions task state to `MERGING`
5. Rebases the task branch onto the current `main_branch` head
6. If rebase conflict: transitions to `MERGE_FAILED`, aborts rebase, removes from queue, returns
7. If `merge.rerun_checks` is true, re-runs the full check DAG in the worktree (against the rebased code). If any check fails: transitions to `MERGE_FAILED`, removes from queue, returns
8. Fast-forward merges the rebased task branch into `main_branch` (rebase guarantees this is always a fast-forward)
9. If merge succeeds: transitions to `MERGED`, removes from queue, removes the worktree, deletes the branch
10. Releases the merge lock. Next queued task proceeds.

Output while waiting:
```
Queued for merge (position #2).
Waiting for task-002 "fix database migration"...
```

Output when processing:
```
Now processing merge for task-003...
Rebasing add-user-authentication onto main...
Re-running checks...
  install  ✓  lint  ✓  test  ✓  build  ✓

Merging add-user-authentication into main...
Merge successful.

Cleaned up:
  Worktree: .worktrees/add-user-authentication (removed)
  Branch: add-user-authentication (deleted)

Task task-003 is now MERGED.
```

Output on failure:
```
Now processing merge for task-003...
Rebasing add-user-authentication onto main...
Rebase conflict in src/auth.go

Task task-003 is now MERGE_FAILED.
Fix the conflicts, then run: harness check
```

### `harness diff [task-id]`

Shows `git diff main...<task-branch>`. Convenience command.

## Review Flow

### Agent review (`review.type: agent`)

1. Main agent calls `harness request-review`
2. Main agent reads the output (diff, context, review prompt)
3. Main agent spawns a reviewer subagent (via Task tool or equivalent)
4. Reviewer subagent prompt includes: diff, task description, previous review comments, review prompt from config, and instruction to call `harness record-review`
5. Reviewer subagent reads relevant codebase files for context
6. Reviewer subagent calls `harness record-review approve` or `harness record-review request-changes --comment "..."`
7. Main agent calls `harness status` to see the result
8. If approved: `harness merge`. If changes requested: fix and retry.

### Human review (`review.type: human`)

1. Agent calls `harness request-review`
2. Agent tells the user: "Review requested for task-003. Waiting for your approval."
3. Human in a separate terminal:
   - `harness diff task-003`
   - `harness record-review approve task-003` or `harness record-review request-changes --comment "..." task-003`
4. Agent calls `harness status`, sees the result, proceeds.

## Dogfooding: Bootstrap Plan

Since we're building harness using itself, we bootstrap in phases:

1. **Phase 0 (manual)**: Build the minimum viable `harness` binary (init, new, status, check, merge) using direct git commands. No enforcement yet.
2. **Phase 1 (self-hosted)**: Once the MVP works, configure agent permissions to deny `git:*` and use `harness` for all further development. Add `request-review`, `record-review`, `review-history`.
3. **Phase 2 (complete)**: Build remaining features (DAG parallelism, merge strategies, review prompts) using the harness itself.

## Out of Scope (v1)

- Web UI for reviews
- Integration with GitHub/GitLab (push to remote, create remote PRs)
- Automatic triggers (file watchers, auto-run checks on commit)
- Multi-repo support
- Agent-specific adapters (the harness is agent-agnostic by design)
- Secrets management for API keys
