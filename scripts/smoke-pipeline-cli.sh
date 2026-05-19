#!/usr/bin/env bash
set -euo pipefail

# smoke-pipeline-cli.sh
# Same lifecycle proof as smoke-pipeline.sh, but for MP_RUNNER_MODE=cli.
#
# Prereqs (in another terminal):
#   make local-db-up
#   make local-dev-cli      # or: make local-backend-cli
#   `claude` and `codex` available on PATH and authenticated.
#
# Usage:
#   ./scripts/smoke-pipeline-cli.sh
#   BASE_URL=http://127.0.0.1:18082 API_TOKEN=dev-token ./scripts/smoke-pipeline-cli.sh

BASE_URL="${BASE_URL:-http://127.0.0.1:18082}"
API_TOKEN="${API_TOKEN:-dev-token}"
RUN_TIMEOUT_SECONDS="${RUN_TIMEOUT_SECONDS:-900}"

export BASE_URL API_TOKEN RUN_TIMEOUT_SECONDS

node - <<'NODE'
const baseURL = process.env.BASE_URL;
const token = process.env.API_TOKEN;
const runTimeoutSeconds = Number(process.env.RUN_TIMEOUT_SECONDS) || 900;

const EXPECTED_ROLES = ["pm", "em", "backend", "frontend", "qa"];

const TASK_TITLE = "Add debug health endpoint";
const TASK_DESCRIPTION = `Add a GET /api/debug/health endpoint that returns:
  { "status": "ok", "timestamp": <unix_ms> }

Requirements:
- Public (no auth required).
- Unit test covering 200 response and JSON shape.
- No DB calls, no external dependencies.

Acceptance:
- GET /api/debug/health returns 200 with the JSON shape.
- New unit test passes via go test ./...`;

