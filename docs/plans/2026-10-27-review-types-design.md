# Review types design

Status: approved 2026-10-27.

## Goal

Let a task or epic skip the review gate, or require a human reviewer, without
letting an agent quietly opt out of review after the fact. Checks are never
skipped.

## Review type

Every task and epic has a review type:

| Type | Review gate |
|---|---|
| `default` | Today's flow: `review request` launches the reviewer from the config committed on main (`review.type`/`review.cmd`). |
| `human` | `review request` pins the commit but launches no agent reviewer and mints no usable token; only a human verdict (CLI at a terminal, or the dashboard) counts. |
| `none` | No review gate. The task may enter the merge queue from CHECKS_PASSED; the epic may merge without an epic review. |

Epics behave exactly like tasks: an epic's type governs its own review gate
into main. A task's type governs only that task's merge (into main or into its
epic branch).

## Setting the type

- At creation: `harness new --review=default|human|none`,
  `harness task new --review=…`, `harness epic new --review=…`. Default is
  `default`. `task start` keeps the planned task's type.
- `none` at creation is refused unless the config committed on main
  (`refs/heads/<main>`) has `review.allow_none: true`. The default is false,
  so self-declared no-review is opt-in per repository.
- After creation the type changes only through the dashboard. There is no CLI
  edit path.

Every type change records who set it, how, and when: e.g.
`none · self-declared (allow_none) · 2026-10-27 10:12`,
`none · pre-approved by terminal:alice (dashboard) · 2026-10-27 11:40`.

## Dashboard actions

Both actions use the human-verdict authorization: real terminal, no
`HARNESS_REVIEW_TOKEN` in the environment, typed subject ID to confirm. They
apply to task rows and epic headers in any non-terminal state.

- `p` **pre-approve** — sets type `none`. Does not need `review.allow_none`
  (it is a human decision, not a self-declaration).
  - If a review is in flight (REVIEW_REQUESTED, agent reviewer running): kill
    the reviewer's process group, revoke the request's token by closing the
    request, and record a verdict `approve` on the requested commit with
    reviewer `terminal:<user> (dashboard, pre-approved)` and the time. The
    task becomes APPROVED; the existing merge rules apply (branch head must
    equal the approved commit, required checks must have passed on it).
  - In any other state: only the type changes. When the task later has
    passing checks, it can merge without a review step.
  - QUEUED/MERGING: allowed; it only records the type (the merge is already
    past the review gate).
- `t` **type** — a picker for `default` / `human` / `none`, any direction,
  same confirmation. Switching to `human` while a review is in flight kills
  the agent reviewer's process group, revokes its token (closes the request)
  and moves the task back to CHECKS_PASSED; the next `review request` then
  waits for a human. Switching away from `none` on a task that already passed
  the gate does not undo an APPROVED/QUEUED state.

Killing the reviewer uses the run record from reviewer supervision
(`reviewer_pid` is the process-group leader); the supervising
`harness review request` then sees the request already resolved and reports
it, without treating it as a failure.

## State machine and merge

- New transition for type `none`: CHECKS_PASSED → QUEUED.
- Merge of a `none` task has no approval commit, so it pins to the checked
  commit: the branch head must equal the commit of the latest passing check
  run, and `merge.require.checks` must have passed on exactly that commit.
- Epic merge with type `none` skips the epic approval check; the epic merge
  still re-runs checks when `merge.rerun_checks` is set.
- `merge.require.review` (never enforced) is removed; a config that still
  sets it fails to load with an error pointing to `review.allow_none` and
  review types.

## Display

- `work status`, `task list`, `epic view`, `review history`: show the type and
  its provenance.
- Dashboard: tasks get normal treatment (no special section). A new column
  **approval** combines type and status, e.g. `default · not started yet`,
  `default · reviewing`, `human · awaiting`, `human · approved`,
  `none · pre-approved by alice`, `none · self-declared`. A `none` task never
  appears under AWAITING REVIEW.
- Merge commit message / transition log note "merged without review
  (review: none, set by …)".

## Data

- `tasks.review_type`, `tasks.review_type_set_by`, `tasks.review_type_set_at`
  (and the same on `epics`), migration with default `default`.

## Testing

- Creation: `--review=none` refused without `allow_none`, accepted with it;
  the gate reads the committed config, not the working tree.
- No CLI path changes the type after creation.
- Merge of a `none` task: from CHECKS_PASSED, pinned to the checked commit,
  refused if the head moved or required checks did not pass there.
- `human`: `review request` launches nothing; agent token verdicts refused;
  terminal verdict accepted.
- Dashboard `p`: in REVIEW_REQUESTED with a live reviewer — reviewer group
  killed, request closed, approval recorded with user and time, task
  APPROVED; in other states — type only. `t`: every direction; to `human`
  kills and revokes. Both refused without a terminal or with the token env.
- Epic equivalents of the above.
- Config: `merge.require.review` rejected with a helpful error.

## Implementation tasks

1. Review type data model, creation flags, `allow_none` gate, config key
   removal, display in CLI.
2. Gate changes: `human` request path, `none` merge path (tasks and epics),
   state transition, merge pinning to the checked commit.
3. Reviewer kill + token revoke helper (shared by pre-approve and type
   change), with the supervisor tolerating an externally resolved request.
4. Dashboard: `p` pre-approve, `t` type picker, approval column.
