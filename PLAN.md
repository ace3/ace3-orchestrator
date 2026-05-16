# mini-paperclip — Comprehensive Implementation Plan

A Go-based multi-agent orchestrator that wraps your `ace3/skills` via `claude`/`codex` CLI,
with a web Kanban board, SQLite persistence, and a heartbeat-driven task loop.

---

## 1. Goals and Non-Goals

### Goals
- Define agents as **Go structs** (editable in source; no YAML config file needed)
- Persist tasks + comments in **SQLite** (zero external deps)
- Run agent heartbeats via a **background goroutine** (or cron flag)
- Spawn `claude` / `codex` CLI per agent, passing skills + role prompt + task context
- Parse structured **JSON responses** from the CLI agent and update tasks accordingly
- Serve a **web Kanban board** (plain HTML + Alpine.js, no build step)
- Support **subtask creation**, reassignment, and status machine transitions
- Be runnable on a single OCI/GCP instance with `go build && ./mini-paperclip`

### Non-Goals
- Multi-tenant org trees, budgets, billing
- Plugin marketplace
- Real-time collaborative editing
- GitHub/GitLab integration (left as a future extension)

---

## 2. Repository Layout

```
mini-paperclip/
├── cmd/
│   └── mini-paperclip/
│       └── main.go              # CLI entrypoint (serve | heartbeat | task | agent)
├── internal/
│   ├── agents/
│   │   └── registry.go          # Agent definitions (edit here to change agents)
│   ├── db/
│   │   ├── schema.sql           # SQLite DDL (embedded via go:embed)
│   │   ├── db.go                # Open/migrate helpers
│   │   ├── tasks.go             # Task CRUD
│   │   └── comments.go          # Comment CRUD
│   ├── orchestrator/
│   │   ├── heartbeat.go         # Heartbeat loop + per-agent dispatch
│   │   ├── runner.go            # Spawn claude/codex CLI, capture stdout
│   │   ├── prompt.go            # Build agent prompt from task + comments
│   │   └── protocol.go          # Parse AgentResponse JSON
│   ├── api/
│   │   ├── router.go            # Chi router: REST + SSE
│   │   ├── tasks.go             # Task endpoints
│   │   ├── agents.go            # Agent list endpoint
│   │   └── heartbeat.go        # Manual heartbeat trigger endpoint
│   └── web/
│       ├── static/
│       │   └── app.js           # Alpine.js Kanban logic
│       └── templates/
│           └── index.html       # Single-page Kanban (embedded)
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## 3. Agent Registry (source-editable)

Agents are plain Go structs in `internal/agents/registry.go`. To add or modify an agent, edit this file and recompile — no config files.

```go
// internal/agents/registry.go

package agents

type Agent struct {
    ID         string   // unique key: "ceo", "em", "dev", "qa"
    Name       string   // display name
    RolePrompt string   // injected into every prompt for this agent
    Skills     []string // skill names from ace3/skills
    CLIBin     string   // "claude" or "codex"
    CLIProfile string   // --profile flag value (optional)
}

var Registry = []Agent{
    {
        ID:   "pm",
        Name: "Product Manager",
        RolePrompt: `You are a Product Manager. Your job is to convert high-level goals into
clear task descriptions with acceptance criteria. Break work into subtasks
for the EM agent. Respond only in the JSON schema provided.`,
        Skills:  []string{"product-manager", "research"},
        CLIBin:  "claude",
    },
    {
        ID:   "em",
        Name: "Engineering Manager",
        RolePrompt: `You are an Engineering Manager. You receive tasks from the PM and break them
into concrete implementation subtasks assigned to dev or qa agents. Define
interfaces and acceptance criteria. Respond only in the JSON schema provided.`,
        Skills:  []string{"engineering-manager", "drawing"},
        CLIBin:  "claude",
    },
    {
        ID:   "dev",
        Name: "Backend Developer",
        RolePrompt: `You are a Backend Developer. Implement tasks using the approved plan. Write
or update code in the target repo, run tests, and report results. Respond only
in the JSON schema provided.`,
        Skills:  []string{"backend-developer", "diagnose"},
        CLIBin:  "claude",
    },
    {
        ID:   "qa",
        Name: "QA Engineer",
        RolePrompt: `You are a QA Engineer. Write test cases, verify implementations, and report
defects by creating subtasks assigned back to dev. Respond only in the JSON
schema provided.`,
        Skills:  []string{"qa-manager", "qa-engineer", "qa-tester"},
        CLIBin:  "claude",
    },
    {
        ID:   "security",
        Name: "Security Reviewer",
        RolePrompt: `You are a Security Reviewer. Run static and dynamic security reviews and
report findings as subtasks. Respond only in the JSON schema provided.`,
        Skills:  []string{"security-sast"},
        CLIBin:  "claude",
    },
}
```

---

## 4. Data Model (SQLite)

### `tasks`
| Column | Type | Notes |
|---|---|---|
| id | TEXT PK | UUID v4 |
| title | TEXT | |
| description | TEXT | |
| status | TEXT | `todo` `in_progress` `blocked` `done` `cancelled` |
| assignee_agent_id | TEXT | FK → agent.ID (or `human`) |
| parent_id | TEXT | nullable FK → tasks.id |
| created_at | DATETIME | |
| updated_at | DATETIME | |

### `comments`
| Column | Type | Notes |
|---|---|---|
| id | TEXT PK | UUID v4 |
| task_id | TEXT | FK → tasks.id |
| author | TEXT | `human:ignas` or `agent:dev` |
| body | TEXT | |
| created_at | DATETIME | |

---

## 5. Agent–Orchestrator Protocol

Every agent invocation receives a prompt that ends with:

```
Respond ONLY with a single JSON object matching this exact schema. No markdown, no prose.

