# harness

A local CLI that enforces a development workflow for AI coding agents. Every
change goes through a task with its own branch and worktree, a DAG of checks, a
review, and a serialized merge queue. The agent can't mark its own work done:
`harness` decides what state a task is in, and merges are blocked until the
configured checks pass and a review approves the exact commit being merged.

Configuration lives in `.harness.yaml`, committed to the repo. State lives in
SQLite at `.harness/harness.db`, which is gitignored.

## Install

```sh
make install   # vet + fmt-check + tests, then go install and symlink into ~/.local/bin
```

Requires Go (version in `go.mod`) and `git`.

## Quickstart

```sh
harness init               # writes .harness.yaml, creates .harness/, updates .gitignore
harness setup opencode     # merges permission rules into opencode.json and installs agent skills
harness setup claude       # or: merges deny rules into .claude/settings.json
git add .harness.yaml && git commit -m "add harness"   # review config is read from main

harness new "add login page"      # task-001: branch + worktree under .worktrees/
# ... edit and commit in the worktree (harness work commit -m "..." uses the commit template)
harness work check task-001       # run the check DAG
harness review request task-001   # needs CHECKS_PASSED
harness work merge task-001       # needs APPROVED; waits in the merge queue
```

Other useful commands:

- `harness work status [task]`, `harness work diff [task|epic]`, `harness work close [task] [--cleanup]`
- `harness work sync [task] [--continue|--abort]` rebases the task onto its
  base branch inside the worktree, so you can recover from conflicts. On a
  conflict, resolve the files, `git add` them, and run `--continue`. The task
  goes back to `IN_PROGRESS`, so you need to run the checks again.
