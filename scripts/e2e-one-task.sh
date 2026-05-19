#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
PROJECT_NAME="mp-e2e-one-task-${RANDOM}-$(date +%s)"
ENV_FILE="${TMP_DIR}/.env"
DEPLOY_ENV_FILE="${ROOT_DIR}/deploy/.env"
DEPLOY_ENV_BACKUP=""
API_TOKEN="e2e-token"

cleanup() {
	docker compose -p "${PROJECT_NAME}" -f "${ROOT_DIR}/deploy/docker-compose.yml" --env-file "${ENV_FILE}" down -v --remove-orphans >/dev/null 2>&1 || true
	if [[ -n "${DEPLOY_ENV_BACKUP}" && -f "${DEPLOY_ENV_BACKUP}" ]]; then
		mv "${DEPLOY_ENV_BACKUP}" "${DEPLOY_ENV_FILE}"
	else
		rm -f "${DEPLOY_ENV_FILE}"
	fi
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

mkdir -p "${TMP_DIR}/code" "${TMP_DIR}/claude" "${TMP_DIR}/codex"

PUBLIC_PORT="$(node - <<'NODE'
const net = require("node:net");
const server = net.createServer();
server.listen(0, "127.0.0.1", () => {
  console.log(server.address().port);
  server.close();
});
NODE
)"

cat >"${ENV_FILE}" <<EOF
POSTGRES_PASSWORD=mp_e2e_password
MP_API_TOKEN=${API_TOKEN}
MP_PUBLIC_PORT=${PUBLIC_PORT}
MP_HEARTBEAT_INTERVAL=3600s
MP_WORKERS=1
MP_MAX_TASKS_PER_HEARTBEAT=1
MP_CLI_TIMEOUT=60s
MP_RUN_MAX_USD=1.00
MP_MONTH_MAX_USD=100.00
MP_RUNNER_MODE=mock
MP_REPO_ALLOWLIST=/host/code
HOST_CODE_DIR=${TMP_DIR}/code
HOST_CLAUDE_DIR=${TMP_DIR}/claude
HOST_CODEX_DIR=${TMP_DIR}/codex
EOF

if [[ -f "${DEPLOY_ENV_FILE}" ]]; then
	DEPLOY_ENV_BACKUP="${TMP_DIR}/deploy.env.backup"
	cp "${DEPLOY_ENV_FILE}" "${DEPLOY_ENV_BACKUP}"
fi
cp "${ENV_FILE}" "${DEPLOY_ENV_FILE}"

docker compose -p "${PROJECT_NAME}" -f "${ROOT_DIR}/deploy/docker-compose.yml" --env-file "${ENV_FILE}" up -d --build

BASE_URL="http://127.0.0.1:${PUBLIC_PORT}" API_TOKEN="${API_TOKEN}" node - <<'NODE'
const baseURL = process.env.BASE_URL;
const token = process.env.API_TOKEN;

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

async function sleep(ms) {
  await new Promise((resolve) => setTimeout(resolve, ms));
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
  const data = text ? JSON.parse(text) : null;
  if (!response.ok) {
    throw new Error(`${options.method || "GET"} ${path} failed: ${data?.error?.message || response.statusText}`);
  }
  return data;
}

async function waitForHealth() {
  for (let i = 0; i < 90; i++) {
    try {
      const response = await fetch(`${baseURL}/healthz`);
      if (response.ok) return;
    } catch {
      // service is still starting
    }
    await sleep(1000);
  }
  throw new Error("service did not become healthy");
}

async function createAgent(role, name) {
  return api("/api/agents", {
    method: "POST",
    body: {
      name,
      role,
      role_prompt: `You are the ${name}.`,
      cli_kind: "codex",
      enabled: true,
      skill_ids: [],
    },
  });
}

async function main() {
  await waitForHealth();

  const backend = await createAgent("backend", "E2E Backend Agent");
  const qa = await createAgent("qa", "E2E QA Agent");
  assert(backend.id !== "backend", "backend agent should use generated id");
  assert(qa.id !== "qa", "qa agent should use generated id");

  const project = await api("/api/projects", {
    method: "POST",
    body: {
      name: "E2E lifecycle project",
      description: "One-task lifecycle verification",
      default_cli_kind: "codex",
      default_branch_strategy: "worktree-per-run",
    },
  });

  const task = await api(`/api/projects/${project.id}/tasks`, {
    method: "POST",
    body: {
      title: "E2E one-task lifecycle",
      description: "Verify role assignees canonicalize and mock runner creates child tasks.",
      status: "todo",
      assignee_agent_id: "backend",
      priority: 10,
    },
  });
  assert(task.assignee_agent_id === backend.id, `parent assignee was not canonicalized: ${task.assignee_agent_id}`);

  const wakeup = await api(`/api/tasks/${task.id}/run`, { method: "POST" });
  let run = null;
  let currentRun = null;
  for (let i = 0; i < 60; i++) {
    if (!run) {
      const wakeups = await api(`/api/tasks/${task.id}/wakeups`);
      const currentWakeup = wakeups.find((item) => item.id === wakeup.id);
      if (currentWakeup && currentWakeup.run_id) {
        run = await api(`/api/runs/${currentWakeup.run_id}`);
      } else {
        await sleep(1000);
        continue;
      }
    }
    currentRun = await api(`/api/runs/${run.id}`);
    if (currentRun.status === "done") break;
    if (currentRun.status === "error" || currentRun.status === "cancelled") break;
    await sleep(1000);
  }

  const events = run ? await api(`/api/runs/${run.id}/events`) : [];
  if (!currentRun || currentRun.status !== "done") {
    throw new Error(`run ended as ${currentRun ? currentRun.status : "unclaimed"}; events: ${events.map((e) => `${e.level}:${e.message}`).join(" | ")}`);
  }

  const tasks = await api(`/api/projects/${project.id}/tasks`);
  const parent = tasks.find((item) => item.id === task.id);
  const children = tasks.filter((item) => item.parent_id === task.id);
  assert(parent?.status === "done", `parent task status is ${parent?.status}`);
  assert(children.length === 2, `expected 2 child tasks, got ${children.length}`);
  assert(children.some((item) => item.assignee_agent_id === backend.id), "missing backend child task assignee");
  assert(children.some((item) => item.assignee_agent_id === qa.id), "missing qa child task assignee");
  assert(
    events.some((event) => event.level === "info" && event.message.includes("mock runner generated deterministic acceptance response")),
    "missing mock runner lifecycle event",
  );

  console.log(`one-task lifecycle e2e passed on ${baseURL}`);
}

main().catch((error) => {
  console.error(error.message);
  process.exit(1);
});
NODE
