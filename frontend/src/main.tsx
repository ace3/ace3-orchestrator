import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { Bot, Boxes, Check, Copy, FolderGit2, LayoutDashboard, MessageSquare, Play, Plus, RefreshCw, Save, Trash2 } from "lucide-react";
import "./styles.css";
import {
  Agent,
  BootstrapStatus,
  Comment,
  Project,
  Run,
  RunEvent,
  Skill,
  SkillSource,
  Task,
  addComment,
  addRepo,
  createAgent,
  createProject,
  createTask,
  deleteAgent,
  deleteProject,
  deleteRepo,
  duplicateAgent,
  eventsURL,
  getAgent,
  getBootstrapStatus,
  getProject,
  getToken,
  heartbeat,
  improveAgentPrompt,
  listAgents,
  listComments,
  listInstalledSkills,
  listProjects,
  listRunEvents,
  listRuns,
  listSkillSources,
  listTasks,
  pinSkillSource,
  runBootstrap,
  runTask,
  setAgentEnabled,
  setToken,
  syncSkillSource,
  updateAgent,
  updateTask,
  updateProject
} from "./lib/api";

type Route = "bootstrap" | "projects" | "project" | "board" | "agents" | "agent" | "skills";

type RouteState = {
  route: Route;
  projectId: string | null;
  agentId: string | null;
};

function routeFromPath(pathname = window.location.pathname): RouteState {
  const parts = pathname.split("/").filter(Boolean).map((part) => decodeURIComponent(part));
  if (parts[0] === "bootstrap") return { route: "bootstrap", projectId: null, agentId: null };
  if (parts[0] === "projects" && parts[1] && parts[2] === "board") return { route: "board", projectId: parts[1], agentId: null };
  if (parts[0] === "projects" && parts[1]) return { route: "project", projectId: parts[1], agentId: null };
  if (parts[0] === "agents" && parts[1]) return { route: "agent", projectId: null, agentId: parts[1] };
  if (parts[0] === "agents") return { route: "agents", projectId: null, agentId: null };
  if (parts[0] === "skills") return { route: "skills", projectId: null, agentId: null };
  return { route: "projects", projectId: null, agentId: null };
}

function pathForRoute(next: Route, id?: string) {
  switch (next) {
    case "bootstrap": return "/bootstrap";
    case "project": return id ? `/projects/${encodeURIComponent(id)}` : "/projects";
    case "board": return id ? `/projects/${encodeURIComponent(id)}/board` : "/projects";
    case "agents": return "/agents";
    case "agent": return id ? `/agents/${encodeURIComponent(id)}` : "/agents";
    case "skills": return "/skills";
    case "projects":
    default: return "/projects";
  }
}

function App() {
  const initialRoute = useMemo(() => routeFromPath(), []);
  const [route, setRoute] = useState<Route>(initialRoute.route);
  const [projectId, setProjectId] = useState<string | null>(initialRoute.projectId);
  const [agentId, setAgentId] = useState<string | null>(initialRoute.agentId);
  const [token, updateToken] = useState(getToken());
  const [bootstrapStatus, setBootstrapStatus] = useState<BootstrapStatus | null>(null);

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
        setBootstrapStatus(status);
        if (!status.bootstrapped) {
          navigate("bootstrap", undefined, true);
        } else if (routeFromPath().route === "bootstrap") {
          navigate("projects", undefined, true);
        }
      })
      .catch(() => navigate("bootstrap", undefined, true));
  }, []);

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
        {!bootstrapStatus?.bootstrapped && <button className={route === "bootstrap" ? "active" : ""} onClick={() => navigate("bootstrap")}>Bootstrap</button>}
        <button className={route === "projects" || route === "project" || route === "board" ? "active" : ""} onClick={() => navigate("projects")}><FolderGit2 size={16} /> Projects</button>
        <button className={route === "agents" || route === "agent" ? "active" : ""} onClick={() => navigate("agents")}><Bot size={16} /> Agents</button>
        <button className={route === "skills" ? "active" : ""} onClick={() => navigate("skills")}><RefreshCw size={16} /> Skill Sources</button>
        <label className="token">API token<input value={token} onChange={(event) => { updateToken(event.target.value); setToken(event.target.value); }} /></label>
      </aside>
      <section>
        {route === "bootstrap" && <BootstrapPage bootstrapStatus={bootstrapStatus} onStatus={setBootstrapStatus} onDone={(status) => { setBootstrapStatus(status); navigate("projects"); }} />}
        {route === "projects" && <ProjectsPage openProject={(id) => navigate("project", id)} openBoard={(id) => navigate("board", id)} />}
        {route === "project" && projectId && <ProjectPage id={projectId} onOpenBoard={() => navigate("board", projectId)} onDeleted={() => navigate("projects")} />}
        {route === "board" && projectId && <BoardPage id={projectId} />}
        {route === "agents" && <AgentsPage openAgent={(id) => navigate("agent", id)} />}
        {route === "agent" && agentId && <AgentDetailPage id={agentId} onSaved={(id) => navigate("agent", id)} onDeleted={() => navigate("agents")} />}
        {route === "skills" && <SkillSourcesPage />}
      </section>
    </main>
  );
}