{
  "task_updates": {
    "status": "todo|in_progress|blocked|done",
    "comment": "Concise summary of what you did or found.",
    "reassign_to": "pm|em|dev|qa|security|null",
    "create_subtasks": [
      {
        "title": "Subtask title",
        "description": "What needs to be done.",
        "assignee_agent_id": "dev",
        "initial_comment": "Context for the assignee."
      }
    ]
  }
}
```

The orchestrator:
1. Parses `status` → updates `tasks.status` and `tasks.updated_at`
2. Appends `comment` → inserts a row into `comments`
3. `reassign_to` (non-null) → updates `tasks.assignee_agent_id`
4. `create_subtasks` → inserts new task rows with `parent_id = current task id`
5. Parse errors → inserts error comment, sets status `blocked`, retries next heartbeat

---

## 6. Heartbeat Loop

```
every N seconds (configurable, default 60s):
  for each agent in registry:
    tasks = SELECT * FROM tasks
            WHERE assignee_agent_id = agent.id
              AND status IN ('todo', 'in_progress', 'blocked')
            ORDER BY updated_at ASC
            LIMIT 1

    if tasks is empty: skip agent

    prompt = buildPrompt(agent, task, recentComments(task.id, limit=10))
    response = runCLI(agent, prompt)       // os/exec → stdout
    apply(response, task)                  // update DB
```

The loop is a goroutine inside the server process. It can also be triggered manually via `POST /api/heartbeat` for development.

---

## 7. CLI Runner

```go
// internal/orchestrator/runner.go

func RunAgent(ctx context.Context, agent Agent, prompt string) (string, error) {
    args := []string{"--print", "--dangerously-skip-permissions"}
    for _, skill := range agent.Skills {
        args = append(args, "--allowedTools", "Bash,Edit,Read")
        // pass skill context via the prompt itself (no --skill flag needed
        // when using claude CLI; skills are referenced in the prompt text)
    }
    args = append(args, prompt)

    cmd := exec.CommandContext(ctx, agent.CLIBin, args...)
    cmd.Env = append(os.Environ(), "TERM=dumb")
    out, err := cmd.Output()
    return string(out), err
}
```

**Note:** Skills from `ace3/skills` are referenced by name in the prompt text (e.g. `Use backend-developer.`). The claude CLI picks them up from `~/.claude/skills/` or `.claude/skills/` in the target repo. No extra flags needed.

---

## 8. Prompt Builder

```
=== AGENT ROLE ===
{agent.RolePrompt}

=== ACTIVE SKILLS ===
{agent.Skills joined with ", "}

=== TASK ===
ID: {task.id}
Title: {task.title}
Description: {task.description}
Status: {task.status}
Parent: {parent task title or "none"}

=== RECENT COMMENTS (last 10) ===
{for each comment: "[author] created_at: body"}

=== WORKING REPO ===
{REPO_PATH env var or "." if unset}

=== RESPONSE SCHEMA ===
Respond ONLY with a single JSON object...
{schema block}
```

---

## 9. REST API

| Method | Path | Description |
|---|---|---|
| GET | `/api/tasks` | List all tasks (with `?status=` and `?assignee=` filters) |
| POST | `/api/tasks` | Create task |
| GET | `/api/tasks/:id` | Get task + comments |
| PATCH | `/api/tasks/:id` | Update task (status, assignee, title, description) |
| DELETE | `/api/tasks/:id` | Cancel task |
| POST | `/api/tasks/:id/comments` | Add human comment |
| GET | `/api/agents` | List agents from registry |
| POST | `/api/heartbeat` | Trigger one heartbeat cycle (dev/debug) |
| GET | `/api/events` | SSE stream for real-time Kanban updates |

---

## 10. Kanban Web UI

Single HTML page served from `internal/web/templates/index.html`, embedded via `go:embed`.

**Columns:** `todo` · `in_progress` · `blocked` · `done` · `cancelled`

**Features:**
- Drag-and-drop cards between columns (updates status via PATCH)
- Click card → modal with description + comment timeline
- Add comment inline
- Create new task form (title, description, assignee dropdown)
- Assignee avatar/badge per card
- SSE-powered auto-refresh (no polling)
- Parent/child task indentation
- Agent badges colored by role (PM=blue, EM=purple, Dev=green, QA=orange, Security=red)

**Stack:** Plain HTML + [Alpine.js](https://alpinejs.dev/) + [Sortable.js](https://sortablejs.github.io/Sortable/) — zero build step, all from CDN.

---

## 11. CLI Commands

```
mini-paperclip serve              # Start HTTP server + heartbeat loop
mini-paperclip serve --port 8080 --interval 60s --repo /path/to/target/repo

