import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { Bot, Boxes, Check, FileText, FolderGit2, GitBranch, LayoutDashboard, MessageSquare, Monitor, Moon, MoreHorizontal, Play, Plus, RefreshCw, Save, Sun, Trash2 } from "lucide-react";
import "./styles.css";
import {
  Agent,
  AgentWakeup,
  BootstrapStatus,
  Comment,
  OrchestratorMap,
  Project,
  Run,
  RunEvent,
  Skill,
  SkillTreeEntry,
  SkillSource,
  Task,
  TaskArtifact,
  TaskInteraction,
  TaskLiveness,
  addComment,
  addRepo,
  acceptInteraction,
  createAgent,
  createSkillSource,
  createTaskArtifact,
  createProject,
  createTask,
  deleteAgent,
  deleteSkillSource,
  deleteTaskArtifact,
  deleteProject,
  deleteRepo,
  duplicateAgent,
  eventsURL,
  getAgent,
  getTaskLiveness,
  getBootstrapStatus,
  getOrchestratorMap,
  getSkillContent,
  getSkillTree,
  getProject,
  getToken,
  heartbeat,
  listAgents,
  listComments,
  listInteractions,
  listInstalledSkills,
  listProjects,
  listRunEvents,
  listRuns,
  listSkillSources,
  listTasks,
  listTaskArtifacts,
  listWakeups,
  pinSkillSource,
  runBootstrap,
  runTask,
  rejectInteraction,
  setAgentEnabled,
  setToken,
  syncSkillSource,
  updateAgent,
  updateTaskArtifact,
  updateTask,
  updateProject
} from "./lib/api";

type Route = "bootstrap" | "projects" | "project" | "board" | "agents" | "agent-new" | "agent" | "skills" | "map";

type RouteState = {
  route: Route;
  projectId: string | null;
  agentId: string | null;
};

type BootstrapState =
  | { phase: "loading" }
  | { phase: "ready"; status: BootstrapStatus }
  | { phase: "error"; message: string };

function routeFromPath(pathname = window.location.pathname): RouteState {
  const parts = pathname.split("/").filter(Boolean).map((part) => decodeURIComponent(part));
  if (parts[0] === "bootstrap") return { route: "bootstrap", projectId: null, agentId: null };
  if (parts[0] === "projects" && parts[1] && parts[2] === "board") return { route: "board", projectId: parts[1], agentId: null };
  if (parts[0] === "projects" && parts[1]) return { route: "project", projectId: parts[1], agentId: null };
  if (parts[0] === "agents" && parts[1] === "new") return { route: "agent-new", projectId: null, agentId: null };
  if (parts[0] === "agents" && parts[1]) return { route: "agent", projectId: null, agentId: parts[1] };
  if (parts[0] === "agents") return { route: "agents", projectId: null, agentId: null };
  if (parts[0] === "skills") return { route: "skills", projectId: null, agentId: null };
  if (parts[0] === "map") return { route: "map", projectId: null, agentId: null };
  return { route: "projects", projectId: null, agentId: null };
}

function pathForRoute(next: Route, id?: string) {
  switch (next) {
    case "bootstrap": return "/bootstrap";
    case "project": return id ? `/projects/${encodeURIComponent(id)}` : "/projects";
    case "board": return id ? `/projects/${encodeURIComponent(id)}/board` : "/projects";
    case "agents": return "/agents";
    case "agent-new": return "/agents/new";
    case "agent": return id ? `/agents/${encodeURIComponent(id)}` : "/agents";
    case "skills": return "/skills";
    case "map": return "/map";
    case "projects":
    default: return "/projects";
  }
}

type ThemeMode = "light" | "dark" | "system";

function resolveTheme(mode: ThemeMode): "light" | "dark" {
  if (mode === "system") {
    return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }
  return mode;
}

function useTheme(): { mode: ThemeMode; setMode: (mode: ThemeMode) => void } {
  const [mode, setModeState] = useState<ThemeMode>(() => {
    const stored = (typeof window !== "undefined" && window.localStorage.getItem("mp-theme")) as ThemeMode | null;
    return stored === "light" || stored === "dark" || stored === "system" ? stored : "system";
  });

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", resolveTheme(mode));
    try { window.localStorage.setItem("mp-theme", mode); } catch { /* ignore */ }
  }, [mode]);

  useEffect(() => {
    if (mode !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: light)");
    const handler = () => document.documentElement.setAttribute("data-theme", resolveTheme("system"));
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, [mode]);

  return { mode, setMode: setModeState };
}

function ThemeSwitcher() {
  const { mode, setMode } = useTheme();
  const options: Array<{ id: ThemeMode; label: string; Icon: typeof Sun }> = [
    { id: "light", label: "Light", Icon: Sun },
    { id: "system", label: "System", Icon: Monitor },
    { id: "dark", label: "Dark", Icon: Moon },
  ];
  return (
    <div className="theme-switch" role="group" aria-label="Theme">
      {options.map(({ id, label, Icon }) => (
        <button
          key={id}
          type="button"
          className={mode === id ? "active" : ""}
          onClick={() => setMode(id)}
          aria-pressed={mode === id}
          title={label}
        >
          <Icon size={14} />
        </button>
      ))}
    </div>
  );
}

