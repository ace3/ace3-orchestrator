package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mini-paperclip/backend/internal/auth"
	"mini-paperclip/backend/internal/backup"
	"mini-paperclip/backend/internal/bootstrap"
	"mini-paperclip/backend/internal/config"
	"mini-paperclip/backend/internal/fsutil"
	"mini-paperclip/backend/internal/httpx"
	"mini-paperclip/backend/internal/lifecycles"
	"mini-paperclip/backend/internal/orchestrator"
	"mini-paperclip/backend/internal/store"
)

type API struct {
	cfg        config.Config
	store      *store.Store
	backups    *backup.Service
	bootstrap  *bootstrap.Service
	orch       *orchestrator.Orchestrator
	lifecycles *lifecycles.Service
}

type debugHealthResponse struct {
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
}

func NewRouter(cfg config.Config, st *store.Store, bs *bootstrap.Service, orch *orchestrator.Orchestrator, lifecycleServices ...*lifecycles.Service) http.Handler {
	var lifecycleService *lifecycles.Service
	if len(lifecycleServices) > 0 {
		lifecycleService = lifecycleServices[0]
	}
	var backupService *backup.Service
	if st != nil {
		backupService = backup.New(st.DB(), cfg.DBDSN, cfg.BackupDir)
	}
	api := &API{cfg: cfg, store: st, backups: backupService, bootstrap: bs, orch: orch, lifecycles: lifecycleService}
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/api/debug/health", debugHealth)
	r.Route("/api", func(r chi.Router) {
		r.Use(auth.Bearer(cfg.APIToken))
		r.Use(api.auditMiddleware)
		r.Get("/bootstrap-status", api.bootstrapStatus)
		r.Post("/bootstrap/run", api.bootstrapRun)
		r.Get("/backups", api.listBackups)
		r.Get("/backups/{id}/download", api.downloadBackup)
		r.Post("/backups/full", api.createFullBackup)
		r.Post("/backups/full/upload", api.uploadFullBackup)
		r.Post("/backups/full/validate", api.validateFullBackup)
		r.Post("/backups/full/restore-plan", api.fullRestorePlan)
		r.Post("/backups/app/export", api.exportAppBackup)
		r.Post("/backups/app/upload", api.uploadAppBackup)
		r.Post("/backups/app/validate", api.validateAppBackup)
		r.Post("/backups/app/dry-run", api.dryRunAppBackup)
		r.Post("/backups/app/import", api.importAppBackup)
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
		r.Post("/tasks/{id}/checkout", api.checkoutTask)
		r.Post("/tasks/{id}/release", api.releaseTask)
		r.Post("/tasks/{id}/comments", api.addComment)
		r.Get("/tasks/{id}/comments", api.listComments)
		r.Get("/tasks/{id}/wakeups", api.listWakeups)
		r.Post("/tasks/{id}/wakeups", api.createWakeup)
		r.Get("/tasks/{id}/interactions", api.listInteractions)
		r.Post("/tasks/{id}/interactions", api.createInteraction)
		r.Post("/task-interactions/{id}/answer", api.answerInteraction)
		r.Post("/task-interactions/{id}/accept", api.acceptInteraction)
		r.Post("/task-interactions/{id}/reject", api.rejectInteraction)
		r.Get("/tasks/{id}/liveness", api.taskLiveness)
		r.Get("/tasks/{id}/active-run", api.activeRun)
		r.Get("/tasks/{id}/artifacts", api.listTaskArtifacts)
		r.Post("/tasks/{id}/artifacts", api.createTaskArtifact)
		r.Get("/task-artifacts/{id}", api.getTaskArtifact)
		r.Patch("/task-artifacts/{id}", api.updateTaskArtifact)
		r.Delete("/task-artifacts/{id}", api.deleteTaskArtifact)
		r.Post("/tasks/{id}/run", api.runTask)
		r.Get("/tasks/{id}/runs", api.listRuns)
		r.Get("/runs/{id}", api.getRun)
		r.Get("/runs/{id}/events", api.listRunEvents)
		r.Get("/skills", api.listInstalledSkills)
		r.Get("/skills/{id}/tree", api.getSkillTree)
		r.Get("/skills/{id}/content", api.getSkillContent)
		r.Patch("/skills/{id}", api.updateSkill)
		r.Get("/skill-sources", api.listSkillSources)
		r.Get("/skill-drift", api.skillDrift)
		r.Get("/lifecycles", api.listLifecycles)
		r.Post("/lifecycles", api.createLifecycle)
		r.Get("/lifecycles/{id}", api.getLifecycle)
		r.Patch("/lifecycles/{id}", api.updateLifecycle)
		r.Delete("/lifecycles/{id}", api.deleteLifecycle)
		r.Get("/lifecycles/{id}/tag-vocabulary", api.lifecycleTagVocabulary)
		r.Get("/settings/default-model", api.getDefaultModel)
		r.Put("/settings/default-model", api.setDefaultModel)
		r.Post("/skill-sources", api.createSkillSource)
		r.Post("/skill-sources/check-updates", api.checkSkillSourceUpdates)
		r.Post("/skill-sources/import-github-skill", api.importGitHubSkillSource)
		r.Post("/skill-sources/{id}/sync", api.syncSkillSource)
		r.Post("/skill-sources/{id}/pin", api.pinSkillSource)
		r.Delete("/skill-sources/{id}", api.deleteSkillSource)
		r.Get("/orchestrator-map", api.orchestratorMap)
		r.Post("/heartbeat", api.heartbeat)
		r.Get("/events", api.events)
	})
	return r
}

type auditResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (a *API) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", requestID)
		rec := &auditResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		metadata, _ := json.Marshal(map[string]any{"status": rec.status})
		if a.store != nil {
			a.store.RecordAuditEvent(r.Context(), store.AuditEventInput{
				Actor:     bearerFingerprint(r.Header.Get("Authorization")),
				Action:    r.Method + " " + chi.RouteContext(r.Context()).RoutePattern(),
				Target:    r.URL.Path,
				RequestID: requestID,
				IP:        clientIP(r),
				Metadata:  metadata,
			})
		}
	})
}

func bearerFingerprint(header string) string {
	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" {
		return "bearer:missing"
	}
	sum := sha256.Sum256([]byte(token))
	return "bearer:sha256:" + hex.EncodeToString(sum[:])[:16]
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return r.RemoteAddr
}

func debugHealth(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, debugHealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UnixMilli(),
	})
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
	id := chi.URLParam(r, "id")
	if a.lifecycleReferencesAgent(r.Context(), id) {
		respond(w, nil, store.ErrConflict)
		return
	}
	err := a.store.DeleteAgent(r.Context(), id)
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

func (a *API) lifecycleReferencesAgent(ctx context.Context, id string) bool {
	if a == nil || a.store == nil {
		return true
	}
	lifecycles, err := a.store.ListLifecycles(ctx)
	if err != nil {
		return false
	}
	for _, lifecycle := range lifecycles {
		for _, step := range lifecycle.Steps {
			if step.AgentID == id {
				return true
			}
		}
	}
	return false
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
	entries, err := fsutil.BrowseWithAliases(path, a.cfg.RepoAllowlist, a.cfg.RepoPathAliases)
	respond(w, map[string]any{"path": path, "entries": entries}, err)
}

func (a *API) listSkillSources(w http.ResponseWriter, r *http.Request) {
	sources, err := a.store.ListSkillSources(r.Context())
	respond(w, sources, err)
}

func (a *API) createSkillSource(w http.ResponseWriter, r *http.Request) {
	var in store.SkillSourceInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if err := a.cfg.ValidateSkillSource(in.UpstreamURL, in.PinnedSHA); err != nil {
		respond(w, nil, err)
		return
	}
	source, err := a.store.CreateSkillSource(r.Context(), in)
	respondCreated(w, source, err)
}

func (a *API) listInstalledSkills(w http.ResponseWriter, r *http.Request) {
	includeIgnored := r.URL.Query().Get("include_ignored") == "true"
	skills, err := a.store.ListInstalledSkills(r.Context(), includeIgnored)
	respond(w, skills, err)
}

func (a *API) updateSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ignored bool `json:"ignored"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	skill, err := a.store.SetSkillIgnored(r.Context(), chi.URLParam(r, "id"), body.Ignored)
	respond(w, skill, err)
}

func (a *API) syncSkillSource(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	source, err := a.store.GetSkillSource(r.Context(), id)
	if err != nil {
		respond(w, nil, err)
		return
	}
	if err := a.cfg.ValidateSkillSource(source.UpstreamURL, source.PinnedSHA); err != nil {
		respond(w, nil, err)
		return
	}
	err = a.bootstrap.SyncSource(r.Context(), id)
	if err != nil {
		respond(w, nil, err)
		return
	}
	source, err = a.store.GetSkillSource(r.Context(), id)
	respond(w, source, err)
}

func (a *API) skillDrift(w http.ResponseWriter, r *http.Request) {
	report, err := a.bootstrap.CheckSkillDrift(r.Context())
	respond(w, report, err)
}

func (a *API) checkSkillSourceUpdates(w http.ResponseWriter, r *http.Request) {
	if err := a.bootstrap.CheckUpdates(r.Context()); err != nil {
		respond(w, nil, err)
		return
	}
	sources, err := a.store.ListSkillSources(r.Context())
	respond(w, sources, err)
}

func (a *API) pinSkillSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SHA string `json:"sha"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	source, err := a.store.GetSkillSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	if err := a.cfg.ValidateSkillSource(source.UpstreamURL, body.SHA); err != nil {
		respond(w, nil, err)
		return
	}
	source, err = a.bootstrap.PinSource(r.Context(), chi.URLParam(r, "id"), body.SHA)
	respond(w, source, err)
}

func (a *API) deleteSkillSource(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteSkillSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
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
	case errors.Is(err, backup.ErrValidation):
		httpx.Error(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, store.ErrConflict):
		httpx.Error(w, http.StatusConflict, "conflict", "operation conflicts with current state")
	case errors.Is(err, store.ErrLifecycleIsDefault):
		httpx.Error(w, http.StatusConflict, "lifecycle_is_default", "default lifecycles cannot be deleted")
	case errors.Is(err, fsutil.ErrOutsideAllowlist):
		httpx.Error(w, http.StatusBadRequest, "outside_allowlist", "path is outside MP_REPO_ALLOWLIST")
	default:
		httpx.Error(w, http.StatusBadRequest, "request_failed", err.Error())
	}
}