- `harness review approve|reject [task|epic] --comment "..."`, `harness review history [task|epic]`
- `harness dashboard`: see [Dashboard](#dashboard).

If you run a command from inside a task's worktree, you can leave out the task ID.

## Task states

```
PLANNED -> IN_PROGRESS -> CHECKS_RUNNING -> CHECKS_PASSED -> REVIEW_REQUESTED -> APPROVED -> QUEUED -> MERGING -> MERGED
                              |  ^                                 |                 |
                    CHECKS_FAILED, REQUEST_CHANGES, MERGE_FAILED --+  (re-run: harness work check)
```

You can re-run `harness work check` from any state between `IN_PROGRESS` and
`APPROVED`, and from `MERGE_FAILED`. You can't re-run it from
`PLANNED`/`QUEUED`/`MERGING` or from a terminal state. `work sync` moves a
task back to `IN_PROGRESS` from `CHECKS_FAILED`, `CHECKS_PASSED`,
`REQUEST_CHANGES` or `MERGE_FAILED`. If the branch moves after approval,
`work merge` resets the task to `IN_PROGRESS`.
`internal/state.go` has the full transition table.

## Review model

- `harness review request` records the branch head and mints a **one-time
  token** for that request.
- The `review:` section is always read from `.harness.yaml` **as committed at
  `refs/heads/<main_branch>`**. Edits to the working tree are ignored, so an
  implementer can't redirect the reviewer.
- `review.type: agent` needs `review.cmd`. Harness runs that command with the
  composed prompt in `{{prompt_file}}` and the token in `HARNESS_REVIEW_TOKEN`
  in the command's environment only. The token is never printed. The reviewer
  records its verdict with `harness review approve|reject`.
- The reviewer runs in its own process group under `review.timeout` (Go
  duration, default `30m`), with stdin at `/dev/null`: it gets no terminal, so
  it must read the prompt from `{{prompt_file}}` and can't be interactive. On
  timeout, Ctrl-C, SIGTERM or SIGHUP (terminal closed), harness kills the
  whole group. Each run is recorded on its request (pid, start, heartbeat,
  exit code, outcome: `verdict`, `no_verdict`, `failed`, `timeout`,
  `interrupted`). `harness work status` and `harness review history` show it; a
  run whose `harness review request` process died reads as `orphaned`.
- Fail closed: if the reviewer exits without a verdict, harness closes the
  request (its token stops working) and `review request` exits non-zero. The
  task stays `REVIEW_REQUESTED`; run `harness work check`, then
  `harness review request` again. A verdict followed by a non-zero exit still
  counts, with a warning.
- `review.type: human`: without a token, approving or rejecting only works from
  an interactive terminal (stdin is a TTY), and the human has to type the task
  ID to confirm.
- A verdict covers exactly the commit that was shown to the reviewer. If the
  branch moved, you need to run checks again and request a new review.

Do not let the reviewer run code from the branch under review (`go test`,
`make`, ...). It holds the approval token, and the checks have already run.

## Merge queue

`harness work merge` puts the task in a queue in SQLite and blocks until the
task reaches the head. Before it queues, it checks that `merge.require.checks`
passed on exactly the approved commit, and refuses otherwise. At the head, it:

1. prepares the branch (a rebase, for the `merge` strategy),
2. optionally runs the checks again (`merge.rerun_checks`),
3. merges into the task's base branch,
4. removes the worktree and branch.

The merge happens in the target branch's own worktree, or in a temporary one.
It never switches the branch of your main checkout.

Each queue entry is a **lease** owned by the merging process (its PID). The
process renews a heartbeat every 10s. Other mergers reclaim an entry whose PID
is dead or whose heartbeat is more than 60s old, and the abandoned task moves to
`MERGE_FAILED`. If you press Ctrl-C while waiting, the task gives up its slot.
`merge.queue_timeout` (default 300s) limits how long a merger waits.

```sh
harness queue list                 # entries, owner PID, heartbeat age, live/stale
harness queue clear <task> [--force]   # remove a stale entry (--force for a live one)
```

## Dashboard

```sh
harness dashboard [--interval 2s] [--all]   # full-screen view, refreshed every --interval
harness dashboard --once [--all]            # print one plain snapshot and exit
```

The dashboard shows open review requests (tasks and epics), in-flight tasks
grouped by project and epic, the merge queue with lease status, and tasks
merged or closed in the last 24h. A red `!` marks rows that need a human:
failed checks or merge, a failed/timed-out/orphaned reviewer, a stale queue
lease, an unfinished rebase in the worktree, or a review waiting for a human.
`--all` adds PLANNED tasks and older finished ones.

| key | action |
|---|---|
| `↑`/`↓`, `j`/`k` | select a row (`g`/`G` first/last) |
| `enter`, `tab` | toggle the detail pane: state history, checks, review request and reviewer run, queue lease, live check log |
| `f` | refresh now |
| `q`, `Ctrl-C` | quit |
| `a`, `r`, `d` | approve, reject, diff (shown dimmed; not available yet) |

The interactive view needs a terminal; from scripts and agents use `--once`,
whose output has no colour when stdout is not a terminal. `NO_COLOR` turns
colour off in both. If a refresh fails, the last good snapshot stays on screen
with a `stale:` note in the header.

All mergers have to run on one host, because PID liveness is checked locally.

## Checks

- Checks form a DAG through `needs`. Independent checks run in parallel, each
  as its own process group in the task worktree. If a check fails, everything
  downstream is skipped.
- Each check has a `timeout` (Go duration, default `30m`). When it expires,
  harness kills the whole process group, and the check gets exit code 124. An
  interrupted check gets 130.
- Live output goes to `.harness/logs/<task>/<check>.log` while the check runs.
  The results are also stored in the DB and shown by `harness work status`.

## Planning layer

Projects contain epics, and epics contain tasks. Planned tasks don't get git
resources until they're started.

```sh
harness project new "auth" --description "..."
harness epic new "login" --project proj-001          # creates branch epic/login by default
harness task new "login form" --epic epic-001        # PLANNED, no branch yet
harness task dep add task-001 task-002               # task-001 blocks task-002 (--type blocks|related)
harness task list --ready                            # planned tasks whose blockers are merged/closed
harness task start task-002                          # branch + worktree, now IN_PROGRESS
harness review request epic-001 && harness epic merge epic-001
```

`task list` shows `PLANNED` tasks by default. Use `--status all` or
`--status <state>` to filter by state, and `--epic` or `--project` to scope the
list. Projects and epics also have `edit`, `list`, `view` and `close`. Tasks
also have `edit`, `view`, `close`, `delete` and `dep rm`.

## Configuration (`.harness.yaml`)

```yaml
main_branch: main              # auto-detected from git if omitted
worktree_dir: .worktrees       # default
epic_feature_branches: true    # default; false = epics get no branch unless --branch

commit:
  message: "[{{proj}}] [{{epic}}] [{{task}}] {{message}}"   # default "{{message}}"

checks:
  test:
    run: make test
    timeout: 10m               # default 30m
  build:
    run: make build
    needs: [test]

merge:
  require:
    checks: [build]            # a terminal node implies its dependencies
    review: approved
  strategy: merge              # merge (rebase + fast-forward) | squash
  rerun_checks: true           # re-run checks on the rebased branch before merging
  queue_timeout: 300           # seconds to wait in the queue

review:
  type: agent                  # agent | human
  timeout: 30m                 # default 30m; review.cmd is killed after this
  cmd: 'claude -p --setting-sources user --allowedTools Read Grep Glob "Bash(git diff:*)" "Bash(harness review approve:*)" "Bash(harness review reject:*)" < {{prompt_file}}'
  prompt: |
    Review {{task_id}}: {{task_description}}
    {{diff}}
    Previous comments: {{previous_comments}}
```

The `cmd` placeholders are `{{prompt_file}}` and `{{task_id}}`. The `prompt`
placeholders are `{{diff}}`, `{{previous_comments}}`, `{{task_description}}`
and `{{task_id}}`.

## Known limits

Harness is a set of guardrails against a forgetful agent. It isn't a sandbox
against a hostile one. An agent with shell access that runs as the same OS
user can still:

- edit `.harness/harness.db` directly, or
- forge a TTY (for example with `script` or a pty library) and pass the
  interactive-terminal check for human approval.

The defence-in-depth layer is the permission rules from `harness setup
opencode` and `harness setup claude`. Both merge their rules into an existing
config. They deny:

- `git push/merge/rebase/checkout/stash/replace/update-ref/tag`
- `harness review approve/reject` and `harness queue clear`
- `sqlite3`
- for opencode, also shell writes into `.harness`

With these rules the agent has to go through `harness`. For stronger
guarantees, run the agent as a different user, or in a sandbox that can't write
to `.harness/`.

These denies also apply to a `review.cmd` reviewer that loads the project's
settings, for example `claude -p` run in this repo. In Claude Code, deny beats
allow, so that reviewer can't record a verdict. Launch the reviewer with a
config that skips the project settings, for example
`claude -p --setting-sources user ...`.

## Development

```sh
make lint        # gofmt check + go vet
make test        # lint + go test ./...
make test-race   # go test -race ./...
make build       # test + build ./harness
```

CI (`.github/workflows/ci.yml`) runs the lint, race tests, build and golangci-lint.
