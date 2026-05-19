export type Agent = {
  id: string;
  name: string;
  role: string;
  role_prompt: string;
  base_prompt?: string;
  definition_hash?: string;
  cli_kind: "claude" | "codex";
  cli_profile: string | null;
  enabled: boolean;
  skills?: Skill[];
};

export type Skill = {
  id: string;
  source_id: string;
  name: string;
  path_in_source: string;
  version: string;
  archived: boolean;
  ignored: boolean;
};

export type SkillTreeEntry = {
  name: string;
  path: string;
  type: "file" | "directory";
  children?: SkillTreeEntry[];
};

export type SkillTreeResponse = {
  skill: Skill;
  source: SkillSource;
  root: SkillTreeEntry;
};

export type SkillContentResponse = {
  skill_id: string;
  path: string;
  content: string;
};

export type SkillSource = {
  id: string;
  name: string;
  upstream_url: string;
  pinned_sha: string;
  path_filter: string;
  last_synced_at: string | null;
  kind: string;
  has_update: boolean;
};

export type Project = {
  id: string;
  name: string;
  description: string;
  default_cli_kind: "claude" | "codex";
  default_branch_strategy: "worktree-per-run" | "shared";
  repos?: Repo[];
};

export type Task = {
  id: string;
  project_id: string;
  repo_id: string | null;
  title: string;
  description: string;
  status: "todo" | "in_progress" | "in_review" | "blocked" | "done" | "cancelled";
  assignee_agent_id: string | null;
  parent_id: string | null;
  priority: number;
  retry_count: number;
  tags: string[];
  lifecycle_id: string;
  checkout_run_id: string | null;
  execution_run_id: string | null;
  execution_state: string | null;
};

export type TaskArtifact = {
  id: string;
  task_id: string;
  kind: "pm_document" | "pm_handoff" | "em_document" | "em_handoff" | "qa_report" | "implementation_note" | "run_log" | "other";
  title: string;
  body: string;
  format: "markdown" | "text" | "json";
  metadata: Record<string, unknown>;
  created_by: string;
  run_id: string | null;
  created_at: string;
  updated_at: string;
};

export type Comment = {
  id: string;
  task_id: string;
  author: string;
  body: string;
  created_at: string;
};

export type Run = {
  id: string;
  agent_id: string;
  task_id: string;
  wakeup_id: string | null;
  status: "queued" | "running" | "done" | "error" | "cancelled";
  cli_kind: "claude" | "codex";
  started_at: string | null;
  finished_at: string | null;
  exit_code: number | null;
  worktree_path: string | null;
};

export type AgentWakeup = {
  id: string;
  agent_id: string;
  task_id: string;
  source: string;
  reason: string;
  payload_json: Record<string, unknown>;
  context_snapshot: Record<string, unknown>;
  idempotency_key: string | null;
  requester_type: string;
  requester_id: string | null;
  status: "queued" | "claimed" | "running" | "done" | "error" | "cancelled" | "coalesced";
  coalesced_count: number;
  run_id: string | null;
  error: string;
  created_at: string;
  updated_at: string;
};

export type TaskInteraction = {
  id: string;
  task_id: string;
  kind: "suggest_tasks" | "ask_user_questions" | "request_confirmation" | "handoff" | "qa_finding" | "approval_request";
  status: "open" | "accepted" | "rejected" | "resolved" | "cancelled";
  title: string;
  summary: string;
  payload: Record<string, unknown>;
  continuation_policy: "none" | "wake_assignee";
  idempotency_key: string | null;
  source_comment_id: string | null;
  source_run_id: string | null;
  created_by: string;
  resolved_by: string | null;
  resolved_at: string | null;
  created_at: string;
  updated_at: string;
};

export type TaskLiveness = {
  task_id: string;
  liveness: "ready" | "running" | "waiting" | "stalled";
  has_active_run: boolean;
  has_queued_wake: boolean;
  has_waiting_interaction: boolean;
  has_human_review: boolean;
  task_updated_at: string;
};

export type RunEvent = {
  id: number;
  run_id: string;
  ts: string;
  level: "info" | "warn" | "error" | "stdout" | "stderr";
  message: string;
};

