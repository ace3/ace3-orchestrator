#!/usr/bin/env bash
set -euo pipefail

# smoke-pipeline.sh
# Pushes one tiny task through the PM -> EM -> Backend -> Frontend -> QA
# lifecycle against an already-running local-dev stack (mock runner).
#
# Prereqs (in another terminal):
#   make local-db-up
#   make local-dev
#
# Usage:
#   ./scripts/smoke-pipeline.sh
#   BASE_URL=http://127.0.0.1:18081 API_TOKEN=dev-token ./scripts/smoke-pipeline.sh

BASE_URL="${BASE_URL:-http://127.0.0.1:18081}"
API_TOKEN="${API_TOKEN:-dev-token}"
RUN_TIMEOUT_SECONDS="${RUN_TIMEOUT_SECONDS:-180}"

export BASE_URL API_TOKEN RUN_TIMEOUT_SECONDS

node - <<'NODE'
const baseURL = process.env.BASE_URL;
const token = process.env.API_TOKEN;
const runTimeoutSeconds = Number(process.env.RUN_TIMEOUT_SECONDS) || 180;
const { execFileSync } = require("node:child_process");

const EXPECTED_ROLES = ["pm", "em", "backend", "frontend", "qa"];
const MOCK_EVENT_MARKER = "mock runner generated deterministic acceptance response";

const TASK_TITLE = "Add debug health endpoint";
const TASK_DESCRIPTION = `Add a GET /api/debug/health endpoint that returns:
  { "status": "ok", "timestamp": <unix_ms> }

Requirements:
- Public (no auth required).
- Unit test covering 200 response and JSON shape.
- No DB calls, no external dependencies.

Acceptance:
- curl http://localhost:8080/api/debug/health returns 200 with the JSON shape.
- New unit test passes via go test ./...`;

const PROJECT_NAME = `smoke-pipeline-${Date.now()}`;

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
    `Start it with: make local-db-up && make local-dev`
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

