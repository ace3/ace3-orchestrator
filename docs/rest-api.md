# ACE3 Orchestrator REST API

All `/api/*` routes require:

```http
Authorization: Bearer $MP_API_TOKEN
Content-Type: application/json
```

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

- `POST /api/tasks/{task_id}/run` queues one run immediately.
- `POST /api/heartbeat` queues eligible assigned tasks.
- `GET /api/tasks/{task_id}/runs` lists run history.
- `GET /api/runs/{run_id}/events?since={event_id}` reads log events.

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