export type Repo = {
  id: string;
  project_id: string;
  local_path: string;
  default_branch: string;
  status: "ok" | "missing" | "dirty";
};

export type BootstrapStatus = {
  bootstrapped: boolean;
  agents_count: number;
};

export type OrchestratorMapSkill = Skill & {
  source_name: string;
  recommended_agents: string[];
  trigger_keywords: string[];
  trigger_tags: string[];
  notes: string;
};

export type OrchestratorMapAgent = {
  id: string;
  name: string;
  role: string;
  cli_kind: "claude" | "codex";
  base_prompt: string;
  assigned_skills: string[];
  recommended_skills: string[];
};

export type OrchestratorMapLifecycle = {
  id: string;
  description: string;
  steps: Array<{ agent: string; skip_when: string[] }>;
};

export type OrchestratorMap = {
  sources: SkillSource[];
  skills: OrchestratorMapSkill[];
  agents: OrchestratorMapAgent[];
  lifecycles: OrchestratorMapLifecycle[];
};

function resolveAPIBase() {
  const configured = (import.meta.env.VITE_API_BASE || "").trim();
  if (configured === "" || configured === "/") {
    return "/api";
  }
  return configured.replace(/\/+$/, "");
}

const API_BASE = resolveAPIBase();
const TOKEN_KEY = "mini-paperclip-token";

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || "dev-token";
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token);
}

