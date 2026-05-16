export type Agent = {
  id: string;
  name: string;
  role: string;
  role_prompt: string;
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
};

export type SkillSource = {
  id: string;
  name: string;
  upstream_url: string;
  pinned_sha: string;
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
  status: "queued" | "running" | "done" | "error" | "cancelled";
  cli_kind: "claude" | "codex";
  started_at: string | null;
  finished_at: string | null;
  exit_code: number | null;
  worktree_path: string | null;
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

const API_BASE = import.meta.env.VITE_API_BASE || "/api";
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
  const data = text ? JSON.parse(text) : null;
  if (!response.ok) {
    throw new Error(data?.error?.message || response.statusText);
  }
  return data as T;
}

export const getBootstrapStatus = () => api<BootstrapStatus>("/bootstrap-status");
export const runBootstrap = () => api<BootstrapStatus>("/bootstrap/run", { method: "POST" });
export const listAgents = () => api<Agent[]>("/agents");
export const createAgent = (body: Partial<Agent> & { skill_ids?: string[] }) => api<Agent>("/agents", { method: "POST", body: JSON.stringify(body) });
export const updateAgent = (id: string, body: Partial<Agent> & { skill_ids?: string[] }) => api<Agent>(`/agents/${id}`, { method: "PATCH", body: JSON.stringify(body) });
export const duplicateAgent = (id: string) => api<Agent>(`/agents/${id}/duplicate`, { method: "POST" });
export const setAgentEnabled = (id: string, enabled: boolean) => api<Agent>(`/agents/${id}/enabled`, { method: "POST", body: JSON.stringify({ enabled }) });
export const deleteAgent = (id: string) => api<{ deleted: boolean }>(`/agents/${id}`, { method: "DELETE" });
export const listProjects = () => api<Project[]>("/projects");
export const createProject = (body: Partial<Project>) => api<Project>("/projects", { method: "POST", body: JSON.stringify(body) });
export const getProject = (id: string) => api<Project>(`/projects/${id}`);
export const updateProject = (id: string, body: Partial<Project>) => api<Project>(`/projects/${id}`, { method: "PATCH", body: JSON.stringify(body) });
export const addRepo = (projectId: string, body: { local_path: string; default_branch?: string }) => api<Repo>(`/projects/${projectId}/repos`, { method: "POST", body: JSON.stringify(body) });
export const deleteRepo = (id: string) => api<{ deleted: boolean }>(`/repos/${id}`, { method: "DELETE" });
export const listTasks = (projectId: string) => api<Task[]>(`/projects/${projectId}/tasks`);
export const createTask = (projectId: string, body: Partial<Task>) => api<Task>(`/projects/${projectId}/tasks`, { method: "POST", body: JSON.stringify(body) });
export const updateTask = (id: string, body: Partial<Task>) => api<Task>(`/tasks/${id}`, { method: "PATCH", body: JSON.stringify(body) });
export const listComments = (taskId: string) => api<Comment[]>(`/tasks/${taskId}/comments`);
export const addComment = (taskId: string, body: string) => api<Comment>(`/tasks/${taskId}/comments`, { method: "POST", body: JSON.stringify({ body }) });
export const runTask = (taskId: string) => api<Run>(`/tasks/${taskId}/run`, { method: "POST" });
export const listRuns = (taskId: string) => api<Run[]>(`/tasks/${taskId}/runs`);
export const listRunEvents = (runId: string, since = 0) => api<RunEvent[]>(`/runs/${runId}/events?since=${since}`);
export const heartbeat = () => api<{ queued: number }>("/heartbeat", { method: "POST" });
export const listInstalledSkills = () => api<Skill[]>("/skills");
export const listSkillSources = () => api<SkillSource[]>("/skill-sources");
export const syncSkillSource = (id: string) => api<SkillSource>(`/skill-sources/${id}/sync`, { method: "POST" });
export const pinSkillSource = (id: string, sha: string) => api<SkillSource>(`/skill-sources/${id}/pin`, { method: "POST", body: JSON.stringify({ sha }) });
