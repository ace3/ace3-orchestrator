package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"mini-paperclip/backend/internal/auth"
	"mini-paperclip/backend/internal/bootstrap"
	"mini-paperclip/backend/internal/config"
	"mini-paperclip/backend/internal/fsutil"
	"mini-paperclip/backend/internal/httpx"
	"mini-paperclip/backend/internal/orchestrator"
	"mini-paperclip/backend/internal/store"
)

type API struct {
	cfg       config.Config
	store     *store.Store
	bootstrap *bootstrap.Service
	orch      *orchestrator.Orchestrator
}

func NewRouter(cfg config.Config, st *store.Store, bs *bootstrap.Service, orch *orchestrator.Orchestrator) http.Handler {
	api := &API{cfg: cfg, store: st, bootstrap: bs, orch: orch}
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Route("/api", func(r chi.Router) {
		r.Use(auth.Bearer(cfg.APIToken))
		r.Get("/bootstrap-status", api.bootstrapStatus)
		r.Post("/bootstrap/run", api.bootstrapRun)
		r.Get("/agents", api.listAgents)
		r.Post("/agents", api.createAgent)
		r.Get("/agents/{id}", api.getAgent)
		r.Patch("/agents/{id}", api.updateAgent)
		r.Delete("/agents/{id}", api.deleteAgent)
		r.Post("/agents/{id}/duplicate", api.duplicateAgent)
		r.Post("/agents/{id}/enabled", api.setAgentEnabled)
		r.Post("/agents/{id}/improve-prompt", api.improveAgentPrompt)
		r.Get("/projects", api.listProjects)
		r.Post("/projects", api.createProject)
		r.Get("/projects/{id}", api.getProject)
		r.Patch("/projects/{id}", api.updateProject)
		r.Delete("/projects/{id}", api.deleteProject)
		r.Get("/projects/{id}/repos", api.listRepos)
		r.Post("/projects/{id}/repos", api.createRepo)
		r.Delete("/repos/{id}", api.deleteRepo)
		r.Get("/fs/browse", api.browseFS)
		r.Get("/projects/{id}/tasks", api.listTasks)
		r.Post("/projects/{id}/tasks", api.createTask)
		r.Get("/tasks/{id}", api.getTask)
		r.Patch("/tasks/{id}", api.updateTask)
		r.Post("/tasks/{id}/comments", api.addComment)
		r.Get("/tasks/{id}/comments", api.listComments)
		r.Post("/tasks/{id}/run", api.runTask)
		r.Get("/tasks/{id}/runs", api.listRuns)
		r.Get("/runs/{id}", api.getRun)
		r.Get("/runs/{id}/events", api.listRunEvents)
		r.Get("/skills", api.listInstalledSkills)
		r.Get("/skill-sources", api.listSkillSources)
		r.Post("/skill-sources/{id}/sync", api.syncSkillSource)
		r.Post("/skill-sources/{id}/pin", api.pinSkillSource)
		r.Post("/heartbeat", api.heartbeat)
		r.Get("/events", api.events)
	})
	return r
}

func (a *API) bootstrapStatus(w http.ResponseWriter, r *http.Request) {
	status, err := a.bootstrap.Status(r.Context())
	respond(w, status, err)
}

func (a *API) bootstrapRun(w http.ResponseWriter, r *http.Request) {
	status, err := a.bootstrap.Run(r.Context())
	respond(w, status, err)
}

func (a *API) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := a.store.ListAgents(r.Context())
	respond(w, agents, err)
}

func (a *API) createAgent(w http.ResponseWriter, r *http.Request) {
	var in store.AgentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	agent, err := a.store.CreateAgent(r.Context(), in)
	respondCreated(w, agent, err)
}

func (a *API) getAgent(w http.ResponseWriter, r *http.Request) {
	agent, err := a.store.GetAgent(r.Context(), chi.URLParam(r, "id"))
	respond(w, agent, err)
}

func (a *API) updateAgent(w http.ResponseWriter, r *http.Request) {
	var in store.AgentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	agent, err := a.store.UpdateAgent(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, agent, err)
}

func (a *API) deleteAgent(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteAgent(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (a *API) duplicateAgent(w http.ResponseWriter, r *http.Request) {
	agent, err := a.store.DuplicateAgent(r.Context(), chi.URLParam(r, "id"))
	respondCreated(w, agent, err)
}

func (a *API) setAgentEnabled(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	agent, err := a.store.SetAgentEnabled(r.Context(), chi.URLParam(r, "id"), body.Enabled)
	respond(w, agent, err)
}

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := a.store.ListProjects(r.Context())
	respond(w, projects, err)
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) {
	var in store.ProjectInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	project, err := a.store.CreateProject(r.Context(), in)
	respondCreated(w, project, err)
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) {
	project, err := a.store.GetProject(r.Context(), chi.URLParam(r, "id"))
	respond(w, project, err)
}

func (a *API) updateProject(w http.ResponseWriter, r *http.Request) {
	var in store.ProjectInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	project, err := a.store.UpdateProject(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, project, err)
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteProject(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (a *API) listRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := a.store.ListRepos(r.Context(), chi.URLParam(r, "id"))
	respond(w, repos, err)
}

func (a *API) createRepo(w http.ResponseWriter, r *http.Request) {
	var in store.RepoInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	repo, err := a.store.CreateRepo(r.Context(), chi.URLParam(r, "id"), in)
	respondCreated(w, repo, err)
}

func (a *API) deleteRepo(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteRepo(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (a *API) browseFS(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" && len(a.cfg.RepoAllowlist) > 0 {
		path = a.cfg.RepoAllowlist[0]
	}
	entries, err := fsutil.Browse(path, a.cfg.RepoAllowlist)
	respond(w, map[string]any{"path": path, "entries": entries}, err)
}

func (a *API) listSkillSources(w http.ResponseWriter, r *http.Request) {
	sources, err := a.store.ListSkillSources(r.Context())
	respond(w, sources, err)
}

func (a *API) listInstalledSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := a.store.ListInstalledSkills(r.Context())
	respond(w, skills, err)
}

func (a *API) syncSkillSource(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := a.bootstrap.SyncSource(r.Context(), id)
	if err != nil {
		respond(w, nil, err)
		return
	}
	source, err := a.store.GetSkillSource(r.Context(), id)
	respond(w, source, err)
}

func (a *API) pinSkillSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SHA string `json:"sha"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	source, err := a.bootstrap.PinSource(r.Context(), chi.URLParam(r, "id"), body.SHA)
	respond(w, source, err)
}

func respondCreated(w http.ResponseWriter, value any, err error) {
	if err != nil {
		respond(w, nil, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, value)
}

func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		httpx.JSON(w, http.StatusOK, value)
		return
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, store.ErrConflict):
		httpx.Error(w, http.StatusConflict, "conflict", "operation conflicts with current state")
	case errors.Is(err, fsutil.ErrOutsideAllowlist):
		httpx.Error(w, http.StatusBadRequest, "outside_allowlist", "path is outside MP_REPO_ALLOWLIST")
	default:
		httpx.Error(w, http.StatusBadRequest, "request_failed", err.Error())
	}
}
