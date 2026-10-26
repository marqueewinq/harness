# `harness dashboard` design

Status: approved 2026-10-26. Depends on task-025 (reviewer run records).

## Goal

A human sees every in-flight task at a glance and can perform human review
without leaving the view. Read-mostly; human review is the first (and in v1
only) action, built on an action hook that later actions plug into.

## Command

```
harness dashboard [--once] [--interval 2s] [--all]
```

- Full-screen terminal UI (Bubble Tea + Lip Gloss), redrawn every `--interval`
  until `q`/Ctrl-C.
- `--once` prints one snapshot and exits; colour off when stdout is not a
  terminal; no actions. Usable by scripts and agents.
- `--all` includes PLANNED and old MERGED/CLOSED tasks.

## Layers

1. `internal/dashboard/snapshot.go` — `BuildSnapshot(app) (Snapshot, error)`.
   Pure data, one DB/git read per tick:
   - `Snapshot{Repo, MainHead, Now, Reviews []ReviewRow, Groups []EpicGroup,
     Queue []QueueRow, Recent []EpicGroup, Warnings []string}`
   - `EpicGroup{Project, Epic (nil = "No epic"), Branch, Done, Total,
     EpicReview, Tasks []TaskRow}`
   - `TaskRow`: id, title, state, age in state (transitions table), latest
     check run per check (running check: elapsed from its live log under
     `.harness/logs/<task>/`), review request + reviewer run (outcome incl.
     `orphaned`), latest verdict, queue position/lease, attention reason.
   - `attention(row)` is the single place deciding the red `!`:
     CHECKS_FAILED, MERGE_FAILED, reviewer failed/timeout/orphaned/no_verdict,
     stale queue lease, unfinished rebase/merge in the worktree, review waiting
     on a human.
   - In flight = every state except PLANNED, MERGED, CLOSED. Recent =
     merged/closed in the last 24h.
2. `internal/dashboard/view.go` — `Render(Snapshot, UIState, width, height)
   string`. Pure; golden-tested.
3. `internal/dashboard/model.go` — Bubble Tea model: snapshot + UI state
   (selection, open pane, scroll, modal). Tick command rebuilds the snapshot.

WAL SQLite means the dashboard never blocks mergers. Git per tick: main head,
and an operation-in-progress check per in-flight worktree.

## Layout

```
harness · main @ 39829b9 · 16:42:07 ⟳
AWAITING REVIEW
 ! task-026  81742677  12m  agent reviewer orphaned
   epic-002  5d1c09aa   3m  human
IN FLIGHT
 proj-001 › epic-002 Planning layer · epic/planning · 3/7 done · review requested
   task-025  CHECKS_RUNNING    3m  test ▶ 41s
 ! task-027  MERGE_FAILED      1h  ✓ 2/2  rebase conflict
 No epic
   task-030  IN_PROGRESS       5m
MERGE QUEUE
 1 task-028  pid 5150  hb 4s  live
RECENT (24h)
 proj-001 › epic-002 …
   task-024  MERGED  18m ago
── detail / diff pane ──
 approve  reject  diff  enter details  refresh  quit
```

- Empty sections collapse to one dim line.
- Detail pane (Enter/Tab): `work status` content plus the tail of the live
  check log or reviewer output. Diff pane (`d`): scrollable.
- Terminal smaller than 60×12: "terminal too small".

## Key bar and hotkeys

htop-style: the hotkey letter is highlighted in the accent colour inside its
label word, so every hotkey is a letter of its own word:

| key | label | action |
|---|---|---|
| `a` | **a**pprove | approve selected review |
| `r` | **r**eject | reject selected review |
| `d` | **d**iff | open diff pane |
| `f` | re**f**resh | refresh now |
| `q` | **q**uit | quit |
| `↑↓` / `jk` | select | move selection |
| `enter` | details | toggle detail pane |

Keys not applicable to the selected row are dimmed, never hidden. Every modal
has its own key bar (`enter confirm  esc cancel`).

## Colour

Lip Gloss adaptive colours; `NO_COLOR` respected. Green: passed / approved /
merged. Yellow: running / queued / requested. Red: failed / merge failed /
orphaned / stale. Dim: planned / closed. Red `!` gutter for attention rows;
action errors on a red status line until the next keypress.

## Human review (first action)

- Applies to rows in AWAITING REVIEW (tasks and epics with an open request).
- `d`: diff of exactly the requested commit, `git diff <base>...<request SHA>`,
  beside title/description, previous comments, and check results on that
  commit.
- `a` approve: confirm box "Approve task-026 at 81742677?" requiring the
  subject ID to be typed (same rule as the CLI terminal path); optional
  comment.
- `r` reject: required comment (text area), then the typed-ID confirm.
- `esc` cancels at any step.

Authorization: `authorizeVerdict`'s terminal path is split into an internal
`recordTerminalVerdict(subject, decision, comment, via string)`, called only
by the CLI after its stdin confirm and by the dashboard after its in-TUI
confirm. Actions are enabled only when the dashboard runs on a real TTY.
Reviewer recorded as `terminal:<user> (dashboard)`. Everything else is the
existing `recordVerdict` path: open request required, branch head must equal
the request SHA, verdict closes the request. Threat level unchanged (faking a
TTY already reached the CLI path).

Races: a human verdict while an agent reviewer runs closes the request; the
agent's later verdict fails "already resolved" and task-025's supervisor sees a
recorded verdict. A moved branch fails with the existing "moved since review
was requested" error, shown in the status line.

## Future actions

`keymap → Action{Label, Key, Enabled(row) bool, Confirm, Run(app, id) error}`.
v1 registers approve and reject. Later: merge, close, `queue clear`, sync —
each one table entry calling the same `App.Cmd*` as the CLI.

## Errors

Snapshot failure keeps the last good snapshot with a "stale: <error>" header
and retries next tick. Missing log/worktree renders `-`. `--once` exits
non-zero only if the snapshot fails.

## Testing

- Snapshot: table tests on seeded DBs for every state, every attention reason,
  and epic grouping (incl. "No epic", progress counts).
- Render: golden tests at fixed width, colour off; one colour-on test for
  hotkey styling and dimmed keys.
- Model: key-driven `Update` tests — selection, panes, approve requires typed
  ID, esc cancels, reject requires comment, actions disabled without a TTY,
  verdict lands via `recordTerminalVerdict` with the dashboard reviewer label.
- E2E: `harness dashboard --once` through the built binary on a real repo.

## Implementation tasks

1. `recordTerminalVerdict` split + reviewer label (small, independent).
2. Snapshot layer + `--once` plain renderer (after task-025).
3. Bubble Tea model, layout, key bar, detail pane.
4. Human review action: diff pane, approve/reject modals.
