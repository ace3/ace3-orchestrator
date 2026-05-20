# Nocturne REST API

All `/api/*` routes require:

```http
Authorization: Bearer $MP_API_TOKEN
Content-Type: application/json
```

Bearer tokens are accepted only in the `Authorization` header, including for
the `/api/events` server-sent event stream. Query-string tokens are rejected.

Errors use:

```json
{"error":{"code":"request_failed","message":"details"}}
```

## Task Flow

Create a task under a project:

```http
POST /api/projects/{project_id}/tasks
```

```json
{
  "repo_id": "repo-id-or-null",
  "title": "Implement password reset",
  "description": "Goal, constraints, and acceptance criteria.",
  "status": "todo",
  "assignee_agent_id": "pm",
  "priority": 5,
  "tags": ["backend-only"],
  "lifecycle_id": "backend-only"
}
```

List and update tasks:

- `GET /api/projects/{project_id}/tasks`
- `GET /api/tasks/{task_id}`
- `PATCH /api/tasks/{task_id}`

Run orchestration:

- `POST /api/tasks/{task_id}/run` creates a manual wakeup for the assignee.
- `POST /api/heartbeat` creates heartbeat wakeups for eligible assigned tasks.
- `GET /api/tasks/{task_id}/wakeups` lists durable wake commands.
- `POST /api/tasks/{task_id}/wakeups` creates a wakeup.
- `GET /api/tasks/{task_id}/runs` lists run history.
- `GET /api/tasks/{task_id}/active-run` returns the active queued/running run or `null`.
- `GET /api/tasks/{task_id}/liveness` returns `ready`, `running`, `waiting`, or `stalled`.
- `GET /api/runs/{run_id}/events?since={event_id}` reads log events.

New dispatch always flows through `agent_wakeups`; `runs` is the execution
ledger and links back with `wakeup_id`. Existing legacy runs remain readable.
Agent-created subtasks are bounded to prevent runaway fan-out: at most 5
subtasks per run, depth 4 per task tree, and 50 tasks per root. Duplicate child
titles under the same parent are skipped. Generic child titles are prefixed with
the parent task title so child tasks keep the original request context. QA runs
that finish with `status = "done"` are terminal for that branch and cannot spawn
new children.

Create a wakeup:

```http
POST /api/tasks/{task_id}/wakeups
```

```json
{
  "source": "manual",
  "reason": "operator_retry",
  "payload_json": {"note": "retry after review"},
  "context_snapshot": {"source": "task_drawer"},
  "idempotency_key": "optional-key",
  "requester_type": "human",
  "requester_id": "ignas"
}
```

Wake statuses are `queued`, `claimed`, `running`, `done`, `error`,
`cancelled`, and `coalesced`. Reusing the same idempotency key with the same
payload returns the existing wakeup; reusing it with a different payload returns
`409 conflict`. Duplicate queued timer/manual wakeups coalesce.

Checkout and release:

- `POST /api/tasks/{task_id}/checkout`
- `POST /api/tasks/{task_id}/release`

```json
{"run_id": "run-or-lock-id", "expected_status": "todo"}
```

Checkout sets `checkout_run_id`, moves the task to `in_progress`, and returns
`409 conflict` if a checkout/execution lock already exists or the expected
status is stale. Release clears the lock and returns `409 conflict` when the
supplied `run_id` does not match.

## Task Interactions

Interactions are actionable workflow cards. Comments remain audit text; comments
do not wake agents by themselves.

Allowed `kind` values:

- `suggest_tasks`
- `ask_user_questions`
- `request_confirmation`
- `handoff`
- `qa_finding`
- `approval_request`

Allowed `status` values:

- `open`
- `accepted`
- `rejected`
- `resolved`
- `cancelled`

Agent-created human interactions move the task to `waiting`. Heartbeat and
manual task runs do not continue the task while an open interaction exists.

Create an interaction:

```http
POST /api/tasks/{task_id}/interactions
```

```json
{
  "kind": "request_confirmation",
  "title": "Approve rollout plan",
  "summary": "Agent needs operator approval before continuing.",
  "payload": {"question": "Approve the plan?"},
  "continuation_policy": "wake_assignee",
  "idempotency_key": "optional-key",
  "source_comment_id": "comment-id-or-null",
  "source_run_id": "run-id-or-null",
  "created_by": "agent:em"
}
```

Interaction routes:

- `GET /api/tasks/{task_id}/interactions`
- `POST /api/tasks/{task_id}/interactions`
- `POST /api/task-interactions/{interaction_id}/answer`
- `POST /api/task-interactions/{interaction_id}/accept`
- `POST /api/task-interactions/{interaction_id}/reject`

Answering `ask_user_questions` requires:

```json
{"response": "Use option A."}
```

Accepting or rejecting approvals can include an optional note:

```json
{"note": "Approved for the dev environment only."}
```

