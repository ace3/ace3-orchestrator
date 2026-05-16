You can build a “mini‑Paperclip” around your existing Codex skills by adding one small control plane: a task store plus a scheduler that launches Codex CLI as different agents and records their work as tasks + comments, similar to Paperclip’s V1 spec. [github](https://github.com/paperclipai/paperclip/blob/master/doc/SPEC-implementation.md)

Below is a concrete architecture and step‑by‑step plan tailored to a single‑machine setup with Codex CLI and skills.

***

## What you’re trying to replicate

Paperclip’s core loop is surprisingly simple conceptually: [github](https://github.com/paperclipai/paperclip/blob/master/doc/SPEC-implementation.md)

- A human board creates a **company**, **agents**, and **goals**.  
- Agents receive **tasks** via periodic **heartbeats** (scheduled invocations).  
- All work is tracked as **tasks + comments** with a single assignee per task.  
- Communication between agents is **only via tasks and comments**, no free chat channel.  

You don’t need the whole company/org‑tree piece right now; you mainly want:

- A way to define multiple “roles” (CEO/PM/EM/Dev/QA) backed by Codex + skills.  
- A task list with ownership and comments.  
- A scheduler that calls Codex CLI for each agent and lets agents update or create tasks.  

***

## Minimal architecture for a homebrew control plane

Here’s a minimal component breakdown that mirrors Paperclip, but is simple enough for one dev on a single server. [github](https://github.com/awslabs/cli-agent-orchestrator/blob/main/docs/codex-cli.md)

| Component        | Responsibility                                                                 |
|------------------|-------------------------------------------------------------------------------|
| Task store       | Persist tasks, status, assignee, and comments (SQLite or simple JSON).       |
| Agent registry   | Define agents, their role, default skills, and command to run Codex CLI.     |
| Orchestrator     | Heartbeat loop: find tasks, invoke agents, update tasks/comments.            |
| Human UI         | Simple TUI/CLI or minimal web page to create tasks and inspect progress.     |

You can implement this as one Go or Node binary with:

- A small HTTP API or CLI commands for tasks/agents.  
- A background loop (goroutine / cron / systemd timer) that runs “agent heartbeats”.  

This is much smaller than Paperclip’s multi‑tenant, plugin‑ready spec, but preserves the important ideas. [github](https://github.com/paperclipai/paperclip/blob/master/doc/plugins/ideas-from-opencode.md)

***

## Representing agents and skills

Codex already gives you **skills** as reusable workflows (`SKILL.md` + optional scripts) that Codex loads when needed. You’ll build **agents** as thin wrappers around “Codex CLI + a set of skills + a role prompt”. [developers.openai](https://developers.openai.com/codex/skills)

A simple `agents.yaml` could look like:

```yaml
agents:
  - id: ceo
    name: CEO Agent
    role_prompt: >
      You are the CEO. Define high-level goals, break them into projects and tasks,
      and assign them to CTO/EM/Dev/QA agents.
    skills:
      - company-planning
      - roadmap
    codex_profile: default

  - id: em
    name: Engineering Manager
    role_prompt: >
      You are an EM. Turn CEO plans into technical tasks, define scopes and acceptance
      criteria, and assign tasks to dev or QA agents.
    skills:
      - architecture
      - spec-writer
    codex_profile: dev

  - id: dev
    name: Developer
    role_prompt: >
      You are a backend/frontend dev. Implement tasks in the Git repo, run tests,
      and update task comments with progress.
    skills:
      - repo-coder
      - test-writer
    codex_profile: dev

  - id: qa
    name: QA
    role_prompt: >
      You are QA. Write test cases, verify on the develop branch, and report bugs.
    skills:
      - qa-cases
      - bug-reporter
    codex_profile: qa
```

At runtime the orchestrator turns this into a Codex CLI command, e.g.:

```bash
codex chat \
  --agent-profile dev \
  --skill repo-coder \
  --skill test-writer \
  --message "$PROMPT_JSON"
```

The `PROMPT_JSON` bundle can include:

- The agent’s `role_prompt`.  
- The current task title/description.  
- Recent comments.  
- Any relevant repo context.  

Multi‑agent tools like AWS’s CLI Agent Orchestrator and community projects like `codex-weave` use the same pattern: they treat each CLI agent as a worker with a profile, and an orchestration layer provides task context and collects results. [reddit](https://www.reddit.com/r/codex/comments/1qkonr0/practical_cli_agent_orchestration_for_real/)

***

## Task model: issues + comments + ownership

Paperclip’s V1 spec uses a strict “tasks + comments” model with **single assignee** and an explicit state machine for each task. You can copy a stripped‑down version: [github](https://github.com/paperclipai/paperclip/blob/master/doc/SPEC-implementation.md)

**Task fields (SQLite table or JSON):**

- `id`  
- `title`  
- `description`  
- `status` → one of `todo`, `in_progress`, `blocked`, `done`, `cancelled`  
- `assignee_agent_id`  
- `parent_id` (optional, for subtasks)  
- `created_at`, `updated_at`

**Comment fields:**

- `id`  
- `task_id`  
- `author` (`human:<name>` or `agent:<id>`)  
- `body`  
- `created_at`

Rules (mirroring Paperclip, but simpler): [github](https://github.com/paperclipai/paperclip/blob/master/doc/SPEC-implementation.md)

- Only one agent owns a task at a time.  
- An agent marks `in_progress` when it starts working.  
- When it needs help from another role, it creates a subtask with `parent_id`.  
- All communication is via comments on tasks/subtasks.  

This gives you a persistent, auditable trail similar to Paperclip’s “tasks/comments only” communication decision. [github](https://github.com/paperclipai/paperclip/blob/master/doc/SPEC-implementation.md)

***

## Heartbeats: how agents get work

Paperclip uses **heartbeats** so that agents are not constantly running; they wake up, pull tasks, do work, then sleep. You can implement a simpler version: [github](https://github.com/paperclipai/paperclip/blob/master/doc/SPEC-implementation.md)

1. Every N seconds (or via cron), run `orchestrator heartbeat`.  
2. For each agent:
   - Find tasks where `assignee_agent_id = agent.id` and `status in ('todo','in_progress','blocked')`.  
   - Decide which task(s) to process this heartbeat.  
   - Build a prompt containing:
     - Agent role, current task, status.  
     - Recent comments (e.g., last 10).  
     - A “control protocol” telling Codex what responses are valid (see next section).  
   - Invoke Codex CLI for that agent.  
   - Parse the result and update the task store (status and new comments).  

This mirrors the heartbeat loop described in Paperclip’s implementation spec (agents receive tasks via heartbeat invocations, and all work is tracked through tasks/comments). [github](https://github.com/paperclipai/paperclip/blob/master/doc/SPEC-implementation.md)

You can start with a **single‑threaded** heartbeat runner; later you can parallelize per agent if needed.

***

## Designing the agent–orchestrator protocol

Codex CLI itself doesn’t know about tasks; you need a simple protocol so the orchestrator can interpret responses. Community examples of “controller agent + worker agent” in Codex use structured, machine‑parsable responses (JSON or tagged blocks) to let a controller react programmatically. [reddit](https://www.reddit.com/r/codex/comments/1rt6314/a_codex_delegation_skill_that_enables_multiagent/)

Define a response schema like:

```json
{
  "task_updates": {
    "status": "in_progress | blocked | done",
    "comment": "What you did or found.",
    "reassign_to": "dev | qa | em | ceo | null",
    "create_subtasks": [
      {
        "title": "New task title",
        "description": "What needs to be done.",
        "assignee_agent_id": "dev",
        "initial_comment": "Context for this subtask."
      }
    ]
  }
}
```

In the prompt you send to Codex, explicitly instruct:

> Respond with a single JSON object matching this schema. Do not include any extra text, markdown, or explanations.

This is the same pattern used by people who built Codex delegation skills: they keep one main controller chat, call out to worker sessions (via Codex CLI or similar), and have workers respond in a machine‑readable format so the controller can orchestrate. [reddit](https://www.reddit.com/r/codex/comments/1rt6314/a_codex_delegation_skill_that_enables_multiagent/)

The orchestrator then:

- Parses `status` and updates the task.  
- Adds `comment`.  
- Creates any requested subtasks and assigns them.  
- Optionally reassigns the current task (`reassign_to`).  

***

## Wiring Codex CLI into the orchestrator

Since you already have Codex CLI and skills installed, your orchestrator just needs to:

1. **Build the prompt**  
   - Load the agent’s role prompt and skills list.  
   - Gather task + comments.  
   - Add the JSON schema instructions.  

2. **Spawn Codex CLI**  
   - Use something like `os/exec` (Go) or `child_process.spawn` (Node).  
   - Invoke `codex chat` (or `codex exec` if you prefer) with:
     - `--agent-profile` (if you use Codex profiles).  
     - One or more `--skill` flags referencing your skills.  
     - A `--message` containing your full prompt.  

3. **Capture and parse the result**  
   - Read stdout, parse as JSON according to your protocol schema.  
   - On parse errors, attach an error comment and maybe mark the task as `blocked` or retry.

AWS’s CLI Agent Orchestrator shows how you can wrap Codex CLI as a provider: a server launches Codex‑backed sessions and interacts via HTTP, treating Codex as one of several “agents”. You don’t need their full stack, but the pattern is the same: your orchestrator is the boss; Codex is a worker process you call. [github](https://github.com/awslabs/cli-agent-orchestrator/blob/main/docs/codex-cli.md)

***

## Example multi‑agent flow

Here’s how a full flow could look in your mini‑Paperclip:

1. **You (human) create a top‑level task**  
   - “Implement user password reset flow.”  
   - `assignee_agent_id = ceo` or `em`.  

2. **CEO/EM heartbeat**  
   - CEO/EM agent sees the task, plans work, and returns JSON with:
     - A comment describing the plan.  
     - `status: "done"` (CEO’s part is done).  
     - `create_subtasks`: one for Dev (implementation) and one for QA (test plan).  

3. **Dev heartbeat**  
   - Dev agent picks up its subtask, edits code via Codex skills, and returns JSON:
     - Comment: what was implemented, where.  
     - `status: "done"`.  

4. **QA heartbeat**  
   - QA agent reads the develop branch, writes tests, and sets `status: "done"`.  
   - If something fails during verification, QA creates a bug subtask assigned back to Dev.  

5. **EM or CEO heartbeat**  
   - Final check: read all subtasks, check they’re `done`, leave a summary comment, and mark the top‑level task `done`.  

All communication between agents flows through task status and comments, just like Paperclip’s “tasks + comments only” decision. [github](https://github.com/paperclipai/paperclip/blob/master/doc/SPEC-implementation.md)

***

## Concrete implementation steps for your setup

Given your current stack (Codex CLI + skills + comfort with Go/TS), a pragmatic path is:

1. **Pick the storage and language**  
   - Use SQLite + Go (with GORM or sqlc), or SQLite + Node (Prisma/knex).  
   - Define tables for `agents`, `tasks`, `comments`.  

2. **Define a small REST or CLI API**  
   - Endpoints/commands:
     - `create-task`, `list-tasks`, `show-task`, `reassign-task`.  
     - `create-agent`, `list-agents`.  
   - This lets you drive everything from the terminal or a tiny web UI.  

3. **Implement the heartbeat runner**  
   - CLI command `orchestrator heartbeat`:
     - For each agent, select one pending task.  
     - Build prompt and call Codex CLI.  
     - Parse JSON, update DB.  
   - Run it via cron (`* * * * * orchestrator heartbeat`) or as a long‑running loop with a sleep.  

4. **Codex integration layer**  
   - Wrap the Codex CLI call in a small library:
     - `runAgent(agentId, taskId) → AgentResponse`.  
   - This is the only place that knows about Codex flags, profiles, and skill paths.  

5. **Tighten the protocol and skills**  
   - Update your existing skills so that:
     - PM/CEO skills know how to create subtasks and assign them.  
     - EM skills know how to split work and add acceptance criteria.  
     - Dev/QA skills know to respond with JSON using your schema and to reference specific files in comments.  
   - This is very close to how Codex skills are meant to package repeatable workflows for agents. [developers.openai](https://developers.openai.com/codex/skills)

6. **Optional: Git integration**  
   - Once this works locally, add:
     - A Git hook (or just instructions in skills) so Dev agent commits to feature branches.  
     - A QA skill that runs tests and posts results as comments.  

7. **Optional: Web UI**  
   - Build a simple React/Vue/Svelte or Go HTML template UI:
     - List tasks grouped by assignee and status.  
     - Show comment timelines.  
     - Manually reassign or pause tasks.  

***

## How this compares to full Paperclip

Your mini‑control plane will **not** include:

- Multi‑tenant companies, budgets, cost accounting, or plugin systems.  
- A full org tree with reports_to relationships and board controls.  

But it will adopt the elements that actually matter for a practical multi‑agent engineering loop: [mindstudio](https://www.mindstudio.ai/blog/build-multi-agent-company-paperclip-claude-code/)

- Agents defined as roles with clear responsibilities.  
- Tasks as the unit of work with single ownership.  
- Comments as the communication channel.  
- Heartbeats as the execution rhythm.  
- Codex CLI + skills as the execution engine for each agent. [reddit](https://www.reddit.com/r/codex/comments/1rt6314/a_codex_delegation_skill_that_enables_multiagent/)

If you want, next we can design:

- The exact JSON protocol for responses (with error handling).  
- A concrete `agents.yaml` and `skills` mapping for your PM/EM/Dev/QA roles using your existing skills repo.