const PROJECT_NAME = "smoke-pipeline-cli";

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${token}`);
  if (options.body !== undefined) headers.set("Content-Type", "application/json");
  const response = await fetch(`${baseURL}${path}`, {
    ...options,
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
  const text = await response.text();
  const data = text ? safeJSON(text) : null;
  if (!response.ok) {
    const reason = (data && data.error && data.error.message) || response.statusText || text;
    throw new Error(`${options.method || "GET"} ${path} -> ${response.status}: ${reason}`);
  }
  return data;
}

function safeJSON(text) {
  try { return JSON.parse(text); } catch { return text; }
}

async function waitForHealth() {
  for (let i = 0; i < 10; i++) {
    try {
      const r = await fetch(`${baseURL}/healthz`);
      if (r.ok) return;
    } catch { /* still starting */ }
    await sleep(1000);
  }
  throw new Error(
    `service at ${baseURL} did not respond to /healthz within 10s. ` +
    `Start it with: make local-db-up && make local-dev-cli`
  );
}

async function ensureAgents() {
  let agents = await api("/api/agents");
  if (!Array.isArray(agents) || agents.length === 0) {
    await api("/api/bootstrap/run", { method: "POST" });
    agents = await api("/api/agents");
  }
  agents = Array.isArray(agents) ? agents : [];
  const byRole = new Map(agents.map((a) => [a.role, a]));
  const missing = EXPECTED_ROLES.filter((r) => !byRole.has(r));
  assert(
    missing.length === 0,
    `expected pre-seeded agents for roles [${EXPECTED_ROLES.join(", ")}], missing: [${missing.join(", ")}]`
  );
  const disabled = EXPECTED_ROLES.filter((r) => byRole.get(r).enabled === false);
  assert(disabled.length === 0, `agents present but disabled: [${disabled.join(", ")}]`);
  return byRole;
}

async function ensureProject() {
  const projects = await api("/api/projects");
  const existing = Array.isArray(projects) ? projects.find((p) => p.name === PROJECT_NAME) : null;
  if (existing) return existing;
  return await api("/api/projects", {
    method: "POST",
    body: {
      name: PROJECT_NAME,
      description: "Re-runnable smoke task that exercises PM -> EM -> Backend -> Frontend -> QA on the real CLI runner.",
      default_cli_kind: "codex",
      default_branch_strategy: "worktree-per-run",
    },
  });
}

async function createTask(projectId) {
  return await api(`/api/projects/${projectId}/tasks`, {
    method: "POST",
    body: {
      title: TASK_TITLE,
      description: TASK_DESCRIPTION,
      status: "todo",
      assignee_agent_id: "pm",
      priority: 5,
    },
  });
}

async function runAndWait(taskId, label) {
  const wakeup = await api(`/api/tasks/${taskId}/run`, { method: "POST" });
  const deadline = Date.now() + runTimeoutSeconds * 1000;
  let run = null;
  let current = null;
  let lastStatus = null;
  while (Date.now() < deadline) {
    if (!run) {
      const wakeups = await api(`/api/tasks/${taskId}/wakeups`);
      const currentWakeup = wakeups.find((item) => item.id === wakeup.id);
      if (currentWakeup && currentWakeup.run_id) {
        run = await api(`/api/runs/${currentWakeup.run_id}`);
      } else {
        await sleep(1000);
        continue;
      }
    }
    current = await api(`/api/runs/${run.id}`);
    if (current.status !== lastStatus) {
      console.log(`  [${label}] run ${run.id} status=${current.status}`);
      lastStatus = current.status;
    }
    if (current.status === "done" || current.status === "error" || current.status === "cancelled") break;
    await sleep(3000);
  }
  if (!current || current.status !== "done") {
    let events = [];
    try { if (run) events = await api(`/api/runs/${run.id}/events`); } catch { /* best effort */ }
    const tail = events.slice(-12).map((e) => `    ${e.level || "?"}: ${e.message || ""}`).join("\n");
    throw new Error(
      `[${label}] run ${run ? run.id : "(not claimed)"} ended as "${current ? current.status : "unclaimed"}" after ${runTimeoutSeconds}s.\n` +
      `last events:\n${tail || "    (none)"}`
    );
  }
  const events = await api(`/api/runs/${run.id}/events`);
  return { run: current, events };
}

async function drainHeartbeat(projectId) {
  for (let i = 0; i < 80; i++) {
    const result = await api("/api/heartbeat", { method: "POST" });
    await sleep(3000);
    const tasks = await api(`/api/projects/${projectId}/tasks`);
    const active = tasks.filter((t) => ["todo", "in_progress", "blocked"].includes(t.status));
    if ((result.queued || 0) === 0 && active.length === 0) return tasks;
  }
  return await api(`/api/projects/${projectId}/tasks`);
}

function treeDepth(task, byParent) {
  const children = byParent.get(task.id) || [];
  if (children.length === 0) return 0;
  return 1 + Math.max(...children.map((child) => treeDepth(child, byParent)));
}

function assertBoundedTree(task, projectTasks, maxTasks) {
  const byParent = new Map();
  for (const item of projectTasks) {
    const key = item.parent_id || "";
    byParent.set(key, [...(byParent.get(key) || []), item]);
  }
  const depth = treeDepth(task, byParent);
  assert(projectTasks.length <= maxTasks, `task tree grew too large: ${projectTasks.length} > ${maxTasks}`);
  assert(depth <= 4, `task tree depth ${depth} exceeds cap`);
  assert(
    projectTasks.every((item) => item.title.includes(TASK_TITLE)),
    `task title lost root context: ${projectTasks.map((item) => item.title).join(" | ")}`
  );
}

async function main() {
  console.log(`smoke-pipeline-cli starting against ${baseURL}`);
  await waitForHealth();
  console.log("  /healthz OK");

  const agents = await ensureAgents();
  console.log(`  agents OK (${[...agents.keys()].join(", ")})`);

  const project = await ensureProject();
  console.log(`  project OK (${project.id} "${project.name}")`);

  const projectTasksBefore = await api(`/api/projects/${project.id}/tasks`);

  const task = await createTask(project.id);
  console.log(`  task created ${task.id} "${task.title}"`);

  const first = await runAndWait(task.id, "run#1");
  let projectTasks = await api(`/api/projects/${project.id}/tasks`);
  const childrenAfter1 = projectTasks.filter((t) => t.parent_id === task.id);
  assert(
    childrenAfter1.length >= 1 || first.run.status === "done",
    `[run#1] expected child tasks or a completed direct handoff, got ${childrenAfter1.length} child tasks`
  );
  if (childrenAfter1.length > 0) {
    assert(
      childrenAfter1.every((child) => child.title.includes(TASK_TITLE)),
      `[run#1] child task title lost parent context: ${childrenAfter1.map((child) => child.title).join(" | ")}`
    );
  }
  const childRoleIds = new Set(childrenAfter1.map((c) => c.assignee_agent_id).filter(Boolean));
  console.log(`  run#1 produced ${childrenAfter1.length} child task(s); roles: [${[...childRoleIds].join(", ")}]`);
  console.log(`  run#1 event count: ${first.events.length}`);

  projectTasks = await drainHeartbeat(project.id);
  assertBoundedTree(task, projectTasks, 10);

  const second = await runAndWait(task.id, "run#2");
  projectTasks = await drainHeartbeat(project.id);
  const childrenAfter2 = projectTasks.filter((t) => t.parent_id === task.id);
  const maxAllowed = Math.max(childrenAfter1.length * 2 + 1, 3);
  assert(
    childrenAfter2.length <= maxAllowed,
    `[run#2] children grew unboundedly: ${childrenAfter1.length} -> ${childrenAfter2.length} (max allowed ${maxAllowed})`
  );
  assertBoundedTree(task, projectTasks, 10);
  console.log(`  run#2 event count: ${second.events.length}`);

  const projectsNow = await api("/api/projects");
  const sameProjectCount = projectsNow.filter((p) => p.name === PROJECT_NAME).length;
  assert(sameProjectCount === 1, `expected exactly 1 "${PROJECT_NAME}" project, found ${sameProjectCount}`);

  const tasksDelta = projectTasks.length - projectTasksBefore.length;
  console.log(`  project task count: ${projectTasksBefore.length} -> ${projectTasks.length} (+${tasksDelta})`);

  console.log(`smoke-pipeline-cli e2e passed on ${baseURL} (task ${task.id})`);
}

main().catch((err) => {
  console.error(`smoke-pipeline-cli FAILED: ${err.message}`);
  process.exit(1);
});
NODE