async function drainHeartbeat(projectId) {
  for (let i = 0; i < 20; i++) {
    const result = await api("/api/heartbeat", { method: "POST" });
    await sleep(1500);
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

async function ensureProject() {
  const projects = await api("/api/projects");
  const existing = Array.isArray(projects) ? projects.find((p) => p.name === PROJECT_NAME) : null;
  if (existing) return existing;
  return await api("/api/projects", {
    method: "POST",
    body: {
      name: PROJECT_NAME,
      description: "Re-runnable smoke task that exercises PM -> EM -> Backend -> Frontend -> QA in mock mode.",
      default_cli_kind: "codex",
      default_branch_strategy: "worktree-per-run",
    },
  });
}

async function ensureRepo(project) {
  const current = await api(`/api/projects/${project.id}`);
  if (current.repos && current.repos.length > 0) return current;
  let branch = "main";
  try {
    const out = execFileSync("git", ["rev-parse", "--abbrev-ref", "HEAD"], { encoding: "utf8" }).trim();
    if (out && out !== "HEAD") branch = out;
  } catch { /* keep main */ }
  await api(`/api/projects/${project.id}/repos`, {
    method: "POST",
    body: { local_path: process.cwd(), default_branch: branch },
  });
  return await api(`/api/projects/${project.id}`);
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

async function createReviewTask(projectId) {
  return await api(`/api/projects/${projectId}/tasks`, {
    method: "POST",
    body: {
      title: `${TASK_TITLE} review fixture`,
      description: "Mock-mode A2 review fixture.",
      status: "in_review",
      assignee_agent_id: "qa",
      priority: 1,
    },
  });
}

async function runAndWait(taskId, label) {
  const wakeup = await api(`/api/tasks/${taskId}/run`, { method: "POST" });
  return await waitForWakeupRun(taskId, wakeup, label);
}

async function waitForWakeupRun(taskId, wakeup, label) {
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
    await sleep(2000);
  }
  if (!current || current.status !== "done") {
    let events = [];
    try { if (run) events = await api(`/api/runs/${run.id}/events`); } catch { /* best effort */ }
    const tail = events.slice(-8).map((e) => `    ${e.level || "?"}: ${e.message || ""}`).join("\n");
    throw new Error(
      `[${label}] run ${run ? run.id : "(not claimed)"} ended as "${current ? current.status : "unclaimed"}" after ${runTimeoutSeconds}s.\n` +
      `last events:\n${tail || "    (none)"}`
    );
  }
  const events = await api(`/api/runs/${run.id}/events`);
  return { run: current, events };
}

async function runAttemptsAndWait(taskId, attempts) {
  const result = await api(`/api/tasks/${taskId}/run`, { method: "POST", body: { attempts } });
  assert(result.attempts_group_id, "attempt run response missing attempts_group_id");
  assert(Array.isArray(result.wakeups) && result.wakeups.length === attempts.length, "attempt run response missing wakeups");
  const completed = [];
  for (let i = 0; i < result.wakeups.length; i++) {
    completed.push(await waitForWakeupRun(taskId, result.wakeups[i], `attempt#${i + 1}`));
  }
  return { attempts_group_id: result.attempts_group_id, completed };
}

async function assertReviewFlow(projectId) {
  const reviewTask = await createReviewTask(projectId);
  const diff = await api(`/api/tasks/${reviewTask.id}/diff`);
  assert(Array.isArray(diff.files) && diff.files.length === 3, `expected 3 mock diff files, got ${diff.files && diff.files.length}`);
  const targetFile = diff.files[0];
  const firstAdded = targetFile.hunks.flatMap((h) => h.lines).find((line) => line.kind === "add");
  assert(firstAdded && firstAdded.new_line, "mock diff missing added line for inline comment");
  const comment = await api(`/api/tasks/${reviewTask.id}/review-comments`, {
    method: "POST",
    body: {
      run_id: diff.run_id,
      file_path: targetFile.path,
      line_start: firstAdded.new_line,
      line_end: firstAdded.new_line,
      body: "Please keep this review feedback in the next run.",
    },
  });
  assert(comment.status === "open", `review comment status ${comment.status}, want open`);
  const changed = await api(`/api/tasks/${reviewTask.id}/review`, {
    method: "POST",
    body: { action: "request_changes", feed_back_to_agent: true },
  });
  assert(changed.status === "todo", `request_changes status ${changed.status}, want todo`);
  assert(changed.last_review_decision === "changes_requested", `last decision ${changed.last_review_decision}, want changes_requested`);
  const artifacts = await api(`/api/tasks/${reviewTask.id}/artifacts`);
  assert(
    artifacts.some((artifact) => artifact.title === "Reviewer feedback" && artifact.body.includes("Please keep this review feedback")),
    "review feedback artifact not found"
  );
  const approved = await api(`/api/tasks/${reviewTask.id}/review`, {
    method: "POST",
    body: { action: "approve", feed_back_to_agent: false },
  });
  assert(approved.status === "done", `approve status ${approved.status}, want done`);
  assert(approved.last_review_decision === "approved", `last decision ${approved.last_review_decision}, want approved`);
  console.log(`  review flow OK (${reviewTask.id})`);
}

async function assertDraftAttemptReviewFlow(project) {
  let draft = await api("/api/drafts", { method: "POST", body: { repo_id: project.repos[0].id } });
  draft = await api(`/api/drafts/${draft.draft.id}/turn`, { method: "POST", body: { user_message: "Add a tiny smoke fixture file for comparing parallel attempts." } });
  draft = await api(`/api/drafts/${draft.draft.id}/turn`, { method: "POST", body: { user_message: "Done means both attempts finish, produce diffs, and the selected attempt can be reviewed." } });
  draft = await api(`/api/drafts/${draft.draft.id}/finalize`, { method: "POST" });
  assert(draft.preview_brief.goal && draft.preview_brief.acceptance_criteria.length > 0, "draft preview missing goal or acceptance criteria");
  const task = await api(`/api/drafts/${draft.draft.id}/submit`, {
    method: "POST",
    body: { project_id: project.id, assignee_agent_id: "pm", priority: 4 },
  });
  assert(task.id, "draft submit did not create task");
  const group = await runAttemptsAndWait(task.id, [
    { agent_id: "pm", cli: "codex", label: "codex-mock" },
    { agent_id: "pm", cli: "claude", label: "claude-mock" },
  ]);
  const runs = await api(`/api/tasks/${task.id}/attempts/${group.attempts_group_id}`);
  assert(runs.length === 2, `expected 2 attempt runs, got ${runs.length}`);
  const diffs = await api(`/api/tasks/${task.id}/attempts/${group.attempts_group_id}/diffs`);
  assert(diffs.length === 2, `expected 2 attempt diffs, got ${diffs.length}`);
  assert(diffs.every((diff) => Array.isArray(diff.files) && diff.files.length > 0), "attempt diff missing files");
  const selected = await api(`/api/tasks/${task.id}/attempts/${group.attempts_group_id}/select`, {
    method: "POST",
    body: { run_id: runs[0].id },
  });
  assert(selected.status === "in_review", `selected task status ${selected.status}, want in_review`);
  await api(`/api/tasks/${task.id}/review-comments`, {
    method: "POST",
    body: { run_id: runs[0].id, file_path: diffs[0].files[0].path, body: "Winner selected during smoke." },
  });
  const approved = await api(`/api/tasks/${task.id}/review`, {
    method: "POST",
    body: { action: "approve", feed_back_to_agent: false },
  });
  assert(approved.status === "done", `approved selected task status ${approved.status}, want done`);
  console.log(`  draft -> attempts -> review flow OK (${task.id})`);
}

async function main() {
  console.log(`smoke-pipeline starting against ${baseURL}`);
  await waitForHealth();
  console.log("  /healthz OK");

  const agents = await ensureAgents();
  console.log(`  agents OK (${[...agents.keys()].join(", ")})`);

  let project = await ensureProject();
  project = await ensureRepo(project);
  console.log(`  project OK (${project.id} "${project.name}")`);

  const projectTasksBefore = await api(`/api/projects/${project.id}/tasks`);
  const beforeTaskIds = new Set(projectTasksBefore.map((item) => item.id));

  const task = await createTask(project.id);
  console.log(`  task created ${task.id} "${task.title}"`);

  // First run: full lifecycle.
  const first = await runAndWait(task.id, "run#1");
  assert(
    first.events.some((e) => (e.message || "").includes(MOCK_EVENT_MARKER)),
    `[run#1] missing mock runner marker; is MP_RUNNER_MODE=mock?`
  );

  let projectTasks = await api(`/api/projects/${project.id}/tasks`);
  const parent = projectTasks.find((t) => t.id === task.id);
  const childrenAfter1 = projectTasks.filter((t) => t.parent_id === task.id);
  assert(parent, "[run#1] parent task missing");
  assert(
    childrenAfter1.length >= 1,
    `[run#1] expected >=1 child task, got ${childrenAfter1.length}`
  );
  assert(
    childrenAfter1.every((child) => child.title.includes(TASK_TITLE)),
    `[run#1] child task title lost parent context: ${childrenAfter1.map((child) => child.title).join(" | ")}`
  );

  const childRoleIds = new Set(childrenAfter1.map((c) => c.assignee_agent_id).filter(Boolean));
  console.log(`  run#1 produced ${childrenAfter1.length} child task(s); roles: [${[...childRoleIds].join(", ")}]`);

  projectTasks = await drainHeartbeat(project.id);
  const currentRunTasks = projectTasks.filter((item) => !beforeTaskIds.has(item.id));
  const byParentAfterDrain = new Map();
  for (const item of currentRunTasks) {
    const key = item.parent_id || "";
    byParentAfterDrain.set(key, [...(byParentAfterDrain.get(key) || []), item]);
  }
  const rootDepth = treeDepth(task, byParentAfterDrain);
  assert(currentRunTasks.length <= 10, `task tree grew too large: ${currentRunTasks.length}`);
  assert(rootDepth <= 4, `task tree depth ${rootDepth} exceeds cap`);
  assert(
    currentRunTasks.every((item) => item.title.includes(TASK_TITLE)),
    `task title lost root context: ${currentRunTasks.map((item) => item.title).join(" | ")}`
  );
  assert(
    currentRunTasks.every((item) => item.status === "done"),
    `expected steady state with all current-run tasks done: ${currentRunTasks.map((item) => `${item.status}:${item.title}`).join(" | ")}`
  );

  // Idempotency: re-run the same task. Must not blow up and must not duplicate children explosively.
  const second = await runAndWait(task.id, "run#2");
  assert(
    second.events.some((e) => (e.message || "").includes(MOCK_EVENT_MARKER)),
    `[run#2] missing mock runner marker on second run`
  );

  projectTasks = await drainHeartbeat(project.id);
  const childrenAfter2 = projectTasks.filter((t) => t.parent_id === task.id);
  const maxAllowed = childrenAfter1.length * 2 + 1;
  assert(
    childrenAfter2.length <= maxAllowed,
    `[run#2] children grew unboundedly: ${childrenAfter1.length} -> ${childrenAfter2.length} (max allowed ${maxAllowed})`
  );

  // Idempotent seeding: re-fetch project + agents counts; must not have grown because of this run.
  const projectsNow = await api("/api/projects");
  const sameProjectCount = projectsNow.filter((p) => p.name === PROJECT_NAME).length;
  assert(sameProjectCount === 1, `expected exactly 1 "${PROJECT_NAME}" project, found ${sameProjectCount}`);

  const tasksDelta = projectTasks.length - projectTasksBefore.length;
  console.log(`  project task count: ${projectTasksBefore.length} -> ${projectTasks.length} (+${tasksDelta})`);

  await assertReviewFlow(project.id);
  await assertDraftAttemptReviewFlow(project);

  console.log(`smoke-pipeline e2e passed on ${baseURL} (task ${task.id})`);
}

main().catch((err) => {
  console.error(`smoke-pipeline FAILED: ${err.message}`);
  process.exit(1);
});
NODE
