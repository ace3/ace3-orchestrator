import React, { useEffect, useMemo, useRef, useState } from "react";
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
        {route === "project" && projectId && <ProjectPage id={projectId} onOpenBoard={() => navigate("board", projectId)} onOpenProjects={() => navigate("projects")} onDeleted={() => navigate("projects")} />}
        {route === "board" && projectId && <BoardPage id={projectId} onOpenProjects={() => navigate("projects")} onOpenProject={() => navigate("project", projectId)} />}
        {route === "agents" && <AgentsPage openAgent={(id) => navigate("agent", id)} openAddAgent={() => navigate("agent-new")} />}
        {route === "agent-new" && <AgentCreatePage onCreated={(id) => navigate("agent", id)} onCancel={() => navigate("agents")} onOpenAgents={() => navigate("agents")} />}
        {route === "agent" && agentId && <AgentDetailPage id={agentId} onSaved={(id) => navigate("agent", id)} onOpenAgents={() => navigate("agents")} />}
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

function ProjectCreateModal({ open, onClose, onCreated }: {
  open: boolean;
  onClose: () => void;
  onCreated: (project: Project) => void;
}) {
  const [values, setValues] = useState<Pick<Project, "name" | "description" | "default_cli_kind" | "default_branch_strategy">>({ name: "", description: "", default_cli_kind: "codex", default_branch_strategy: "worktree-per-run" });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!open) return;
    setValues({ name: "", description: "", default_cli_kind: "codex", default_branch_strategy: "worktree-per-run" });
    setBusy(false);
    setErr("");
  }, [open]);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!values.name.trim()) return;
    setBusy(true);
    setErr("");
    try {
      const created = await createProject(values);
      onCreated(created);
      onClose();
    } catch (e2) { setErr((e2 as Error).message); setBusy(false); }
  }
  return (
    <Modal open={open} onClose={busy ? () => undefined : onClose} title="New project" footer={
      <>
        <button type="button" onClick={onClose} disabled={busy}>Cancel</button>
        <button type="submit" form="project-modal-form" disabled={busy || !values.name.trim()}><Plus size={14} /> Create project</button>
      </>
    }>
      <form id="project-modal-form" className="task-modal-form" onSubmit={submit}>
        {err && <Error text={err} />}
        <label className="field">
          <span className="field-label">Name</span>
          <input autoFocus value={values.name} onChange={(e) => setValues({ ...values, name: e.target.value })} placeholder="my-project" required />
        </label>
        <label className="field">
          <span className="field-label">Description</span>
          <textarea value={values.description} onChange={(e) => setValues({ ...values, description: e.target.value })} placeholder="Optional" style={{ minHeight: 80 }} />
        </label>
        <div className="task-modal-grid">
          <label className="field">
            <span className="field-label">Default CLI</span>
            <select value={values.default_cli_kind} onChange={(e) => setValues({ ...values, default_cli_kind: e.target.value as "claude" | "codex" })}>
              <option value="codex">codex</option>
              <option value="claude">claude</option>
            </select>
          </label>
          <label className="field">
            <span className="field-label">Branch strategy</span>
            <select value={values.default_branch_strategy} onChange={(e) => setValues({ ...values, default_branch_strategy: e.target.value as "worktree-per-run" | "shared" })}>
              <option value="worktree-per-run">worktree-per-run</option>
              <option value="shared">shared</option>
            </select>
          </label>
        </div>
      </form>
    </Modal>
  );
}

function ProjectsPage({ openProject, openBoard }: { openProject: (id: string) => void; openBoard: (id: string) => void }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [error, setError] = useState("");
  const [addOpen, setAddOpen] = useState(false);
  useEffect(() => { listProjects().then(setProjects).catch((e) => setError(e.message)); }, []);

  return <Panel
    title="Projects"
    actions={<button type="button" className="primary-button" onClick={() => setAddOpen(true)}><Plus size={16} /> New project</button>}
  >
    <Error text={error} />
    {projects.length === 0 ? (
      <div className="empty-state source-empty">
        <p><strong>No projects yet.</strong></p>
        <p>A project groups repos, tasks, and agents under one board.</p>
        <button type="button" className="primary-button" onClick={() => setAddOpen(true)}><Plus size={16} /> Create your first project</button>
      </div>
    ) : (
      <div className="list">
        {projects.map((project) => (
          <article
            key={project.id}
            className="clickable project-row"
            onClick={() => openBoard(project.id)}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => { if (e.key === "Enter") openBoard(project.id); }}
          >
            <div className="project-row-main">
              <h3>{project.name}</h3>
              <p>{project.description || "No description"}</p>
              <span>{project.default_cli_kind} · {project.repos?.length || 0} repo{(project.repos?.length || 0) === 1 ? "" : "s"}</span>
            </div>
            <div className="project-row-actions" onClick={(e) => e.stopPropagation()}>
              <button type="button" onClick={() => openProject(project.id)} title="Edit project details"><FolderGit2 size={14} /> Edit</button>
            </div>
          </article>
        ))}
      </div>
    )}
    <ProjectCreateModal
      open={addOpen}
      onClose={() => setAddOpen(false)}
      onCreated={(p) => setProjects((prev) => [p, ...prev])}
    />
  </Panel>;
}