function App() {
  const initialRoute = useMemo(() => routeFromPath(), []);
  const [route, setRoute] = useState<Route>(initialRoute.route);
  const [projectId, setProjectId] = useState<string | null>(initialRoute.projectId);
  const [agentId, setAgentId] = useState<string | null>(initialRoute.agentId);
  const [token, updateToken] = useState(getToken());
  const [bootstrapState, setBootstrapState] = useState<BootstrapState>({ phase: "loading" });

  function applyRoute(next: RouteState) {
    setProjectId(next.projectId);
    setAgentId(next.agentId);
    setRoute(next.route);
  }

  useEffect(() => {
    function syncFromHistory() {
      applyRoute(routeFromPath());
    }
    window.addEventListener("popstate", syncFromHistory);
    return () => window.removeEventListener("popstate", syncFromHistory);
  }, []);

  useEffect(() => {
    getBootstrapStatus()
      .then((status) => {
        setBootstrapState({ phase: "ready", status });
        if (!status.bootstrapped) {
          navigate("bootstrap", undefined, true);
        } else if (routeFromPath().route === "bootstrap") {
          navigate("projects", undefined, true);
        }
      })
      .catch((error) => setBootstrapState({ phase: "error", message: (error as Error).message }));
  }, []);

  const showBootstrapNav = bootstrapState.phase === "ready" && !bootstrapState.status.bootstrapped;

  function navigate(next: Route, id?: string, replace = false) {
    const path = pathForRoute(next, id);
    if (window.location.pathname !== path) {
      if (replace) {
        window.history.replaceState(null, "", path);
      } else {
        window.history.pushState(null, "", path);
      }
    }
    applyRoute(routeFromPath(path));
  }

  return (
    <main>
      <aside>
        <div className="brand"><Boxes size={22} /> mini-Paperclip</div>
        {showBootstrapNav && <button className={route === "bootstrap" ? "active" : ""} onClick={() => navigate("bootstrap")}>Bootstrap</button>}
        <button className={route === "projects" || route === "project" || route === "board" ? "active" : ""} onClick={() => navigate("projects")}><FolderGit2 size={16} /> Projects</button>
        <button className={route === "agents" || route === "agent-new" || route === "agent" ? "active" : ""} onClick={() => navigate("agents")}><Bot size={16} /> Agents</button>
        <button className={route === "skills" ? "active" : ""} onClick={() => navigate("skills")}><RefreshCw size={16} /> Skill Sources</button>
        <button className={route === "map" ? "active" : ""} onClick={() => navigate("map")}><GitBranch size={16} /> Map</button>
        <div className="sidebar-footer">
          <ThemeSwitcher />
          <label className="token">API token<input value={token} onChange={(event) => { updateToken(event.target.value); setToken(event.target.value); }} /></label>
        </div>
      </aside>
      <section>
        {route === "bootstrap" && <BootstrapPage bootstrapState={bootstrapState} onDone={(status) => { setBootstrapState({ phase: "ready", status }); navigate("projects"); }} />}
        {route === "projects" && <ProjectsPage openProject={(id) => navigate("project", id)} openBoard={(id) => navigate("board", id)} />}
        {route === "project" && projectId && <ProjectPage id={projectId} onOpenBoard={() => navigate("board", projectId)} onDeleted={() => navigate("projects")} />}
        {route === "board" && projectId && <BoardPage id={projectId} />}
        {route === "agents" && <AgentsPage openAgent={(id) => navigate("agent", id)} openAddAgent={() => navigate("agent-new")} />}
        {route === "agent-new" && <AgentCreatePage onCreated={(id) => navigate("agent", id)} onCancel={() => navigate("agents")} />}
        {route === "agent" && agentId && <AgentDetailPage id={agentId} onSaved={(id) => navigate("agent", id)} />}
        {route === "skills" && <SkillSourcesPage />}
        {route === "map" && <MapPage />}
      </section>
    </main>
  );
}

function BootstrapPage({ bootstrapState, onDone }: { bootstrapState: BootstrapState; onDone: (status: BootstrapStatus) => void }) {
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (bootstrapState.phase === "loading") {
      setMessage("Checking bootstrap status...");
    } else if (bootstrapState.phase === "error") {
      setMessage(bootstrapState.message);
    } else {
      setMessage(bootstrapState.status.bootstrapped ? `${bootstrapState.status.agents_count} agents seeded.` : "Bootstrap is required.");
    }
  }, [bootstrapState]);

  async function run() {
    setBusy(true);
    try {
      const status = await runBootstrap();
      setMessage(`${status.agents_count} agents ready.`);
      onDone(status);
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const canRunBootstrap = bootstrapState.phase === "ready" && !bootstrapState.status.bootstrapped;

  return <Panel title="Bootstrap"><p>{message}</p>{canRunBootstrap && <button onClick={run} disabled={busy}><Check size={16} /> Run bootstrap</button>}</Panel>;
}

function ProjectsPage({ openProject, openBoard }: { openProject: (id: string) => void; openBoard: (id: string) => void }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [form, setForm] = useState<Pick<Project, "name" | "description" | "default_cli_kind" | "default_branch_strategy">>({ name: "", description: "", default_cli_kind: "codex", default_branch_strategy: "worktree-per-run" });
  const [error, setError] = useState("");
  useEffect(() => { listProjects().then(setProjects).catch((e) => setError(e.message)); }, []);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    try {
      const created = await createProject(form);
      setProjects([created, ...projects]);
      setForm({ ...form, name: "", description: "" });
    } catch (e) { setError((e as Error).message); }
  }

  return <Panel title="Projects">
    <form className="grid-form" onSubmit={submit}>
      <input placeholder="Project name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
      <input placeholder="Description" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
      <select value={form.default_cli_kind} onChange={(e) => setForm({ ...form, default_cli_kind: e.target.value as "claude" | "codex" })}><option>claude</option><option>codex</option></select>
      <button><Plus size={16} /> Create</button>
    </form>
    <Error text={error} />
    <div className="list">{projects.map((project) => <article key={project.id}>
      <h3>{project.name}</h3>
      <p>{project.description || "No description"}</p>
      <span>{project.default_cli_kind} · {project.repos?.length || 0} repos</span>
      <div className="toolbar">
        <button type="button" onClick={() => openProject(project.id)}><FolderGit2 size={16} /> Details</button>
        <button type="button" onClick={() => openBoard(project.id)}><LayoutDashboard size={16} /> Board</button>
      </div>
    </article>)}</div>
  </Panel>;
}