Resolving an open interaction with `continuation_policy = "wake_assignee"`
stores the human response in `resolution_payload`, writes a human comment, and
creates a wakeup for the task assignee.

## Runtime Continuity

Runs include wakeup context, recent run summaries, and saved runtime state in
the agent prompt. CLI session resume is best-effort: if Claude or Codex emits a
session id, it is saved in `agent_runtime_state`; if no session id is reported,
the run log gets a warning and the next run starts without CLI resume continuity.

## Backup & Restore

Backup artifacts live under `MP_BACKUP_DIR`.

List and download artifacts:

- `GET /api/backups`
- `GET /api/backups/{backup_id}/download`

Full database lane:

- `POST /api/backups/full` creates a PostgreSQL custom-format `pg_dump`.
- `POST /api/backups/full/upload` accepts multipart field `backup`.
- `POST /api/backups/full/validate` validates an existing full backup.
- `POST /api/backups/full/restore-plan` returns operator instructions and a `pg_restore` command.

```json
{"backup_id": "full-db-20260519T010203Z.dump"}
```

The API never executes full database restore. Operators must run the returned command on the server after stopping writers and taking an out-of-band backup.

Nocturne application data lane:

- `POST /api/backups/app/export` creates a versioned JSON export.
- `POST /api/backups/app/upload` accepts multipart field `backup`.
- `POST /api/backups/app/validate` validates bundle selection and dependencies.
- `POST /api/backups/app/dry-run` returns insert/update counts.
- `POST /api/backups/app/import` performs merge-overwrite import when `confirm` is `RESTORE`.

```json
{
  "backup_id": "ace3-app-20260519T010203Z.json",
  "bundles": ["configuration", "projects", "tasks", "execution_history"],
  "confirm": "RESTORE"
}
```

Bundles are `configuration`, `projects`, `tasks`, and `execution_history`.
Partial restores are dependency-aware; missing dependency bundles fail validation
or are included from the export. Import creates a pre-restore Nocturne JSON backup,
blocks while runs or wakeups are active, uses one transaction, and normalizes
queued/running imported execution state so old work is not restarted.

## Task Artifacts

Artifacts store durable task context for PM documents, handoffs, engineering plans, QA reports, implementation notes, and run logs.

Allowed `kind` values:

- `pm_document`
- `pm_handoff`
- `em_document`
- `em_handoff`
- `qa_report`
- `implementation_note`
- `run_log`
- `other`

Allowed `format` values:

- `markdown`
- `text`
- `json`

Create an artifact:

```http
POST /api/tasks/{task_id}/artifacts
```

```json
{
  "kind": "pm_document",
  "title": "PRD",
  "body": "## Goal\n...",
  "format": "markdown",
  "metadata": {"source": "api"},
  "created_by": "agent:pm"
}
```

Artifact routes:

- `GET /api/tasks/{task_id}/artifacts`
- `POST /api/tasks/{task_id}/artifacts`
- `GET /api/task-artifacts/{artifact_id}`
- `PATCH /api/task-artifacts/{artifact_id}`
- `DELETE /api/task-artifacts/{artifact_id}`

Run-created artifacts have `run_id` set. They can be patched, but deletion returns `409 conflict`.

## Skill Sources

Skill content is read from the Git-backed cache under `MP_SKILLS_CACHE_DIR`; the database stores sources, pins, discovered metadata, ignored state, and agent assignments.

- `GET /api/skill-sources`
- `POST /api/skill-sources`
- `POST /api/skill-sources/import-github-skill`
- `POST /api/skill-sources/check-updates`
- `POST /api/skill-sources/{source_id}/sync`
- `POST /api/skill-sources/{source_id}/pin`
- `DELETE /api/skill-sources/{source_id}`
- `GET /api/skills?include_ignored=true`
- `PATCH /api/skills/{skill_id}`
- `GET /api/skill-drift`

`POST /api/skill-sources/{source_id}/sync` fetches the pinned Git ref only when the cached checkout is missing, discovers `SKILL.md` files, and refreshes DB metadata. `GET /api/skill-drift` reports cache/DB/repo-default mismatches and returns `ok: false` when an operator sync or pin action is needed.

## Agent Output Attachments

Agents can persist artifacts from their run JSON:

```json
{
  "task_updates": {
    "status": "done",
    "comment": "Created PM handoff.",
    "reassign_to": null,
    "request_human_review": false,
    "keep_worktree": false,
    "create_subtasks": [],
    "attachments": [
      {
        "kind": "pm_handoff",
        "title": "PM to EM handoff",
        "body": "Scope, acceptance criteria, and non-goals.",
        "format": "markdown",
        "metadata": {"phase": "pm"}
      }
    ]
  }
}
```

Legacy `file` and `log` attachments are accepted. `file` becomes an `implementation_note`; `log` becomes a `run_log`; `path` and `note` are stored in metadata.