function BootstrapPage({ bootstrapStatus, onStatus, onDone }: { bootstrapStatus: BootstrapStatus | null; onStatus: (status: BootstrapStatus) => void; onDone: (status: BootstrapStatus) => void }) {
  const [message, setMessage] = useState("Checking bootstrap status...");
  const [busy, setBusy] = useState(false);
  const [currentStatus, setCurrentStatus] = useState<BootstrapStatus | null>(bootstrapStatus);

  useEffect(() => {
    setCurrentStatus(bootstrapStatus);
  }, [bootstrapStatus]);

  useEffect(() => {
    getBootstrapStatus().then((status) => {
      setCurrentStatus(status);
      onStatus(status);
      setMessage(status.bootstrapped ? `${status.agents_count} agents seeded.` : "Bootstrap is required.");
    });
  }, [onStatus]);

  async function run() {
    setBusy(true);
    try {
      const status = await runBootstrap();
      setCurrentStatus(status);
      setMessage(`${status.agents_count} agents ready.`);
      onDone(status);
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return <Panel title="Bootstrap"><p>{message}</p>{!currentStatus?.bootstrapped && <button onClick={run} disabled={busy}><Check size={16} /> Run bootstrap</button>}</Panel>;
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

  return <Panel title={project.name}>
    <div className="grid-form">
      <input value={project.name} onChange={(e) => setProject({ ...project, name: e.target.value })} />
      <select value={project.default_cli_kind} onChange={(e) => setProject({ ...project, default_cli_kind: e.target.value as "claude" | "codex" })}><option>claude</option><option>codex</option></select>
      <button onClick={save}><Save size={16} /> Save</button>
      <button type="button" onClick={onOpenBoard}><LayoutDashboard size={16} /> Board</button>
    </div>
    <form className="grid-form" onSubmit={attachRepo}>
      <input placeholder="Allowlisted local repo path" value={repoPath} onChange={(e) => setRepoPath(e.target.value)} />
      <button><Plus size={16} /> Add repo</button>
    </form>
    <Error text={error} />
    <div className="list">{project.repos?.map((repo) => <article key={repo.id}><h3>{repo.local_path}</h3><span>{repo.default_branch} · {repo.status}</span><button onClick={async () => { await deleteRepo(repo.id); setProject(await getProject(project.id)); }}><Trash2 size={16} /> Remove</button></article>)}</div>
    <div className="danger-zone">
      <h2 className="section-title">Delete project</h2>
      <p>Type <strong>{project.name}</strong> to confirm.</p>
      <div className="confirm-form">
        <input value={deleteConfirmName} onChange={(e) => setDeleteConfirmName(e.target.value)} placeholder={project.name} />
        <button type="button" className="danger-button" onClick={removeProject} disabled={deleteBusy || deleteConfirmName !== project.name}><Trash2 size={16} /> Delete project</button>
      </div>
    </div>
  </Panel>;
}

function BoardPage({ id }: { id: string }) {
  const [project, setProject] = useState<Project | null>(null);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [taskForm, setTaskForm] = useState({ title: "", description: "", assignee_agent_id: "pm", priority: 0 });
  const [error, setError] = useState("");

  useEffect(() => {
    getProject(id).then(setProject).catch((e) => setError(e.message));
    listAgents().then(setAgents).catch((e) => setError(e.message));
    listTasks(id).then(setTasks).catch((e) => setError(e.message));
    const events = new EventSource(eventsURL());
    events.addEventListener("mp_events", () => listTasks(id).then(setTasks).catch(() => undefined));
    return () => events.close();
  }, [id]);

  if (!project) return <Panel title="Board"><Error text={error || "Loading..."} /></Panel>;

  async function submitTask(event: React.FormEvent) {
    event.preventDefault();
    const current = project;
    if (!current) return;
    try {
      const task = await createTask(current.id, { ...taskForm, assignee_agent_id: taskForm.assignee_agent_id || null, repo_id: current.repos?.[0]?.id || null, status: "todo" });
      setTasks([task, ...tasks]);
      setTaskForm({ title: "", description: "", assignee_agent_id: "pm", priority: 0 });
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
      <button><Plus size={16} /> Add task</button>
      <button type="button" onClick={async () => { await heartbeat(); setTasks(await listTasks(project.id)); }}><RefreshCw size={16} /> Heartbeat</button>
    </form>
    <Kanban tasks={tasks} agents={agents} onOpen={setSelectedTask} onMove={moveTask} />
    {selectedTask && <TaskDrawer task={selectedTask} agents={agents} onClose={() => setSelectedTask(null)} onRefresh={async () => { setTasks(await listTasks(project.id)); setSelectedTask(await updateTask(selectedTask.id, selectedTask)); }} />}
  </Panel>;
}

function Kanban({ tasks, agents, onOpen, onMove }: { tasks: Task[]; agents: Agent[]; onOpen: (task: Task) => void; onMove: (task: Task, status: Task["status"]) => void }) {
  const statuses: Task["status"][] = ["todo", "in_progress", "in_review", "blocked", "done", "cancelled"];
  const agentName = (id: string | null) => agents.find((agent) => agent.id === id)?.name || id || "Unassigned";
  return <div className="kanban">{statuses.map((status) => <section className="column" key={status}>
    <h2>{status.replace("_", " ")}</h2>
    {tasks.filter((task) => task.status === status).map((task) => <article className="task-card" key={task.id} onClick={() => onOpen(task)}>
      <h3>{task.title}</h3>
      <p>{task.description || "No description"}</p>
      <span>{agentName(task.assignee_agent_id)} · priority {task.priority}</span>
      <select value={task.status} onClick={(e) => e.stopPropagation()} onChange={(e) => onMove(task, e.target.value as Task["status"])}>{statuses.map((item) => <option key={item}>{item}</option>)}</select>
    </article>)}
  </section>)}</div>;
}

function TaskDrawer({ task, agents, onClose, onRefresh }: { task: Task; agents: Agent[]; onClose: () => void; onRefresh: () => Promise<void> }) {
  const [comments, setComments] = useState<Comment[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [comment, setComment] = useState("");
  const latestRun = runs[0];
  useEffect(() => {
    listComments(task.id).then(setComments);
    listRuns(task.id).then(setRuns);
  }, [task.id]);
  useEffect(() => {
    if (!latestRun) return;
    const timer = window.setInterval(() => {
      const since = events.length ? events[events.length - 1].id : 0;
      listRunEvents(latestRun.id, since).then((items) => items.length && setEvents((prev) => [...prev, ...items])).catch(() => undefined);
    }, 1500);
    return () => window.clearInterval(timer);
  }, [latestRun?.id, events]);
  return <div className="drawer">
    <div className="drawer-header"><h2>{task.title}</h2><button onClick={onClose}>Close</button></div>
    <p>{task.description || "No description"}</p>
    <span>{agents.find((agent) => agent.id === task.assignee_agent_id)?.name || "Unassigned"} · {task.status} · retries {task.retry_count}</span>
    <div className="toolbar"><button onClick={async () => { await runTask(task.id); await onRefresh(); setRuns(await listRuns(task.id)); }}><Play size={16} /> Run now</button></div>
    <h3>Comments</h3>
    <div className="timeline">{comments.map((item) => <p key={item.id}><strong>{item.author}</strong>: {item.body}</p>)}</div>
    <form className="comment-form" onSubmit={async (e) => { e.preventDefault(); await addComment(task.id, comment); setComment(""); setComments(await listComments(task.id)); }}><input value={comment} onChange={(e) => setComment(e.target.value)} placeholder="Add comment" /><button><MessageSquare size={16} /> Comment</button></form>
    <h3>Runs</h3>
    <div className="runs">{runs.map((run) => <p key={run.id}>{run.status} · {run.cli_kind} · {run.started_at || "not started"}</p>)}</div>
    <h3>Latest Log</h3>
    <pre>{events.map((event) => `[${event.level}] ${event.message}`).join("\n") || "No events yet."}</pre>
  </div>;
}

function AgentsPage({ openAgent }: { openAgent: (id: string) => void }) {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [error, setError] = useState("");
  useEffect(() => { listAgents().then(setAgents).catch((e) => setError(e.message)); }, []);

  return <Panel title="Agents">
    <div className="toolbar"><button onClick={() => openAgent("new")}><Plus size={16} /> New agent</button></div>
    <div className="list">
      {agents.map((agent) => <article className="clickable" key={agent.id} onClick={() => openAgent(agent.id)}>
        <h3>{agent.name}</h3>
        <span>{agent.role} · {agent.cli_kind} · {agent.enabled ? "enabled" : "disabled"} · {agent.skills?.length || 0} skills</span>
      </article>)}
    </div>
    <Error text={error} />
  </Panel>;
}

function AgentDetailPage({ id, onSaved, onDeleted }: { id: string; onSaved: (id: string) => void; onDeleted: () => void }) {
  const blank = useMemo<Agent>(() => ({ id: "", name: "", role: "custom", role_prompt: "", cli_kind: "codex", cli_profile: null, enabled: true, skills: [] }), []);
  const [agent, setAgent] = useState<Agent | null>(id === "new" ? blank : null);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [improving, setImproving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    listInstalledSkills().then(setSkills).catch((e) => setError(e.message));
    if (id === "new") {
      setAgent(blank);
      setSelectedSkillIds([]);
      return;
    }
    getAgent(id).then((item) => {
      setAgent(item);
      setSelectedSkillIds((item.skills || []).map((skill) => skill.id));
    }).catch((e) => setError(e.message));
  }, [blank, id]);

  if (!agent) return <Panel title="Agent"><Error text={error || "Loading..."} /></Panel>;

  function toggleSkill(skillID: string, checked: boolean) {
    setSelectedSkillIds(checked ? [...selectedSkillIds, skillID] : selectedSkillIds.filter((item) => item !== skillID));
  }
  async function save() {
    const current = agent;
    if (!current) return;
    setBusy(true);
    try {
      const saved = current.id ? await updateAgent(current.id, { ...current, skill_ids: selectedSkillIds }) : await createAgent({ ...current, skill_ids: selectedSkillIds });
      setAgent(saved);
      setSelectedSkillIds((saved.skills || []).map((skill) => skill.id));
      onSaved(saved.id);
    } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }
  async function improvePrompt() {
    const current = agent;
    if (!current?.id) {
      setError("Save the agent before improving its base prompt.");
      return;
    }
    setImproving(true);
    try {
      const improved = await improveAgentPrompt(current.id, { ...current, skill_ids: selectedSkillIds });
      setAgent({ ...current, role_prompt: improved.role_prompt });
    } catch (e) { setError((e as Error).message); } finally { setImproving(false); }
  }

  return <Panel title={agent.id ? agent.name : "New agent"}>
    <div className="editor">
      <input value={agent.name} onChange={(e) => setAgent({ ...agent, name: e.target.value })} placeholder="Name" />
      <input value={agent.role} onChange={(e) => setAgent({ ...agent, role: e.target.value })} placeholder="Role" />
      <select value={agent.cli_kind} onChange={(e) => setAgent({ ...agent, cli_kind: e.target.value as "claude" | "codex" })}><option>claude</option><option>codex</option></select>
      <textarea value={agent.role_prompt} onChange={(e) => setAgent({ ...agent, role_prompt: e.target.value })} placeholder="Role prompt" />
      <label><input type="checkbox" checked={agent.enabled} onChange={(e) => setAgent({ ...agent, enabled: e.target.checked })} /> Enabled</label>
      <h2 className="section-title">Skills</h2>
      <div className="skill-picker">{skills.map((skill) => <label key={skill.id}><input type="checkbox" checked={selectedSkillIds.includes(skill.id)} onChange={(e) => toggleSkill(skill.id, e.target.checked)} /> <span>{skill.name}</span></label>)}</div>
      <div className="toolbar">
        <button onClick={save} disabled={busy}><Save size={16} /> Save</button>
        {agent.id && <button onClick={improvePrompt} disabled={improving}><RefreshCw size={16} /> Improve prompt</button>}
        {agent.id && <button onClick={async () => setAgent(await setAgentEnabled(agent.id, !agent.enabled))}>{agent.enabled ? "Disable" : "Enable"}</button>}
        {agent.id && <button onClick={async () => { const copy = await duplicateAgent(agent.id); onSaved(copy.id); }}><Copy size={16} /> Duplicate</button>}
        {agent.id && <button onClick={async () => { await deleteAgent(agent.id); onDeleted(); }}><Trash2 size={16} /> Delete</button>}
      </div>
    </div>
    <Error text={error} />
  </Panel>;
}

function SkillSourcesPage() {
  const [sources, setSources] = useState<SkillSource[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [sha, setSha] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  async function refresh() {
    const [nextSources, nextSkills] = await Promise.all([listSkillSources(), listInstalledSkills()]);
    setSources(nextSources);
    setSkills(nextSkills);
  }
  useEffect(() => { refresh().catch((e) => setError(e.message)); }, []);
  const sourceNames = useMemo(() => Object.fromEntries(sources.map((source) => [source.id, source.name])), [sources]);

  return <Panel title="Skill Sources">
    <Error text={error} />
    <div className="list">{sources.map((source) => <article key={source.id}>
      <h3>{source.name}</h3>
      <p>{source.upstream_url}</p>
      <span>{source.kind} · pinned {source.pinned_sha} · {source.last_synced_at ? "synced" : "never synced"}</span>
      <div className="toolbar">
        <button onClick={async () => { await syncSkillSource(source.id); await refresh(); }}><RefreshCw size={16} /> Sync</button>
        <input placeholder="New SHA" value={sha[source.id] || ""} onChange={(e) => setSha({ ...sha, [source.id]: e.target.value })} />
        <button onClick={async () => { await pinSkillSource(source.id, sha[source.id]); await refresh(); }}>Pin</button>
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
            {skills.map((skill) => <tr key={skill.id}>
              <td>{skill.name}</td>
              <td>{sourceNames[skill.source_id] || skill.source_id}</td>
              <td>{skill.path_in_source}</td>
              <td>{skill.version || "n/a"}</td>
            </tr>)}
          </tbody>
        </table>
      </div>}
  </Panel>;
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return <div className="panel"><h1>{title}</h1>{children}</div>;
}

function Error({ text }: { text: string }) {
  return text ? <p className="error">{text}</p> : null;
}

createRoot(document.getElementById("root")!).render(<App />);