function ProjectPage({ id, onOpenBoard, onDeleted }: { id: string; onOpenBoard: () => void; onDeleted: () => void }) {
  const [project, setProject] = useState<Project | null>(null);
  const [repoPath, setRepoPath] = useState("");
  const [deleteConfirmName, setDeleteConfirmName] = useState("");
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    getProject(id).then(setProject).catch((e) => setError(e.message));
  }, [id]);
  if (!project) return <Panel title="Project"><Error text={error || "Loading..."} /></Panel>;

  async function save() {
    const current = project;
    if (!current) return;
    try { setProject(await updateProject(current.id, current)); } catch (e) { setError((e as Error).message); }
  }
  async function attachRepo(event: React.FormEvent) {
    event.preventDefault();
    const current = project;
    if (!current) return;
    try {
      await addRepo(current.id, { local_path: repoPath });
      setProject(await getProject(current.id));
      setRepoPath("");
    } catch (e) { setError((e as Error).message); }
  }
  async function removeProject() {
    const current = project;
    if (!current || deleteConfirmName !== current.name) return;
    setDeleteBusy(true);
    try {
      await deleteProject(current.id);
      onDeleted();
    } catch (e) {
      setError((e as Error).message);
      setDeleteBusy(false);
    }
  }

  const repoCount = project.repos?.length || 0;

  return <Panel title={project.name}>
    <Error text={error} />

    <section className="detail-card">
      <header className="detail-card-header">
        <div>
          <h2 className="detail-card-title">Overview</h2>
          <p className="detail-card-sub">Project identity and default execution settings.</p>
        </div>
        <div className="detail-card-actions">
          <button type="button" onClick={onOpenBoard}><LayoutDashboard size={16} /> Open board</button>
          <button onClick={save}><Save size={16} /> Save</button>
        </div>
      </header>
      <div className="detail-card-body field-grid">
        <label className="field">
          <span className="field-label">Project name</span>
          <input value={project.name} onChange={(e) => setProject({ ...project, name: e.target.value })} />
        </label>
        <label className="field">
          <span className="field-label">Default CLI</span>
          <select value={project.default_cli_kind} onChange={(e) => setProject({ ...project, default_cli_kind: e.target.value as "claude" | "codex" })}><option>claude</option><option>codex</option></select>
        </label>
        <label className="field field-wide">
          <span className="field-label">Description</span>
          <input value={project.description || ""} placeholder="No description" onChange={(e) => setProject({ ...project, description: e.target.value })} />
        </label>
      </div>
    </section>

    <section className="detail-card">
      <header className="detail-card-header">
        <div>
          <h2 className="detail-card-title">Repositories <span className="count-badge">{repoCount}</span></h2>
          <p className="detail-card-sub">Allowlisted local paths the agents may operate on.</p>
        </div>
      </header>
      <div className="detail-card-body">
        <form className="inline-form" onSubmit={attachRepo}>
          <input placeholder="/path/to/local/repo" value={repoPath} onChange={(e) => setRepoPath(e.target.value)} />
          <button><Plus size={16} /> Add repo</button>
        </form>
        {repoCount === 0 ? (
          <p className="empty-state">No repositories attached yet.</p>
        ) : (
          <ul className="repo-list">
            {project.repos?.map((repo) => (
              <li key={repo.id} className="repo-row">
                <div className="repo-info">
                  <span className="repo-path">{repo.local_path}</span>
                  <span className="repo-meta">{repo.default_branch || "main"} · {repo.status}</span>
                </div>
                <button type="button" className="ghost-danger" onClick={async () => { await deleteRepo(repo.id); setProject(await getProject(project.id)); }}><Trash2 size={14} /> Remove</button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>

    <section className="detail-card danger-card">
      <header className="detail-card-header">
        <div>
          <h2 className="detail-card-title">Delete project</h2>
          <p className="detail-card-sub">This action is permanent. Type <strong>{project.name}</strong> to confirm.</p>
        </div>
      </header>
      <div className="detail-card-body">
        <div className="confirm-form">
          <input value={deleteConfirmName} onChange={(e) => setDeleteConfirmName(e.target.value)} placeholder={project.name} />
          <button type="button" className="danger-button" onClick={removeProject} disabled={deleteBusy || deleteConfirmName !== project.name}><Trash2 size={16} /> Delete project</button>
        </div>
      </div>
    </section>
  </Panel>;
}

type StatusFilter = "all" | Task["status"];
type AssigneeFilter = "all" | "unassigned" | string;

const TASK_STATUSES: Task["status"][] = ["todo", "in_progress", "in_review", "blocked", "done", "cancelled"];
const ARTIFACT_KINDS: TaskArtifact["kind"][] = ["pm_document", "pm_handoff", "em_document", "em_handoff", "qa_report", "implementation_note", "run_log", "other"];
const ARTIFACT_LABELS: Record<TaskArtifact["kind"], string> = {
  pm_document: "PM Document",
  pm_handoff: "PM Handoff",
  em_document: "EM Document",
  em_handoff: "EM Handoff",
  qa_report: "QA Report",
  implementation_note: "Implementation Note",
  run_log: "Run Log",
  other: "Other",
};

function readStatusFilter(): StatusFilter {
  const value = new URLSearchParams(window.location.search).get("status");
  return value && TASK_STATUSES.includes(value as Task["status"]) ? value as Task["status"] : "all";
}

function readAssigneeFilter(agents: Agent[]): AssigneeFilter {
  const value = new URLSearchParams(window.location.search).get("assignee");
  if (!value) return "all";
  if (value === "unassigned") return value;
  return agents.some((agent) => agent.id === value) ? value : "all";
}

function BoardPage({ id }: { id: string }) {
  const [project, setProject] = useState<Project | null>(null);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [taskForm, setTaskForm] = useState({ title: "", description: "", assignee_agent_id: "pm", priority: 0, tags: "", lifecycle_id: "default" });
  const [statusFilter, setStatusFilter] = useState<StatusFilter>(() => readStatusFilter());
  const [assigneeFilter, setAssigneeFilter] = useState<AssigneeFilter>("all");
  const [error, setError] = useState("");

  useEffect(() => {
    getProject(id).then(setProject).catch((e) => setError(e.message));
    listAgents().then(setAgents).catch((e) => setError(e.message));
    listTasks(id).then(setTasks).catch((e) => setError(e.message));
    const events = new EventSource(eventsURL());
    events.addEventListener("mp_events", () => listTasks(id).then(setTasks).catch(() => undefined));
    return () => events.close();
  }, [id]);

  useEffect(() => {
    setAssigneeFilter(readAssigneeFilter(agents));
  }, [agents]);

  useEffect(() => {
    function syncFiltersFromHistory() {
      setStatusFilter(readStatusFilter());
      setAssigneeFilter(readAssigneeFilter(agents));
    }
    window.addEventListener("popstate", syncFiltersFromHistory);
    return () => window.removeEventListener("popstate", syncFiltersFromHistory);
  }, [agents]);

  if (!project) return <Panel title="Board"><Error text={error || "Loading..."} /></Panel>;

  const filteredTasks = tasks.filter((task) => {
    if (statusFilter !== "all" && task.status !== statusFilter) return false;
    if (assigneeFilter === "unassigned") return task.assignee_agent_id === null;
    if (assigneeFilter !== "all" && task.assignee_agent_id !== assigneeFilter) return false;
    return true;
  });
  const filtersActive = statusFilter !== "all" || assigneeFilter !== "all";

  function updateFilters(nextStatus: StatusFilter, nextAssignee: AssigneeFilter) {
    const params = new URLSearchParams(window.location.search);
    if (nextStatus === "all") {
      params.delete("status");
    } else {
      params.set("status", nextStatus);
    }
    if (nextAssignee === "all") {
      params.delete("assignee");
    } else {
      params.set("assignee", nextAssignee);
    }
    const query = params.toString();
    window.history.pushState(null, "", `${window.location.pathname}${query ? `?${query}` : ""}`);
    setStatusFilter(nextStatus);
    setAssigneeFilter(nextAssignee);
  }

  async function submitTask(event: React.FormEvent) {
    event.preventDefault();
    const current = project;
    if (!current) return;
    try {
      const tags = taskForm.tags.split(",").map((tag) => tag.trim()).filter(Boolean);
      const task = await createTask(current.id, { ...taskForm, tags, lifecycle_id: taskForm.lifecycle_id, assignee_agent_id: taskForm.assignee_agent_id || null, repo_id: current.repos?.[0]?.id || null, status: "todo" });
      setTasks([task, ...tasks]);
      setTaskForm({ title: "", description: "", assignee_agent_id: "pm", priority: 0, tags: "", lifecycle_id: "default" });
    } catch (e) { setError((e as Error).message); }
  }
  async function moveTask(task: Task, status: Task["status"]) {
    const updated = await updateTask(task.id, { ...task, status });
    setTasks(tasks.map((item) => item.id === updated.id ? updated : item));
    if (selectedTask?.id === updated.id) setSelectedTask(updated);
  }

  return <Panel title={`${project.name} Board`}>
    <Error text={error} />
    <form className="task-form" onSubmit={submitTask}>
      <input placeholder="New task title" value={taskForm.title} onChange={(e) => setTaskForm({ ...taskForm, title: e.target.value })} required />
      <input placeholder="Description" value={taskForm.description} onChange={(e) => setTaskForm({ ...taskForm, description: e.target.value })} />
      <select value={taskForm.assignee_agent_id} onChange={(e) => setTaskForm({ ...taskForm, assignee_agent_id: e.target.value })}>{agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}</select>
      <select value={taskForm.lifecycle_id} onChange={(e) => setTaskForm({ ...taskForm, lifecycle_id: e.target.value })}>
        <option value="default">Default</option>
        <option value="backend-only">Backend only</option>
        <option value="frontend-only">Frontend only</option>
      </select>
      <input placeholder="Tags, comma-separated" value={taskForm.tags} onChange={(e) => setTaskForm({ ...taskForm, tags: e.target.value })} />
      <button><Plus size={16} /> Add task</button>
      <button type="button" onClick={async () => { await heartbeat(); setTasks(await listTasks(project.id)); }}><RefreshCw size={16} /> Heartbeat</button>
    </form>
    <div className="board-filters" aria-label="Board filters">
      <label>
        <span>Status</span>
        <select value={statusFilter} onChange={(e) => updateFilters(e.target.value as StatusFilter, assigneeFilter)}>
          <option value="all">All statuses</option>
          {TASK_STATUSES.map((status) => <option key={status} value={status}>{STATUS_LABELS[status]}</option>)}
        </select>
      </label>
      <label>
        <span>Assignee</span>
        <select value={assigneeFilter} onChange={(e) => updateFilters(statusFilter, e.target.value)}>
          <option value="all">All assignees</option>
          <option value="unassigned">Unassigned</option>
          {agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}
        </select>
      </label>
    </div>
    {filtersActive && filteredTasks.length === 0 && <p className="filtered-empty">No tasks match the selected filters.</p>}
    <Kanban tasks={filteredTasks} agents={agents} onOpen={setSelectedTask} onMove={moveTask} />
    {selectedTask && <TaskDrawer task={selectedTask} agents={agents} onClose={() => setSelectedTask(null)} onRefresh={async () => { setTasks(await listTasks(project.id)); setSelectedTask(await updateTask(selectedTask.id, selectedTask)); }} />}
  </Panel>;
}

const STATUS_LABELS: Record<Task["status"], string> = {
  todo: "Todo",
  in_progress: "In Progress",
  in_review: "In Review",
  blocked: "Blocked",
  done: "Done",
  cancelled: "Cancelled",
};

function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

function priorityClass(priority: number): string {
  if (priority >= 8) return "high";
  if (priority >= 4) return "med";
  return "";
}

function shortId(id: string): string {
  return id.slice(0, 6).toUpperCase();
}

function Kanban({ tasks, agents, onOpen, onMove }: { tasks: Task[]; agents: Agent[]; onOpen: (task: Task) => void; onMove: (task: Task, status: Task["status"]) => void }) {
  const agentName = (id: string | null) => agents.find((agent) => agent.id === id)?.name || id || "Unassigned";
  return (
    <div className="kanban">
      {TASK_STATUSES.map((status) => {
        const items = tasks.filter((task) => task.status === status);
        return (
          <section className="column" key={status}>
            <header className="column-header">
              <span className={`col-icon status-${status}`} />
              <span className="col-name">{STATUS_LABELS[status]}</span>
              <span className="col-count">{items.length}</span>
              <span className="spacer" />
              <button type="button" className="col-action" aria-label="Column actions">
                <MoreHorizontal size={14} />
              </button>
              <button type="button" className="col-action" aria-label="Add task to column">
                <Plus size={14} />
              </button>
            </header>
            <div className="column-body">
              {items.length === 0 ? (
                <div className="column-empty">No tasks</div>
              ) : (
                items.map((task) => {
                  const aName = agentName(task.assignee_agent_id);
                  return (
                    <article className="task-card" key={task.id} onClick={() => onOpen(task)}>
                      <div className="task-card-top">
                        <span className="task-id">TASK-{shortId(task.id)}</span>
                        <span className={`task-priority ${priorityClass(task.priority)}`}>P{task.priority}</span>
                      </div>
                      <h3 className="task-title">{task.title}</h3>
                      {task.description && <p className="task-desc">{task.description}</p>}
                      {(task.lifecycle_id || task.tags?.length > 0) && <div className="tag-row">
                        {task.lifecycle_id && <span>{task.lifecycle_id}</span>}
                        {(task.tags || []).slice(0, 3).map((tag) => <span key={tag}>{tag}</span>)}
                      </div>}
                      <div className="task-footer">
                        <span className="task-chip agent" title={aName}>
                          <span className="task-avatar" aria-hidden="true">{initialsOf(aName)}</span>
                          {aName}
                        </span>
                        <select
                          value={task.status}
                          onClick={(e) => e.stopPropagation()}
                          onChange={(e) => onMove(task, e.target.value as Task["status"])}
                          aria-label="Move task"
                        >
                          {TASK_STATUSES.map((item) => <option key={item} value={item}>{STATUS_LABELS[item]}</option>)}
                        </select>
                      </div>
                    </article>
                  );
                })
              )}
            </div>
          </section>
        );
      })}
    </div>
  );
}

function TaskDrawer({ task, agents, onClose, onRefresh }: { task: Task; agents: Agent[]; onClose: () => void; onRefresh: () => Promise<void> }) {
  const [comments, setComments] = useState<Comment[]>([]);
  const [artifacts, setArtifacts] = useState<TaskArtifact[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [wakeups, setWakeups] = useState<AgentWakeup[]>([]);
  const [interactions, setInteractions] = useState<TaskInteraction[]>([]);
  const [liveness, setLiveness] = useState<TaskLiveness | null>(null);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [comment, setComment] = useState("");
  const [artifactForm, setArtifactForm] = useState<{ kind: TaskArtifact["kind"]; title: string; body: string; format: TaskArtifact["format"] }>({ kind: "pm_document", title: "", body: "", format: "markdown" });
  const [editingArtifact, setEditingArtifact] = useState<TaskArtifact | null>(null);
  const latestRun = runs[0];
  useEffect(() => {
    listComments(task.id).then(setComments);
    listTaskArtifacts(task.id).then(setArtifacts);
    listRuns(task.id).then(setRuns);
    listWakeups(task.id).then(setWakeups);
    listInteractions(task.id).then(setInteractions);
    getTaskLiveness(task.id).then(setLiveness);
  }, [task.id]);
  useEffect(() => {
    if (!latestRun) return;
    const timer = window.setInterval(() => {
      const since = events.length ? events[events.length - 1].id : 0;
      listRunEvents(latestRun.id, since).then((items) => items.length && setEvents((prev) => [...prev, ...items])).catch(() => undefined);
    }, 1500);
    return () => window.clearInterval(timer);
  }, [latestRun?.id, events]);
  async function refreshArtifacts() {
    setArtifacts(await listTaskArtifacts(task.id));
  }
  async function refreshControlPlane() {
    const [nextRuns, nextWakeups, nextInteractions, nextLiveness] = await Promise.all([
      listRuns(task.id),
      listWakeups(task.id),
      listInteractions(task.id),
      getTaskLiveness(task.id),
    ]);
    setRuns(nextRuns);
    setWakeups(nextWakeups);
    setInteractions(nextInteractions);
    setLiveness(nextLiveness);
  }
  async function submitArtifact(event: React.FormEvent) {
    event.preventDefault();
    await createTaskArtifact(task.id, { ...artifactForm, metadata: {} });
    setArtifactForm({ kind: "pm_document", title: "", body: "", format: "markdown" });
    await refreshArtifacts();
  }
  async function saveArtifact(event: React.FormEvent) {
    event.preventDefault();
    if (!editingArtifact) return;
    await updateTaskArtifact(editingArtifact.id, {
      kind: editingArtifact.kind,
      title: editingArtifact.title,
      body: editingArtifact.body,
      format: editingArtifact.format,
      metadata: editingArtifact.metadata,
    });
    setEditingArtifact(null);
    await refreshArtifacts();
  }
  return <div className="drawer">
    <div className="drawer-header"><h2>{task.title}</h2><button onClick={onClose}>Close</button></div>
    <p>{task.description || "No description"}</p>
    <span>{agents.find((agent) => agent.id === task.assignee_agent_id)?.name || "Unassigned"} · {task.status} · {task.lifecycle_id || "default"} · retries {task.retry_count}</span>
    <div className="tag-row"><span className={`liveness-${liveness?.liveness || "ready"}`}>{liveness?.liveness || "ready"}</span>{task.execution_state && <span>{task.execution_state}</span>}</div>
    {(task.tags || []).length > 0 && <div className="tag-row">{task.tags.map((tag) => <span key={tag}>{tag}</span>)}</div>}
    <div className="toolbar"><button onClick={async () => { await runTask(task.id); await onRefresh(); await refreshControlPlane(); }}><Play size={16} /> Run now</button></div>
    <h3>Interactions</h3>
    <div className="runs">
      {interactions.length === 0 && <p>No interactions yet.</p>}
      {interactions.map((interaction) => (
        <article key={interaction.id} className="artifact-item">
          <div className="artifact-head"><strong>{interaction.title || interaction.kind}</strong><span>{interaction.kind} · {interaction.status} · {interaction.continuation_policy}</span></div>
          {interaction.summary && <p>{interaction.summary}</p>}
          {interaction.status === "open" && <div className="toolbar">
            <button type="button" onClick={async () => { await acceptInteraction(interaction.id); await refreshControlPlane(); }}><Check size={16} /> Accept</button>
            <button type="button" onClick={async () => { await rejectInteraction(interaction.id); await refreshControlPlane(); }}><Trash2 size={16} /> Reject</button>
          </div>}
        </article>
      ))}
    </div>
    <h3>Artifacts</h3>
    <div className="artifacts">
      {artifacts.length === 0 && <p>No artifacts yet.</p>}
      {artifacts.map((artifact) => (
        <article key={artifact.id} className="artifact-item">
          {editingArtifact?.id === artifact.id ? (
            <form className="artifact-form" onSubmit={saveArtifact}>
              <div className="artifact-grid">
                <select value={editingArtifact.kind} onChange={(e) => setEditingArtifact({ ...editingArtifact, kind: e.target.value as TaskArtifact["kind"] })}>
                  {ARTIFACT_KINDS.map((kind) => <option key={kind} value={kind}>{ARTIFACT_LABELS[kind]}</option>)}
                </select>
                <select value={editingArtifact.format} onChange={(e) => setEditingArtifact({ ...editingArtifact, format: e.target.value as TaskArtifact["format"] })}>
                  <option value="markdown">Markdown</option>
                  <option value="text">Text</option>
                  <option value="json">JSON</option>
                </select>
              </div>
              <input value={editingArtifact.title} onChange={(e) => setEditingArtifact({ ...editingArtifact, title: e.target.value })} required />
              <textarea value={editingArtifact.body} onChange={(e) => setEditingArtifact({ ...editingArtifact, body: e.target.value })} />
              <div className="toolbar"><button><Save size={16} /> Save</button><button type="button" onClick={() => setEditingArtifact(null)}>Cancel</button></div>
            </form>
          ) : (
            <>
              <div className="artifact-head"><strong>{artifact.title}</strong><span>{ARTIFACT_LABELS[artifact.kind]} · {artifact.format} · {artifact.created_by}</span></div>
              {artifact.body && <pre>{artifact.body}</pre>}
              <div className="toolbar">
                <button type="button" onClick={() => setEditingArtifact(artifact)}><Save size={16} /> Edit</button>
                {!artifact.run_id && <button type="button" onClick={async () => { await deleteTaskArtifact(artifact.id); await refreshArtifacts(); }}><Trash2 size={16} /> Delete</button>}
              </div>
            </>
          )}
        </article>
      ))}
    </div>
    <form className="artifact-form" onSubmit={submitArtifact}>
      <div className="artifact-grid">
        <select value={artifactForm.kind} onChange={(e) => setArtifactForm({ ...artifactForm, kind: e.target.value as TaskArtifact["kind"] })}>
          {ARTIFACT_KINDS.map((kind) => <option key={kind} value={kind}>{ARTIFACT_LABELS[kind]}</option>)}
        </select>
        <select value={artifactForm.format} onChange={(e) => setArtifactForm({ ...artifactForm, format: e.target.value as TaskArtifact["format"] })}>
          <option value="markdown">Markdown</option>
          <option value="text">Text</option>
          <option value="json">JSON</option>
        </select>
      </div>
      <input value={artifactForm.title} onChange={(e) => setArtifactForm({ ...artifactForm, title: e.target.value })} placeholder="Artifact title" required />
      <textarea value={artifactForm.body} onChange={(e) => setArtifactForm({ ...artifactForm, body: e.target.value })} placeholder="Artifact body" />
      <button><Plus size={16} /> Add artifact</button>
    </form>
    <h3>Comments</h3>
    <div className="timeline">{comments.map((item) => <p key={item.id}><strong>{item.author}</strong>: {item.body}</p>)}</div>
    <form className="comment-form" onSubmit={async (e) => { e.preventDefault(); await addComment(task.id, comment); setComment(""); setComments(await listComments(task.id)); }}><input value={comment} onChange={(e) => setComment(e.target.value)} placeholder="Add comment" /><button><MessageSquare size={16} /> Comment</button></form>
    <h3>Wakeups</h3>
    <div className="runs">{wakeups.length === 0 ? <p>No wakeups yet.</p> : wakeups.map((wakeup) => <p key={wakeup.id}>{wakeup.status} · {wakeup.source} · {wakeup.reason} · coalesced {wakeup.coalesced_count}</p>)}</div>
    <h3>Runs</h3>
    <div className="runs">{runs.map((run) => <p key={run.id}>{run.status} · {run.cli_kind} · wake {run.wakeup_id ? shortId(run.wakeup_id) : "legacy"} · {run.started_at || "not started"}</p>)}</div>
    <h3>Latest Log</h3>
    <pre>{events.map((event) => `[${event.level}] ${event.message}`).join("\n") || "No events yet."}</pre>
  </div>;
}

function recommendationNamesForAgent(map: OrchestratorMap | null, agentId: string) {
  return new Set(map?.agents.find((item) => item.id === agentId)?.recommended_skills || []);
}

function SkillCheckboxes({
  skills,
  selectedSkillIds,
  recommendedSkillNames,
  onToggle,
}: {
  skills: Skill[];
  selectedSkillIds: string[];
  recommendedSkillNames: Set<string>;
  onToggle: (skill: Skill, checked: boolean) => void;
}) {
  const selected = new Set(selectedSkillIds);
  return <div className="skill-picker">{skills.map((skill) => {
    const recommended = recommendedSkillNames.has(skill.name);
    return <label key={skill.id} className={recommended ? "recommended-skill" : undefined}>
      <input type="checkbox" checked={selected.has(skill.id)} onChange={(e) => onToggle(skill, e.target.checked)} />
      <span>{skill.name}</span>
      {recommended && <em>Recommended</em>}
    </label>;
  })}</div>;
}

function AgentsPage({ openAgent, openAddAgent }: { openAgent: (id: string) => void; openAddAgent: () => void }) {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [map, setMap] = useState<OrchestratorMap | null>(null);
  const [error, setError] = useState("");
  async function refresh() {
    const [nextAgents, nextMap] = await Promise.all([listAgents(), getOrchestratorMap()]);
    setAgents(nextAgents);
    setMap(nextMap);
  }
  useEffect(() => {
    refresh().catch((e) => setError(e.message));
  }, []);

  return <Panel title="Agents">
    <div className="toolbar page-actions">
      <button type="button" onClick={openAddAgent}><Plus size={16} /> Add Agent</button>
    </div>
    <div className="list">
      {agents.map((agent) => {
        const mapAgent = map?.agents.find((item) => item.id === agent.id);
        return <article className="clickable" key={agent.id} onClick={() => openAgent(agent.id)}>
        <h3>{agent.name}</h3>
        <span>{agent.role} · {agent.cli_kind} · {agent.enabled ? "enabled" : "disabled"} · {agent.skills?.length || 0} skills</span>
        {mapAgent && <p className="empty-state">Recommended: {mapAgent.recommended_skills.length ? mapAgent.recommended_skills.join(", ") : "none"}</p>}
        <div className="toolbar">
          <button type="button" onClick={async (e) => { e.stopPropagation(); const copy = await duplicateAgent(agent.id); await refresh(); openAgent(copy.id); }}><Plus size={16} /> Duplicate</button>
          <button type="button" onClick={async (e) => { e.stopPropagation(); await deleteAgent(agent.id); await refresh(); }}><Trash2 size={16} /> Delete</button>
        </div>
      </article>;
      })}
    </div>
    <Error text={error} />
  </Panel>;
}

function AgentCreatePage({ onCreated, onCancel }: { onCreated: (id: string) => void; onCancel: () => void }) {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [map, setMap] = useState<OrchestratorMap | null>(null);
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([]);
  const [skillsTouched, setSkillsTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [form, setForm] = useState({
    id: "",
    name: "",
    role: "",
    role_prompt: "",
    cli_kind: "codex" as Agent["cli_kind"],
  });

  useEffect(() => {
    Promise.all([listInstalledSkills(), getOrchestratorMap()])
      .then(([nextSkills, nextMap]) => {
        setSkills(nextSkills);
        setMap(nextMap);
      })
      .catch((e) => setError(e.message));
  }, []);

  const recommendedSkillNames = useMemo(() => recommendationNamesForAgent(map, form.id), [map, form.id]);

  useEffect(() => {
    if (skillsTouched) return;
    setSelectedSkillIds(skills.filter((skill) => recommendedSkillNames.has(skill.name)).map((skill) => skill.id));
  }, [recommendedSkillNames, skills, skillsTouched]);

  function toggleSkill(skill: Skill, checked: boolean) {
    setSkillsTouched(true);
    setSelectedSkillIds((current) => checked
      ? [...current.filter((id) => id !== skill.id), skill.id]
      : current.filter((id) => id !== skill.id));
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const created = await createAgent({ ...form, enabled: true, skill_ids: selectedSkillIds });
      onCreated(created.id);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return <Panel title="Add Agent">
    <form className="editor" onSubmit={submit}>
      <h2 className="section-title">Agent</h2>
      <div className="form-grid">
        <input value={form.id} onChange={(e) => setForm({ ...form, id: e.target.value })} placeholder="Optional stable ID" />
        <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Name" required />
        <input value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })} placeholder="Role" required />
        <select value={form.cli_kind} onChange={(e) => setForm({ ...form, cli_kind: e.target.value as Agent["cli_kind"] })}>
          <option value="codex">codex</option>
          <option value="claude">claude</option>
        </select>
      </div>
      <textarea value={form.role_prompt} onChange={(e) => setForm({ ...form, role_prompt: e.target.value })} placeholder="System prompt" required />
      <h2 className="section-title">Skills</h2>
      <SkillCheckboxes skills={skills} selectedSkillIds={selectedSkillIds} recommendedSkillNames={recommendedSkillNames} onToggle={toggleSkill} />
      <div className="toolbar">
        <button disabled={busy}><Plus size={16} /> Create agent</button>
        <button type="button" onClick={onCancel}>Cancel</button>
      </div>
    </form>
    <Error text={error} />
  </Panel>;
}

function AgentDetailPage({ id, onSaved }: { id: string; onSaved: (id: string) => void }) {
  const [agent, setAgent] = useState<Agent | null>(null);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [map, setMap] = useState<OrchestratorMap | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    Promise.all([getAgent(id), listInstalledSkills(), getOrchestratorMap()])
      .then(([nextAgent, nextSkills, nextMap]) => {
        setAgent(nextAgent);
        setSkills(nextSkills);
        setMap(nextMap);
      })
      .catch((e) => setError(e.message));
  }, [id]);

  if (!agent) return <Panel title="Agent"><Error text={error || "Loading..."} /></Panel>;
  const recommendedSkillNames = recommendationNamesForAgent(map, agent.id);

  async function save() {
    const current = agent;
    if (!current) return;
    setBusy(true);
    try {
      const saved = await updateAgent(current.id, {
        name: current.name,
        role: current.role,
        role_prompt: current.role_prompt,
        cli_kind: current.cli_kind,
        cli_profile: current.cli_profile,
        enabled: current.enabled,
        skill_ids: (current.skills || []).map((skill) => skill.id),
      });
      setAgent(saved);
      onSaved(saved.id);
    } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }

  function toggleSkill(skill: Skill, checked: boolean) {
    const currentSkills = agent?.skills || [];
    const nextSkills = checked
      ? [...currentSkills.filter((item) => item.id !== skill.id), skill].sort((a, b) => a.name.localeCompare(b.name))
      : currentSkills.filter((item) => item.id !== skill.id);
    if (agent) setAgent({ ...agent, skills: nextSkills });
  }

  return <Panel title={agent.name}>
    <div className="editor">
      <input value={agent.name} onChange={(e) => setAgent({ ...agent, name: e.target.value })} aria-label="Agent name" />
      <input value={agent.role} onChange={(e) => setAgent({ ...agent, role: e.target.value })} aria-label="Agent role" />
      <select value={agent.cli_kind} onChange={(e) => setAgent({ ...agent, cli_kind: e.target.value as Agent["cli_kind"] })} aria-label="Agent CLI">
        <option value="codex">codex</option>
        <option value="claude">claude</option>
      </select>
      <input value={agent.cli_profile || ""} onChange={(e) => setAgent({ ...agent, cli_profile: e.target.value || null })} placeholder="CLI profile" />
      <label><input type="checkbox" checked={agent.enabled} onChange={(e) => setAgent({ ...agent, enabled: e.target.checked })} /> Enabled</label>
      <h2 className="section-title">System Prompt</h2>
      <textarea value={agent.role_prompt} onChange={(e) => setAgent({ ...agent, role_prompt: e.target.value })} />
      <h2 className="section-title">Skills</h2>
      <SkillCheckboxes
        skills={skills}
        selectedSkillIds={(agent.skills || []).map((skill) => skill.id)}
        recommendedSkillNames={recommendedSkillNames}
        onToggle={toggleSkill}
      />
      {agent.definition_hash && <p className="empty-state">Definition hash: {agent.definition_hash}</p>}
      <div className="toolbar">
        <button onClick={save} disabled={busy}><Save size={16} /> Save</button>
        {agent.id && <button onClick={async () => setAgent(await setAgentEnabled(agent.id, !agent.enabled))}>{agent.enabled ? "Disable" : "Enable"}</button>}
      </div>
    </div>
    <Error text={error} />
  </Panel>;
}

function SkillSourcesPage() {
  const [sources, setSources] = useState<SkillSource[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [selectedSkill, setSelectedSkill] = useState<Skill | null>(null);
  const [tree, setTree] = useState<SkillTreeEntry | null>(null);
  const [content, setContent] = useState<{ path: string; content: string } | null>(null);
  const [sha, setSha] = useState<Record<string, string>>({});
  const [form, setForm] = useState({
    name: "",
    upstream_url: "",
    pinned_sha: "main",
    kind: "custom",
  });
  const [error, setError] = useState("");
  async function refresh() {
    const [nextSources, nextSkills] = await Promise.all([listSkillSources(), listInstalledSkills()]);
    setSources(nextSources);
    setSkills(nextSkills);
  }
  useEffect(() => { refresh().catch((e) => setError(e.message)); }, []);
  const sourceNames = useMemo(() => Object.fromEntries(sources.map((source) => [source.id, source.name])), [sources]);

  async function openSkill(skill: Skill, path = "SKILL.md") {
    setSelectedSkill(skill);
    setError("");
    try {
      const [nextTree, nextContent] = await Promise.all([getSkillTree(skill.id), getSkillContent(skill.id, path)]);
      setTree(nextTree.root);
      setContent({ path: nextContent.path, content: nextContent.content });
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function openFile(path: string) {
    if (!selectedSkill) return;
    try {
      const nextContent = await getSkillContent(selectedSkill.id, path);
      setContent({ path: nextContent.path, content: nextContent.content });
    } catch (e) {
      setError((e as Error).message);
    }
  }

  return <Panel title="Skill Sources">
    <Error text={error} />
    <form className="editor" onSubmit={async (e) => {
      e.preventDefault();
      setError("");
      try {
        await createSkillSource(form);
        setForm({ name: "", upstream_url: "", pinned_sha: "main", kind: "custom" });
        await refresh();
      } catch (err) {
        setError((err as Error).message);
      }
    }}>
      <h2 className="section-title">Add Source</h2>
      <div className="form-grid">
        <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Name" required />
        <input value={form.upstream_url} onChange={(e) => setForm({ ...form, upstream_url: e.target.value })} placeholder="Git URL" required />
        <input value={form.pinned_sha} onChange={(e) => setForm({ ...form, pinned_sha: e.target.value })} placeholder="Pinned SHA or branch" required />
        <select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
          <option value="custom">custom</option>
          <option value="ace3">ace3</option>
          <option value="verzth">verzth</option>
        </select>
      </div>
      <button><Plus size={16} /> Add source</button>
    </form>
    <div className="list">{sources.map((source) => <article key={source.id}>
      <h3>{source.name}</h3>
      <p>{source.upstream_url}</p>
      <span>{source.kind} · pinned {source.pinned_sha} · {source.last_synced_at ? "synced" : "never synced"}</span>
      <div className="toolbar">
        <button onClick={async () => { await syncSkillSource(source.id); await refresh(); }}><RefreshCw size={16} /> Sync</button>
        <input placeholder="New SHA" value={sha[source.id] || ""} onChange={(e) => setSha({ ...sha, [source.id]: e.target.value })} />
        <button onClick={async () => { await pinSkillSource(source.id, sha[source.id]); await refresh(); }}>Pin</button>
        <button onClick={async () => { await deleteSkillSource(source.id); await refresh(); }}><Trash2 size={16} /> Delete</button>
      </div>
    </article>)}</div>
    <h2 className="section-title">Installed Skills</h2>
    {skills.length === 0 ? <p className="empty-state">No installed skills yet. Sync a skill source to discover active skills.</p> :
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Skill</th>
              <th>Source</th>
              <th>Path</th>
              <th>Version</th>
            </tr>
          </thead>
          <tbody>
            {skills.map((skill) => <tr key={skill.id} className={selectedSkill?.id === skill.id ? "selected-row" : ""} onClick={() => openSkill(skill)}>
              <td>{skill.name}</td>
              <td>{sourceNames[skill.source_id] || skill.source_id}</td>
              <td>{skill.path_in_source}</td>
              <td>{skill.version || "n/a"}</td>
            </tr>)}
          </tbody>
        </table>
      </div>}
    <h2 className="section-title">Browse Skill Content</h2>
    {!selectedSkill ? <p className="empty-state">Select an installed skill to browse its synced files.</p> :
      <div className="browser-grid">
        <div className="tree-pane">
          <h3>{selectedSkill.name}</h3>
          {tree ? <SkillTree node={tree} onOpen={openFile} /> : <p className="empty-state">Loading tree...</p>}
        </div>
        <div className="preview-pane">
          <div className="artifact-head"><strong>{content?.path || "SKILL.md"}</strong><span>{sourceNames[selectedSkill.source_id] || selectedSkill.source_id}</span></div>
          <pre>{content?.content || "No preview available."}</pre>
        </div>
      </div>}
  </Panel>;
}

function SkillTree({ node, onOpen }: { node: SkillTreeEntry; onOpen: (path: string) => void }) {
  if (node.type === "file") {
    return <button type="button" className="tree-file" onClick={() => onOpen(node.path)}><FileText size={14} /> {node.name}</button>;
  }
  return <div className="tree-dir">
    {node.path && <span>{node.name}</span>}
    <div>
      {(node.children || []).map((child) => <SkillTree key={`${child.type}:${child.path}`} node={child} onOpen={onOpen} />)}
    </div>
  </div>;
}

function MapPage() {
  const [map, setMap] = useState<OrchestratorMap | null>(null);
  const [error, setError] = useState("");
  useEffect(() => { getOrchestratorMap().then(setMap).catch((e) => setError(e.message)); }, []);
  if (!map) return <Panel title="Orchestrator Map"><Error text={error || "Loading..."} /></Panel>;

  const skillsBySource = map.sources.map((source) => ({
    source,
    skills: map.skills.filter((skill) => skill.source_id === source.id),
  }));

  return <Panel title="Orchestrator Map">
    <Error text={error} />
    <div className="map-grid">
      <article>
        <h2 className="section-title">Sources - Skills - Recommendations</h2>
        <div className="map-tree">
          {skillsBySource.map(({ source, skills }) => <div className="map-node" key={source.id}>
            <strong>{source.name}</strong>
            <span>{source.kind} · pinned {source.pinned_sha}</span>
            <div className="map-children">
              {skills.map((skill) => <div className="map-node skill" key={skill.id}>
                <strong>{skill.name}</strong>
                <span>{skill.path_in_source}</span>
                {(skill.recommended_agents || []).length > 0 && <div className="tag-row">{skill.recommended_agents.map((agent) => <span key={agent}>{agent}</span>)}</div>}
                {(skill.trigger_tags || []).length > 0 && <p className="empty-state">Tags: {skill.trigger_tags.join(", ")}</p>}
              </div>)}
            </div>
          </div>)}
        </div>
      </article>
      <article>
        <h2 className="section-title">Agents - Prompt - Skills</h2>
        <div className="map-tree">
          {map.agents.map((agent) => <div className="map-node" key={agent.id}>
            <strong>{agent.name}</strong>
            <span>{agent.role} · {agent.cli_kind}</span>
            <p>{agent.base_prompt}</p>
            <div className="tag-row">{agent.assigned_skills.map((skill) => <span key={skill}>{skill}</span>)}</div>
            {agent.recommended_skills.length > 0 && <p className="empty-state">Dynamic: {agent.recommended_skills.join(", ")}</p>}
          </div>)}
        </div>
      </article>
    </div>
    <h2 className="section-title">Lifecycle Routing</h2>
    <div className="list">
      {map.lifecycles.map((lifecycle) => <article key={lifecycle.id}>
        <h3>{lifecycle.id}</h3>
        <p>{lifecycle.description}</p>
        <div className="map-flow">
          {lifecycle.steps.map((step, index) => <React.Fragment key={`${lifecycle.id}:${step.agent}:${index}`}>
            <span>{step.agent}{step.skip_when.length ? ` · skip: ${step.skip_when.join(", ")}` : ""}</span>
            {index < lifecycle.steps.length - 1 && <em>-&gt;</em>}
          </React.Fragment>)}
        </div>
      </article>)}
    </div>
  </Panel>;
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return <div className="panel"><h1>{title}</h1>{children}</div>;
}

function Error({ text }: { text: string }) {
  return text ? <p className="error">{text}</p> : null;
}

createRoot(document.getElementById("root")!).render(<App />);