function ProjectPage({ id, onOpenBoard, onOpenProjects, onDeleted }: { id: string; onOpenBoard: () => void; onOpenProjects: () => void; onDeleted: () => void }) {
  const [project, setProject] = useState<Project | null>(null);
  const [repoPath, setRepoPath] = useState("");
  const [deleteConfirmName, setDeleteConfirmName] = useState("");
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    getProject(id).then(setProject).catch((e) => setError(e.message));
  }, [id]);
  if (!project) return <Panel title="Project" breadcrumb={[{ label: "Projects", onClick: onOpenProjects }]}><Error text={error || "Loading..."} /></Panel>;

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

  return <Panel
    title={project.name}
    breadcrumb={[{ label: "Projects", onClick: onOpenProjects }]}
    actions={<button type="button" className="primary-button" onClick={onOpenBoard}><LayoutDashboard size={16} /> Open board</button>}
  >
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

function Modal({ open, onClose, title, children, footer }: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
  footer?: React.ReactNode;
}) {
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) { if (e.key === "Escape") onClose(); }
    document.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
        <div className="modal-header">
          <h3>{title}</h3>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Close">×</button>
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>
  );
}

function Menu({ open, onClose, children, align = "right" }: {
  open: boolean;
  onClose: () => void;
  children: React.ReactNode;
  align?: "right" | "left";
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    function onDoc(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    }
    function onKey(e: KeyboardEvent) { if (e.key === "Escape") onClose(); }
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, onClose]);
  if (!open) return null;
  return <div ref={ref} className={`menu menu-${align}`} role="menu">{children}</div>;
}

type TaskFormValues = {
  title: string;
  description: string;
  assignee_agent_id: string;
  priority: number;
  tags: string;
  lifecycle_id: string;
  status: Task["status"];
};