export function eventsURL() {
  return `${API_BASE}/events?token=${encodeURIComponent(getToken())}`;
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${getToken()}`);
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(`${API_BASE}${path}`, { ...init, headers });
  const text = await response.text();
  const contentType = response.headers.get("Content-Type") || "";
  const isJSON = contentType.includes("application/json") || contentType.includes("+json");
  if (text && !isJSON) {
    throw new Error(`API returned non-JSON for ${path} (HTTP ${response.status}). Check Vite proxy/backend URL.`);
  }
  let data: any = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch (error) {
      throw new Error(`API returned invalid JSON for ${path}: ${(error as Error).message}`);
    }
  }
  if (!response.ok) {
    throw new Error(data?.error?.message || response.statusText);
  }
  return data as T;
}

export const getBootstrapStatus = () => api<BootstrapStatus>("/bootstrap-status");
export const runBootstrap = () => api<BootstrapStatus>("/bootstrap/run", { method: "POST" });
export const listAgents = () => api<Agent[]>("/agents");
export const getAgent = (id: string) => api<Agent>(`/agents/${id}`);
export const createAgent = (body: Partial<Agent> & { skill_ids?: string[] }) => api<Agent>("/agents", { method: "POST", body: JSON.stringify(body) });
export const updateAgent = (id: string, body: Partial<Agent> & { skill_ids?: string[] }) => api<Agent>(`/agents/${id}`, { method: "PATCH", body: JSON.stringify(body) });
export const improveAgentPrompt = (id: string, body: Partial<Agent> & { skill_ids?: string[] }) => api<{ role_prompt: string }>(`/agents/${id}/improve-prompt`, { method: "POST", body: JSON.stringify(body) });
export const duplicateAgent = (id: string) => api<Agent>(`/agents/${id}/duplicate`, { method: "POST" });
export const setAgentEnabled = (id: string, enabled: boolean) => api<Agent>(`/agents/${id}/enabled`, { method: "POST", body: JSON.stringify({ enabled }) });
export const deleteAgent = (id: string) => api<{ deleted: boolean }>(`/agents/${id}`, { method: "DELETE" });
export const listProjects = () => api<Project[]>("/projects");
export const createProject = (body: Partial<Project>) => api<Project>("/projects", { method: "POST", body: JSON.stringify(body) });
export const getProject = (id: string) => api<Project>(`/projects/${id}`);
export const updateProject = (id: string, body: Partial<Project>) => api<Project>(`/projects/${id}`, { method: "PATCH", body: JSON.stringify(body) });
export const deleteProject = (id: string) => api<{ deleted: boolean }>(`/projects/${id}`, { method: "DELETE" });
export const addRepo = (projectId: string, body: { local_path: string; default_branch?: string }) => api<Repo>(`/projects/${projectId}/repos`, { method: "POST", body: JSON.stringify(body) });
export const deleteRepo = (id: string) => api<{ deleted: boolean }>(`/repos/${id}`, { method: "DELETE" });
export const listTasks = (projectId: string) => api<Task[]>(`/projects/${projectId}/tasks`);
export const createTask = (projectId: string, body: Partial<Task>) => api<Task>(`/projects/${projectId}/tasks`, { method: "POST", body: JSON.stringify(body) });
export const updateTask = (id: string, body: Partial<Task>) => api<Task>(`/tasks/${id}`, { method: "PATCH", body: JSON.stringify(body) });
export const listComments = (taskId: string) => api<Comment[]>(`/tasks/${taskId}/comments`);
export const addComment = (taskId: string, body: string) => api<Comment>(`/tasks/${taskId}/comments`, { method: "POST", body: JSON.stringify({ body }) });
export const listTaskArtifacts = (taskId: string) => api<TaskArtifact[]>(`/tasks/${taskId}/artifacts`);
export const createTaskArtifact = (taskId: string, body: Partial<TaskArtifact>) => api<TaskArtifact>(`/tasks/${taskId}/artifacts`, { method: "POST", body: JSON.stringify(body) });
export const updateTaskArtifact = (id: string, body: Partial<TaskArtifact>) => api<TaskArtifact>(`/task-artifacts/${id}`, { method: "PATCH", body: JSON.stringify(body) });
export const deleteTaskArtifact = (id: string) => api<{ deleted: boolean }>(`/task-artifacts/${id}`, { method: "DELETE" });
export const runTask = (taskId: string) => api<AgentWakeup>(`/tasks/${taskId}/run`, { method: "POST" });
export const listRuns = (taskId: string) => api<Run[]>(`/tasks/${taskId}/runs`);
export const listRunEvents = (runId: string, since = 0) => api<RunEvent[]>(`/runs/${runId}/events?since=${since}`);
export const listWakeups = (taskId: string) => api<AgentWakeup[]>(`/tasks/${taskId}/wakeups`);
export const listInteractions = (taskId: string) => api<TaskInteraction[]>(`/tasks/${taskId}/interactions`);
export const acceptInteraction = (id: string) => api<TaskInteraction>(`/task-interactions/${id}/accept`, { method: "POST" });
export const rejectInteraction = (id: string) => api<TaskInteraction>(`/task-interactions/${id}/reject`, { method: "POST" });
export const getTaskLiveness = (taskId: string) => api<TaskLiveness>(`/tasks/${taskId}/liveness`);
export const getActiveRun = (taskId: string) => api<Run | null>(`/tasks/${taskId}/active-run`);
export const heartbeat = () => api<{ queued: number }>("/heartbeat", { method: "POST" });
export const listInstalledSkills = (includeIgnored = false) => api<Skill[]>(`/skills${includeIgnored ? "?include_ignored=true" : ""}`);
export const getSkillTree = (id: string) => api<SkillTreeResponse>(`/skills/${id}/tree`);
export const getSkillContent = (id: string, path = "SKILL.md") => api<SkillContentResponse>(`/skills/${id}/content?path=${encodeURIComponent(path)}`);
export const listSkillSources = () => api<SkillSource[]>("/skill-sources");
export const createSkillSource = (body: Partial<SkillSource>) => api<SkillSource>("/skill-sources", { method: "POST", body: JSON.stringify(body) });
export const importGitHubSkill = (body: { url: string; name?: string }) => api<SkillSource>("/skill-sources/import-github-skill", { method: "POST", body: JSON.stringify(body) });
export const syncSkillSource = (id: string) => api<SkillSource>(`/skill-sources/${id}/sync`, { method: "POST" });
export const pinSkillSource = (id: string, sha: string) => api<SkillSource>(`/skill-sources/${id}/pin`, { method: "POST", body: JSON.stringify({ sha }) });
export const deleteSkillSource = (id: string) => api<{ deleted: boolean }>(`/skill-sources/${id}`, { method: "DELETE" });
export const updateSkill = (id: string, body: Partial<Skill>) => api<Skill>(`/skills/${id}`, { method: "PATCH", body: JSON.stringify(body) });
export const getOrchestratorMap = () => api<OrchestratorMap>("/orchestrator-map");