mini-paperclip heartbeat          # Run one heartbeat cycle manually

mini-paperclip task create        # Interactive task creation
mini-paperclip task list          # Table of tasks
mini-paperclip task show <id>     # Task + comments
mini-paperclip task assign <id> <agent>

mini-paperclip agent list         # Show registry
```

---

## 12. Configuration (env vars)

| Var | Default | Description |
|---|---|---|
| `MP_DB_PATH` | `./mini-paperclip.db` | SQLite file path |
| `MP_PORT` | `8080` | HTTP server port |
| `MP_HEARTBEAT_INTERVAL` | `60s` | Heartbeat frequency |
| `MP_REPO_PATH` | `.` | Target repo for agent context |
| `MP_CLI_TIMEOUT` | `300s` | Max time per agent invocation |
| `MP_MAX_TASKS_PER_HEARTBEAT` | `1` | Tasks per agent per cycle |

---

## 13. Implementation Phases

### Phase 1 — Core (Week 1)
- [ ] `go mod init`, project scaffold
- [ ] SQLite schema + migration (embedded DDL)
- [ ] Agent registry struct + default 5 agents
- [ ] Task/comment CRUD (internal/db)
- [ ] CLI runner (os/exec → stdout capture)
- [ ] Prompt builder
- [ ] Protocol parser (JSON → DB updates)
- [ ] Heartbeat loop (goroutine + ticker)

### Phase 2 — API + UI (Week 2)
- [ ] Chi router with all REST endpoints
- [ ] SSE event stream
- [ ] Kanban HTML template (Alpine.js + Sortable.js)
- [ ] `mini-paperclip serve` command

### Phase 3 — CLI UX (Week 2 end)
- [ ] `task create / list / show / assign` subcommands (cobra)
- [ ] `agent list` subcommand
- [ ] `heartbeat` one-shot command

### Phase 4 — Polish + Safety (Week 3)
- [ ] CLI timeout + graceful shutdown
- [ ] Error comment insertion on parse failure
- [ ] Retry limit per task (configurable)
- [ ] Logging with zerolog (structured JSON)
- [ ] `Makefile`: build, test, lint, docker
- [ ] README with quickstart

---

## 14. Key Dependencies

```go
// go.mod
require (
    github.com/go-chi/chi/v5       v5.x   // HTTP router
    github.com/mattn/go-sqlite3    v1.x   // SQLite driver
    github.com/google/uuid         v1.x   // UUID generation
    github.com/spf13/cobra         v1.x   // CLI subcommands
    github.com/rs/zerolog          v1.x   // Structured logging
)
```

---

## 15. Makefile Targets

```makefile
build:
    go build -o bin/mini-paperclip ./cmd/mini-paperclip

run:
    ./bin/mini-paperclip serve

test:
    go test ./...

lint:
    golangci-lint run

docker:
    docker build -t mini-paperclip .

migrate:
    ./bin/mini-paperclip db migrate
```

---

## 16. Example First-Run Flow

```bash
# 1. Build
make build

# 2. Point at your target repo
export MP_REPO_PATH=/path/to/my/project

# 3. Start server (opens http://localhost:8080)
./bin/mini-paperclip serve

# 4. Create a top-level task via UI or CLI
./bin/mini-paperclip task create \
  --title "Implement user password reset flow" \
  --assignee pm

# 5. Trigger heartbeat manually (or wait for timer)
curl -X POST http://localhost:8080/api/heartbeat

# 6. Watch Kanban — PM agent plans, creates subtasks for EM/Dev/QA,
#    each heartbeat advances the chain until all subtasks are done
```

---

## 17. File Generation Order for Agent Implementation

If handing this spec to `claude code` or similar, implement in this order:

1. `go.mod` + `go.sum`
2. `internal/db/schema.sql` + `db.go`
3. `internal/db/tasks.go` + `comments.go`
4. `internal/agents/registry.go`
5. `internal/orchestrator/protocol.go`
6. `internal/orchestrator/prompt.go`
7. `internal/orchestrator/runner.go`
8. `internal/orchestrator/heartbeat.go`
9. `internal/api/router.go` + handlers
10. `internal/web/templates/index.html` + `static/app.js`
11. `cmd/mini-paperclip/main.go`
12. `Makefile` + `README.md`