function TaskModal({ open, mode, initial, defaultStatus, agents, onClose, onSubmit }: {
  open: boolean;
  mode: "create" | "edit";
  initial?: Partial<Task>;
  defaultStatus?: Task["status"];
  agents: Agent[];
  onClose: () => void;
  onSubmit: (values: TaskFormValues) => Promise<void>;
}) {
  const [values, setValues] = useState<TaskFormValues>({
    title: "", description: "", assignee_agent_id: "pm", priority: 0, tags: "", lifecycle_id: "default", status: "todo",
  });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!open) return;
    setErr("");
    setBusy(false);
    setValues({
      title: initial?.title || "",
      description: initial?.description || "",
      assignee_agent_id: initial?.assignee_agent_id || (agents[0]?.id ?? "pm"),
      priority: initial?.priority ?? 0,
      tags: (initial?.tags || []).join(", "),
      lifecycle_id: initial?.lifecycle_id || "default",
      status: (initial?.status as Task["status"]) || defaultStatus || "todo",
    });
  }, [open, initial, defaultStatus, agents]);

  const title = mode === "edit"
    ? "Edit task"
    : defaultStatus
      ? `Add task — ${STATUS_LABELS[defaultStatus]}`
      : "Add task";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!values.title.trim()) return;
    setBusy(true);
    setErr("");
    try {
      await onSubmit(values);
      onClose();
    } catch (e2) {
      setErr((e2 as Error).message);
      setBusy(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={busy ? () => undefined : onClose}
      title={title}
      footer={
        <>
          <button type="button" onClick={onClose} disabled={busy}>Cancel</button>
          <button type="submit" form="task-modal-form" disabled={busy || !values.title.trim()}>
            <Plus size={14} /> {mode === "edit" ? "Save changes" : "Create task"}
          </button>
        </>
      }
    >
      <form id="task-modal-form" className="task-modal-form" onSubmit={submit}>
        {err && <Error text={err} />}
        <label className="field">
          <span className="field-label">Title</span>
          <input autoFocus placeholder="What needs to be done?" value={values.title} onChange={(e) => setValues({ ...values, title: e.target.value })} required />
        </label>
        <label className="field">
          <span className="field-label">Description</span>
          <textarea placeholder="Add more detail (optional)" value={values.description} onChange={(e) => setValues({ ...values, description: e.target.value })} style={{ minHeight: 92 }} />
        </label>
        <div className="task-modal-grid">
          {mode === "edit" && (
            <label className="field">
              <span className="field-label">Status</span>
              <select value={values.status} onChange={(e) => setValues({ ...values, status: e.target.value as Task["status"] })}>
                {TASK_STATUSES.map((s) => <option key={s} value={s}>{STATUS_LABELS[s]}</option>)}
              </select>
            </label>
          )}
          <label className="field">
            <span className="field-label">Assignee</span>
            <select value={values.assignee_agent_id} onChange={(e) => setValues({ ...values, assignee_agent_id: e.target.value })}>
              {agents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
          </label>
          <label className="field">
            <span className="field-label">Lifecycle</span>
            <select value={values.lifecycle_id} onChange={(e) => setValues({ ...values, lifecycle_id: e.target.value })}>
              <option value="default">Default</option>
              <option value="backend-only">Backend only</option>
              <option value="frontend-only">Frontend only</option>
            </select>
          </label>
          <label className="field">
            <span className="field-label">Priority</span>
            <input type="number" min={0} max={10} value={values.priority} onChange={(e) => setValues({ ...values, priority: Number(e.target.value) || 0 })} />
          </label>
        </div>
        <label className="field">
          <span className="field-label">Tags</span>
          <input placeholder="comma-separated" value={values.tags} onChange={(e) => setValues({ ...values, tags: e.target.value })} />
        </label>
      </form>
    </Modal>
  );
}

function CardMenu({ task, onEdit, onDuplicate, onMove, onDelete }: {
  task: Task;
  onEdit: () => void;
  onDuplicate: () => void;
  onMove: (status: Task["status"]) => void;
  onDelete: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [moveOpen, setMoveOpen] = useState(false);
  const [confirmDel, setConfirmDel] = useState(false);
  function closeAll() { setOpen(false); setMoveOpen(false); setConfirmDel(false); }
  return (
    <div className="menu-anchor" onClick={(e) => e.stopPropagation()}>
      <button type="button" className="task-kebab" aria-label="Task actions" onClick={() => { setOpen((v) => !v); setMoveOpen(false); setConfirmDel(false); }}>
        <MoreHorizontal size={14} />
      </button>
      <Menu open={open && !confirmDel} onClose={closeAll}>
        <button type="button" onClick={() => { closeAll(); onEdit(); }}>Edit task</button>
        <button type="button" onClick={() => { closeAll(); onDuplicate(); }}>Duplicate</button>
        <button type="button" className="menu-sub-trigger" onClick={() => setMoveOpen((v) => !v)} aria-expanded={moveOpen}>
          Move to <span aria-hidden="true">›</span>
        </button>
        {moveOpen && (
          <div className="menu menu-sub-list">
            {TASK_STATUSES.filter((s) => s !== task.status).map((s) => (
              <button key={s} type="button" onClick={() => { closeAll(); onMove(s); }}>{STATUS_LABELS[s]}</button>
            ))}
          </div>
        )}
        <div className="sep" />
        <button type="button" className="danger" onClick={() => setConfirmDel(true)}>Delete</button>
      </Menu>
      {confirmDel && (
        <div className="menu menu-confirm" role="dialog" aria-label="Confirm delete">
          <p>Delete this task?</p>
          <div className="menu-confirm-actions">
            <button type="button" onClick={closeAll}>Cancel</button>
            <button type="button" className="danger-button" onClick={() => { closeAll(); onDelete(); }}>Delete</button>
          </div>
        </div>
      )}
    </div>
  );
}

function ColumnMenu({ collapsed, onAdd, onToggleCollapsed, onSort }: {
  collapsed: boolean;
  onAdd: () => void;
  onToggleCollapsed: () => void;
  onSort: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="menu-anchor">
      <button type="button" className="col-action" aria-label="Column actions" onClick={() => setOpen((v) => !v)}>
        <MoreHorizontal size={14} />
      </button>
      <Menu open={open} onClose={() => setOpen(false)}>
        <button type="button" onClick={() => { setOpen(false); onAdd(); }}>Add task</button>
        <button type="button" onClick={() => { setOpen(false); onToggleCollapsed(); }}>{collapsed ? "Expand column" : "Collapse column"}</button>
        <button type="button" onClick={() => { setOpen(false); onSort(); }}>Sort by priority</button>
      </Menu>
    </div>
  );
}

function BoardPage({ id, onOpenProjects, onOpenProject }: { id: string; onOpenProjects: () => void; onOpenProject: () => void }) {
  const [project, setProject] = useState<Project | null>(null);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>(() => readStatusFilter());
  const [assigneeFilter, setAssigneeFilter] = useState<AssigneeFilter>("all");
  const [error, setError] = useState("");
  const [modalOpen, setModalOpen] = useState(false);
  const [modalMode, setModalMode] = useState<"create" | "edit">("create");
  const [modalInitial, setModalInitial] = useState<Partial<Task> | undefined>(undefined);
  const [modalDefaultStatus, setModalDefaultStatus] = useState<Task["status"] | undefined>(undefined);

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

  if (!project) return <Panel title="Board" breadcrumb={[{ label: "Projects", onClick: onOpenProjects }]}><Error text={error || "Loading..."} /></Panel>;

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

  function openCreateModal(status?: Task["status"]) {
    setModalMode("create");
    setModalInitial(undefined);
    setModalDefaultStatus(status);
    setModalOpen(true);
  }
  function openEditModal(task: Task) {
    setModalMode("edit");
    setModalInitial(task);
    setModalDefaultStatus(undefined);
    setModalOpen(true);
  }
  async function submitModal(values: TaskFormValues) {
    const current = project;
    if (!current) return;
    const tags = values.tags.split(",").map((tag) => tag.trim()).filter(Boolean);
    if (modalMode === "edit" && modalInitial?.id) {
      const updated = await updateTask(modalInitial.id, {
        ...modalInitial,
        title: values.title,
        description: values.description,
        assignee_agent_id: values.assignee_agent_id || null,
        priority: values.priority,
        lifecycle_id: values.lifecycle_id,
        tags,
        status: values.status,
      });
      setTasks((prev) => prev.map((item) => item.id === updated.id ? updated : item));
      if (selectedTask?.id === updated.id) setSelectedTask(updated);
    } else {
      const task = await createTask(current.id, {
        title: values.title,
        description: values.description,
        assignee_agent_id: values.assignee_agent_id || null,
        priority: values.priority,
        lifecycle_id: values.lifecycle_id,
        tags,
        repo_id: current.repos?.[0]?.id || null,
        status: values.status,
      });
      setTasks((prev) => [task, ...prev]);
    }
  }
  async function duplicateTask(task: Task) {
    const current = project;
    if (!current) return;
    try {
      const copy = await createTask(current.id, {
        title: `${task.title} (copy)`,
        description: task.description,
        assignee_agent_id: task.assignee_agent_id,
        priority: task.priority,
        lifecycle_id: task.lifecycle_id,
        tags: task.tags,
        repo_id: task.repo_id ?? (current.repos?.[0]?.id || null),
        status: "todo",
      });
      setTasks((prev) => [copy, ...prev]);
    } catch (e) { setError((e as Error).message); }
  }
  async function deleteTask(task: Task) {
    try {
      const updated = await updateTask(task.id, { ...task, status: "cancelled" });
      setTasks((prev) => prev.map((item) => item.id === updated.id ? updated : item));
      if (selectedTask?.id === updated.id) setSelectedTask(updated);
    } catch (e) { setError((e as Error).message); }
  }
  async function moveTask(task: Task, status: Task["status"]) {
    try {
      const updated = await updateTask(task.id, { ...task, status });
      setTasks((prev) => prev.map((item) => item.id === updated.id ? updated : item));
      if (selectedTask?.id === updated.id) setSelectedTask(updated);
    } catch (e) { setError((e as Error).message); }
  }

  return <Panel
    title="Board"
    breadcrumb={[
      { label: "Projects", onClick: onOpenProjects },
      { label: project.name, onClick: onOpenProject },
    ]}
  >
    <Error text={error} />
    <div className="board-toolbar">
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
      <div className="board-toolbar-actions">
        <button type="button" onClick={onOpenProject} title="Edit project details"><FolderGit2 size={16} /> Edit project</button>
        <button type="button" onClick={async () => { await heartbeat(); setTasks(await listTasks(project.id)); }}><RefreshCw size={16} /> Heartbeat</button>
        <button type="button" className="primary-button" onClick={() => openCreateModal("todo")}><Plus size={16} /> Add task</button>
      </div>
    </div>
    {filtersActive && filteredTasks.length === 0 && <p className="filtered-empty">No tasks match the selected filters.</p>}
    <Kanban
      tasks={filteredTasks}
      agents={agents}
      onOpen={setSelectedTask}
      onMove={moveTask}
      onAddInColumn={(status) => openCreateModal(status)}
      onEditTask={openEditModal}
      onDuplicateTask={duplicateTask}
      onDeleteTask={deleteTask}
    />
    <TaskModal
      open={modalOpen}
      mode={modalMode}
      initial={modalInitial}
      defaultStatus={modalDefaultStatus}
      agents={agents}
      onClose={() => setModalOpen(false)}
      onSubmit={submitModal}
    />
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

function Kanban({ tasks, agents, onOpen, onMove, onAddInColumn, onEditTask, onDuplicateTask, onDeleteTask }: {
  tasks: Task[];
  agents: Agent[];
  onOpen: (task: Task) => void;
  onMove: (task: Task, status: Task["status"]) => void;
  onAddInColumn: (status: Task["status"]) => void;
  onEditTask: (task: Task) => void;
  onDuplicateTask: (task: Task) => void;
  onDeleteTask: (task: Task) => void;
}) {
  const agentName = (id: string | null) => agents.find((agent) => agent.id === id)?.name || id || "Unassigned";
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [sortByPriority, setSortByPriority] = useState<Record<string, boolean>>({});
  return (
    <div className="kanban">
      {TASK_STATUSES.map((status) => {
        const isCollapsed = !!collapsed[status];
        const items = tasks.filter((task) => task.status === status);
        const ordered = sortByPriority[status]
          ? [...items].sort((a, b) => (b.priority || 0) - (a.priority || 0))
          : items;
        return (
          <section
            className={`column${isCollapsed ? " collapsed" : ""}`}
            key={status}
            onClick={isCollapsed ? () => setCollapsed((prev) => ({ ...prev, [status]: false })) : undefined}
            title={isCollapsed ? "Expand column" : undefined}
          >
            <header className="column-header">
              <span className={`col-icon status-${status}`} />
              <span className="col-name">{STATUS_LABELS[status]}</span>
              <span className="col-count">{items.length}</span>
              {!isCollapsed && <>
                <span className="spacer" />
                <ColumnMenu
                  collapsed={isCollapsed}
                  onAdd={() => onAddInColumn(status)}
                  onToggleCollapsed={() => setCollapsed((prev) => ({ ...prev, [status]: !prev[status] }))}
                  onSort={() => setSortByPriority((prev) => ({ ...prev, [status]: !prev[status] }))}
                />
                <button type="button" className="col-action" aria-label="Add task to column" onClick={() => onAddInColumn(status)}>
                  <Plus size={14} />
                </button>
              </>}
            </header>
            {!isCollapsed && <div className="column-body">
              {ordered.length === 0 ? (
                <div className="column-empty">No tasks</div>
              ) : (
                ordered.map((task) => {
                  const aName = agentName(task.assignee_agent_id);
                  return (
                    <article className="task-card" key={task.id} onClick={() => onOpen(task)}>
                      <div className="task-card-top">
                        <span className="task-id">TASK-{shortId(task.id)}</span>
                        <span className={`task-priority ${priorityClass(task.priority)}`}>P{task.priority}</span>
                        <CardMenu
                          task={task}
                          onEdit={() => onEditTask(task)}
                          onDuplicate={() => onDuplicateTask(task)}
                          onMove={(s) => onMove(task, s)}
                          onDelete={() => onDeleteTask(task)}
                        />
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
            </div>}
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

function AgentCreatePage({ onCreated, onCancel, onOpenAgents }: { onCreated: (id: string) => void; onCancel: () => void; onOpenAgents: () => void }) {
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

  return <Panel title="Add Agent" breadcrumb={[{ label: "Agents", onClick: onOpenAgents }]}>
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

function AgentDetailPage({ id, onSaved, onOpenAgents }: { id: string; onSaved: (id: string) => void; onOpenAgents: () => void }) {
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

  if (!agent) return <Panel title="Agent" breadcrumb={[{ label: "Agents", onClick: onOpenAgents }]}><Error text={error || "Loading..."} /></Panel>;
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

  return <Panel title={agent.name} breadcrumb={[{ label: "Agents", onClick: onOpenAgents }]}>
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

function relativeTime(iso: string | null): string {
  if (!iso) return "never";
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "never";
  const diff = Date.now() - then;
  if (diff < 0) return "just now";
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  if (d < 30) return `${d}d ago`;
  const mo = Math.floor(d / 30);
  if (mo < 12) return `${mo}mo ago`;
  return `${Math.floor(mo / 12)}y ago`;
}

function SourceFormModal({ open, onClose, onSubmit }: {
  open: boolean;
  onClose: () => void;
  onSubmit: (values: { name: string; upstream_url: string; pinned_sha: string; kind: string }) => Promise<void>;
}) {
  const [values, setValues] = useState({ name: "", upstream_url: "", pinned_sha: "main", kind: "custom" });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!open) return;
    setValues({ name: "", upstream_url: "", pinned_sha: "main", kind: "custom" });
    setBusy(false);
    setErr("");
  }, [open]);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!values.name.trim() || !values.upstream_url.trim()) return;
    setBusy(true);
    setErr("");
    try { await onSubmit(values); onClose(); }
    catch (e2) { setErr((e2 as Error).message); setBusy(false); }
  }
  return (
    <Modal open={open} onClose={busy ? () => undefined : onClose} title="Add skill source" footer={
      <>
        <button type="button" onClick={onClose} disabled={busy}>Cancel</button>
        <button type="submit" form="source-modal-form" disabled={busy || !values.name.trim() || !values.upstream_url.trim()}>
          <Plus size={14} /> Add source
        </button>
      </>
    }>
      <form id="source-modal-form" className="task-modal-form" onSubmit={submit}>
        {err && <Error text={err} />}
        <label className="field">
          <span className="field-label">Name</span>
          <input autoFocus value={values.name} onChange={(e) => setValues({ ...values, name: e.target.value })} placeholder="my-skills" required />
        </label>
        <label className="field">
          <span className="field-label">Git URL</span>
          <input value={values.upstream_url} onChange={(e) => setValues({ ...values, upstream_url: e.target.value })} placeholder="git@github.com:org/skills.git" required />
        </label>
        <div className="task-modal-grid">
          <label className="field">
            <span className="field-label">Pinned SHA or branch</span>
            <input value={values.pinned_sha} onChange={(e) => setValues({ ...values, pinned_sha: e.target.value })} required />
          </label>
          <label className="field">
            <span className="field-label">Kind</span>
            <select value={values.kind} onChange={(e) => setValues({ ...values, kind: e.target.value })}>
              <option value="custom">custom</option>
              <option value="ace3">ace3</option>
              <option value="verzth">verzth</option>
            </select>
          </label>
        </div>
      </form>
    </Modal>
  );
}

function PinModal({ open, source, onClose, onSubmit }: {
  open: boolean;
  source: SkillSource | null;
  onClose: () => void;
  onSubmit: (sha: string) => Promise<void>;
}) {
  const [sha, setSha] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!open || !source) return;
    setSha(source.pinned_sha);
    setBusy(false);
    setErr("");
  }, [open, source]);
  if (!source) return null;
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!sha.trim() || sha === source!.pinned_sha) return;
    setBusy(true);
    setErr("");
    try { await onSubmit(sha.trim()); onClose(); }
    catch (e2) { setErr((e2 as Error).message); setBusy(false); }
  }
  return (
    <Modal open={open} onClose={busy ? () => undefined : onClose} title={`Pin ${source.name}`} footer={
      <>
        <button type="button" onClick={onClose} disabled={busy}>Cancel</button>
        <button type="submit" form="pin-modal-form" disabled={busy || !sha.trim() || sha === source.pinned_sha}>
          Pin & re-sync
        </button>
      </>
    }>
      <form id="pin-modal-form" className="task-modal-form" onSubmit={submit}>
        {err && <Error text={err} />}
        <p className="modal-warning">
          Changing the pinned SHA re-syncs all skills from this source. Existing skills may be added, removed, or updated.
        </p>
        <label className="field">
          <span className="field-label">Current pin</span>
          <input value={source.pinned_sha} disabled />
        </label>
        <label className="field">
          <span className="field-label">New SHA or branch</span>
          <input autoFocus value={sha} onChange={(e) => setSha(e.target.value)} placeholder="main, v1.2.3, or a commit SHA" required />
        </label>
      </form>
    </Modal>
  );
}

function SourceMenu({ source, onSync, onPin, onDelete }: {
  source: SkillSource;
  onSync: () => void;
  onPin: () => void;
  onDelete: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [confirmDel, setConfirmDel] = useState(false);
  function closeAll() { setOpen(false); setConfirmDel(false); }
  return (
    <div className="menu-anchor">
      <button type="button" className="col-action" aria-label="Source actions" onClick={() => { setOpen((v) => !v); setConfirmDel(false); }}>
        <MoreHorizontal size={14} />
      </button>
      <Menu open={open && !confirmDel} onClose={closeAll}>
        <button type="button" onClick={() => { closeAll(); onSync(); }}>Sync now</button>
        <button type="button" onClick={() => { closeAll(); onPin(); }}>Pin SHA…</button>
        <button type="button" onClick={() => { closeAll(); window.open(source.upstream_url, "_blank", "noopener"); }}>Open repo URL</button>
        <div className="sep" />
        <button type="button" className="danger" onClick={() => setConfirmDel(true)}>Delete source</button>
      </Menu>
      {confirmDel && (
        <div className="menu menu-confirm" role="dialog" aria-label="Confirm delete">
          <p>Delete <strong>{source.name}</strong> and all its installed skills?</p>
          <div className="menu-confirm-actions">
            <button type="button" onClick={closeAll}>Cancel</button>
            <button type="button" className="danger-button" onClick={() => { closeAll(); onDelete(); }}>Delete</button>
          </div>
        </div>
      )}
    </div>
  );
}

type SkillSort = "name" | "source" | "agents" | "stale";

function SkillSourcesPage() {
  const [sources, setSources] = useState<SkillSource[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [agentList, setAgentList] = useState<Agent[]>([]);
  const [map, setMap] = useState<OrchestratorMap | null>(null);
  const [selectedSkill, setSelectedSkill] = useState<Skill | null>(null);
  const [tree, setTree] = useState<SkillTreeEntry | null>(null);
  const [content, setContent] = useState<{ path: string; content: string } | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<Record<string, string>>({});
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SkillSort>("source");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [addOpen, setAddOpen] = useState(false);
  const [pinTarget, setPinTarget] = useState<SkillSource | null>(null);

  async function refresh() {
    const [nextSources, nextSkills, nextAgents, nextMap] = await Promise.all([
      listSkillSources(),
      listInstalledSkills(),
      listAgents().catch(() => [] as Agent[]),
      getOrchestratorMap().catch(() => null as OrchestratorMap | null),
    ]);
    setSources(nextSources);
    setSkills(nextSkills);
    setAgentList(nextAgents);
    setMap(nextMap);
  }
  useEffect(() => { refresh().catch((e) => setError(e.message)); }, []);

  const sourceNames = useMemo(() => Object.fromEntries(sources.map((s) => [s.id, s.name])), [sources]);

  const usedByMap = useMemo(() => {
    const m = new Map<string, Agent[]>();
    for (const agent of agentList) {
      for (const skill of agent.skills || []) {
        const arr = m.get(skill.id) || [];
        arr.push(agent);
        m.set(skill.id, arr);
      }
    }
    return m;
  }, [agentList]);

  const recommendedByMap = useMemo(() => {
    const m = new Map<string, string[]>();
    if (!map) return m;
    for (const skill of map.skills) {
      if (skill.recommended_agents && skill.recommended_agents.length > 0) {
        m.set(skill.id, skill.recommended_agents);
      }
    }
    return m;
  }, [map]);

  const filteredSkills = useMemo(() => {
    const q = search.trim().toLowerCase();
    let result = skills.slice();
    if (q) {
      result = result.filter((s) =>
        s.name.toLowerCase().includes(q) ||
        s.path_in_source.toLowerCase().includes(q) ||
        (sourceNames[s.source_id] || "").toLowerCase().includes(q)
      );
    }
    if (sort === "name") {
      result.sort((a, b) => a.name.localeCompare(b.name));
    } else if (sort === "agents") {
      result.sort((a, b) => (usedByMap.get(b.id)?.length || 0) - (usedByMap.get(a.id)?.length || 0));
    } else if (sort === "stale") {
      const stamp = (s: Skill) => {
        const src = sources.find((x) => x.id === s.source_id);
        return src?.last_synced_at ? new Date(src.last_synced_at).getTime() : 0;
      };
      result.sort((a, b) => stamp(a) - stamp(b));
    } else {
      result.sort((a, b) => (sourceNames[a.source_id] || "").localeCompare(sourceNames[b.source_id] || "") || a.name.localeCompare(b.name));
    }
    return result;
  }, [skills, search, sort, sourceNames, usedByMap, sources]);

  const groupedSkills = useMemo(() => {
    if (sort !== "source") return null;
    const groups: { source: SkillSource | null; skills: Skill[] }[] = [];
    for (const source of sources) {
      const items = filteredSkills.filter((s) => s.source_id === source.id);
      if (items.length === 0 && search.trim()) continue;
      groups.push({ source, skills: items });
    }
    const orphans = filteredSkills.filter((s) => !sources.some((src) => src.id === s.source_id));
    if (orphans.length > 0) groups.push({ source: null, skills: orphans });
    return groups;
  }, [filteredSkills, sources, sort, search]);

  async function openSkill(skill: Skill, path = "SKILL.md") {
    setSelectedSkill(skill);
    setError("");
    setTree(null);
    setContent(null);
    try {
      const [nextTree, nextContent] = await Promise.all([getSkillTree(skill.id), getSkillContent(skill.id, path)]);
      setTree(nextTree.root);
      setContent({ path: nextContent.path, content: nextContent.content });
    } catch (e) { setError((e as Error).message); }
  }
  async function openFile(path: string) {
    if (!selectedSkill) return;
    try {
      const nextContent = await getSkillContent(selectedSkill.id, path);
      setContent({ path: nextContent.path, content: nextContent.content });
    } catch (e) { setError((e as Error).message); }
  }

  async function handleAdd(values: { name: string; upstream_url: string; pinned_sha: string; kind: string }) {
    await createSkillSource(values);
    await refresh();
  }
  async function handleSync(source: SkillSource) {
    setError("");
    setBusy((prev) => ({ ...prev, [source.id]: "syncing" }));
    try { await syncSkillSource(source.id); await refresh(); }
    catch (e) { setError((e as Error).message); }
    finally { setBusy((prev) => { const { [source.id]: _, ...rest } = prev; return rest; }); }
  }
  async function handlePin(source: SkillSource, sha: string) {
    setBusy((prev) => ({ ...prev, [source.id]: "pinning" }));
    try { await pinSkillSource(source.id, sha); await refresh(); }
    finally { setBusy((prev) => { const { [source.id]: _, ...rest } = prev; return rest; }); }
  }
  async function handleDelete(source: SkillSource) {
    setBusy((prev) => ({ ...prev, [source.id]: "deleting" }));
    try {
      await deleteSkillSource(source.id);
      if (selectedSkill && selectedSkill.source_id === source.id) {
        setSelectedSkill(null); setTree(null); setContent(null);
      }
      await refresh();
    } catch (e) { setError((e as Error).message); }
    finally { setBusy((prev) => { const { [source.id]: _, ...rest } = prev; return rest; }); }
  }

  const totalSkills = skills.length;
  const totalAgents = agentList.length;
  const recommendedCount = recommendedByMap.size;

  return <Panel title="Skill Sources">
    <Error text={error} />

    <div className="board-toolbar">
      <div className="source-stats">
        <span><strong>{sources.length}</strong> source{sources.length === 1 ? "" : "s"}</span>
        <span><strong>{totalSkills}</strong> skill{totalSkills === 1 ? "" : "s"}</span>
        <span><strong>{totalAgents}</strong> agent{totalAgents === 1 ? "" : "s"}</span>
        {recommendedCount > 0 && <span><strong>{recommendedCount}</strong> recommended</span>}
      </div>
      <div className="board-toolbar-actions">
        <button type="button" className="primary-button" onClick={() => setAddOpen(true)}><Plus size={16} /> Add source</button>
      </div>
    </div>

    {sources.length === 0 ? (
      <div className="empty-state source-empty">
        <p><strong>No sources yet.</strong></p>
        <p>Add an upstream git repository to discover skills. Sources can be pinned to a SHA or tracked from a branch.</p>
        <button type="button" className="primary-button" onClick={() => setAddOpen(true)}><Plus size={16} /> Add your first source</button>
      </div>
    ) : (
      <div className="source-grid">
        {sources.map((source) => {
          const skillCount = skills.filter((s) => s.source_id === source.id).length;
          const status = busy[source.id];
          return (
            <article className="source-card" key={source.id}>
              <div className="source-card-head">
                <div className="source-card-title">
                  <strong>{source.name}</strong>
                  <span className="badge badge-kind">{source.kind}</span>
                  {source.has_update && <span className="badge badge-update" title="A newer commit is available on the upstream branch">Update available</span>}
                  {status && <span className="badge badge-busy">{status}…</span>}
                </div>
                <SourceMenu
                  source={source}
                  onSync={() => handleSync(source)}
                  onPin={() => setPinTarget(source)}
                  onDelete={() => handleDelete(source)}
                />
              </div>
              <p className="source-url">{source.upstream_url}</p>
              <div className="source-meta">
                <span>pinned <code>{source.pinned_sha}</code></span>
                <span>{skillCount} skill{skillCount === 1 ? "" : "s"}</span>
                <span title={source.last_synced_at || "never synced"}>synced {relativeTime(source.last_synced_at)}</span>
              </div>
            </article>
          );
        })}
      </div>
    )}

    <div className="skills-toolbar">
      <h2 className="section-title">Installed Skills</h2>
      <div className="skills-controls">
        <input
          className="skills-search"
          placeholder="Search skills, paths, sources…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select value={sort} onChange={(e) => setSort(e.target.value as SkillSort)} aria-label="Sort skills">
          <option value="source">Group by source</option>
          <option value="name">Name (A–Z)</option>
          <option value="agents">Most used by agents</option>
          <option value="stale">Stalest sync first</option>
        </select>
      </div>
    </div>

    <div className={`skills-layout ${selectedSkill ? "has-selection" : ""}`}>
    <div className="skills-list-pane">
    {totalSkills === 0 ? (
      <p className="empty-state">No installed skills yet. Sync a source to discover its skills.</p>
    ) : filteredSkills.length === 0 ? (
      <p className="empty-state">No skills match “{search}”.</p>
    ) : groupedSkills ? (
      <div className="skill-groups">
        {groupedSkills.map(({ source, skills: items }) => {
          const key = source?.id || "__orphans__";
          const isCollapsed = !!collapsed[key];
          return (
            <section className="skill-group" key={key}>
              <header
                className="skill-group-head"
                onClick={() => setCollapsed((prev) => ({ ...prev, [key]: !prev[key] }))}
                role="button"
                tabIndex={0}
              >
                <span className={`chevron ${isCollapsed ? "right" : "down"}`} aria-hidden="true">▾</span>
                <strong>{source?.name || "Orphaned skills (source removed)"}</strong>
                <span className="count-badge">{items.length}</span>
                {source && <span className="skill-group-meta">pinned <code>{source.pinned_sha}</code> · synced {relativeTime(source.last_synced_at)}</span>}
              </header>
              {!isCollapsed && (
                <div className="skill-rows">
                  {items.length === 0 ? (
                    <p className="empty-state">No skills in this source.</p>
                  ) : items.map((skill) => {
                    const users = usedByMap.get(skill.id) || [];
                    const recommended = recommendedByMap.get(skill.id) || [];
                    return (
                      <button
                        type="button"
                        key={skill.id}
                        className={`skill-row ${selectedSkill?.id === skill.id ? "active" : ""}`}
                        onClick={() => openSkill(skill)}
                      >
                        <div className="skill-row-main">
                          <span className="skill-row-name">{skill.name}</span>
                          <span className="skill-row-path">{skill.path_in_source}</span>
                        </div>
                        <div className="skill-row-meta">
                          {skill.version && <span className="badge badge-version">{skill.version}</span>}
                          {users.length > 0 && (
                            <span className="badge badge-agents" title={users.map((a) => a.name).join(", ")}>
                              {users.length} agent{users.length === 1 ? "" : "s"}
                            </span>
                          )}
                          {recommended.length > 0 && (
                            <span className="badge badge-recommended" title={`Recommended for: ${recommended.join(", ")}`}>
                              recommended × {recommended.length}
                            </span>
                          )}
                        </div>
                      </button>
                    );
                  })}
                </div>
              )}
            </section>
          );
        })}
      </div>
    ) : (
      <div className="skill-rows skill-rows-flat">
        {filteredSkills.map((skill) => {
          const users = usedByMap.get(skill.id) || [];
          const recommended = recommendedByMap.get(skill.id) || [];
          return (
            <button
              type="button"
              key={skill.id}
              className={`skill-row ${selectedSkill?.id === skill.id ? "active" : ""}`}
              onClick={() => openSkill(skill)}
            >
              <div className="skill-row-main">
                <span className="skill-row-name">{skill.name}</span>
                <span className="skill-row-path">{sourceNames[skill.source_id] || skill.source_id} · {skill.path_in_source}</span>
              </div>
              <div className="skill-row-meta">
                {skill.version && <span className="badge badge-version">{skill.version}</span>}
                {users.length > 0 && <span className="badge badge-agents">{users.length} agent{users.length === 1 ? "" : "s"}</span>}
                {recommended.length > 0 && <span className="badge badge-recommended">rec × {recommended.length}</span>}
              </div>
            </button>
          );
        })}
      </div>
    )}

    </div>
    {selectedSkill && (
      <aside className="skills-detail-pane">
        <header className="detail-pane-head">
          <div>
            <strong>{selectedSkill.name}</strong>
            <span className="detail-pane-sub">{sourceNames[selectedSkill.source_id] || selectedSkill.source_id}</span>
          </div>
          <button
            type="button"
            className="detail-pane-close"
            aria-label="Close skill viewer"
            onClick={() => { setSelectedSkill(null); setTree(null); setContent(null); }}
          >×</button>
        </header>
        <div className="detail-pane-tree">
          {tree ? <SkillTree node={tree} onOpen={openFile} /> : <p className="empty-state">Loading tree…</p>}
        </div>
        <div className="detail-pane-preview">
          <div className="artifact-head">
            <strong>{content?.path || "SKILL.md"}</strong>
          </div>
          <pre>{content?.content || "No preview available."}</pre>
        </div>
      </aside>
    )}
    </div>

    <SourceFormModal open={addOpen} onClose={() => setAddOpen(false)} onSubmit={handleAdd} />
    <PinModal
      open={!!pinTarget}
      source={pinTarget}
      onClose={() => setPinTarget(null)}
      onSubmit={async (sha) => { if (pinTarget) await handlePin(pinTarget, sha); }}
    />
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

type Crumb = { label: string; onClick?: () => void };

function Panel({ title, children, breadcrumb, actions }: {
  title: string;
  children: React.ReactNode;
  breadcrumb?: Crumb[];
  actions?: React.ReactNode;
}) {
  return (
    <div className="panel">
      {breadcrumb && breadcrumb.length > 0 && (
        <nav className="breadcrumb" aria-label="Breadcrumb">
          {breadcrumb.map((crumb, i) => (
            <React.Fragment key={i}>
              {crumb.onClick ? (
                <button type="button" className="crumb-link" onClick={crumb.onClick}>{crumb.label}</button>
              ) : (
                <span className="crumb-text">{crumb.label}</span>
              )}
              <span className="crumb-sep" aria-hidden="true">›</span>
            </React.Fragment>
          ))}
        </nav>
      )}
      <div className="panel-header">
        <h1>{title}</h1>
        {actions && <div className="panel-actions">{actions}</div>}
      </div>
      {children}
    </div>
  );
}

function Error({ text }: { text: string }) {
  return text ? <p className="error">{text}</p> : null;
}

createRoot(document.getElementById("root")!).render(<App />);
