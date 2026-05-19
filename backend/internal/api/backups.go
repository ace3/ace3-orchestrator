package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"mini-paperclip/backend/internal/backup"
	"mini-paperclip/backend/internal/httpx"
)

type backupRefRequest struct {
	BackupID string   `json:"backup_id"`
	Bundles  []string `json:"bundles"`
	Confirm  string   `json:"confirm"`
}

func (a *API) backupService(w http.ResponseWriter) *backup.Service {
	if a.backups == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "backup_unavailable", "backup service is not configured")
		return nil
	}
	return a.backups
}

func (a *API) listBackups(w http.ResponseWriter, r *http.Request) {
	svc := a.backupService(w)
	if svc == nil {
		return
	}
	artifacts, err := svc.List()
	respond(w, artifacts, err)
}

func (a *API) downloadBackup(w http.ResponseWriter, r *http.Request) {
	svc := a.backupService(w)
	if svc == nil {
		return
	}
	path, artifact, err := svc.ArtifactPath(chi.URLParam(r, "id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+artifact.Filename+`"`)
	http.ServeFile(w, r, path)
}

func (a *API) createFullBackup(w http.ResponseWriter, r *http.Request) {
	svc := a.backupService(w)
	if svc == nil {
		return
	}
	artifact, err := svc.CreateFull(r.Context())
	respondCreated(w, artifact, err)
}

func (a *API) uploadFullBackup(w http.ResponseWriter, r *http.Request) {
	a.uploadBackup(w, r, backup.FullKind)
}

func (a *API) uploadAppBackup(w http.ResponseWriter, r *http.Request) {
	a.uploadBackup(w, r, backup.AppKind)
}

func (a *API) uploadBackup(w http.ResponseWriter, r *http.Request, kind string) {
	svc := a.backupService(w)
	if svc == nil {
		return
	}
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_upload", err.Error())
		return
	}
	file, header, err := r.FormFile("backup")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "missing_file", "multipart field backup is required")
		return
	}
	defer file.Close()
	artifact, err := svc.StoreUploaded(kind, filepath.Base(header.Filename), file)
	if err != nil {
		respond(w, nil, err)
		return
	}
	if kind == backup.AppKind {
		doc, err := svc.ReadAppArtifact(artifact.ID)
		if err != nil {
			respond(w, nil, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, map[string]any{"artifact": artifact, "validation": svc.ValidateApp(doc, nil)})
		return
	}
	validation, err := svc.ValidateFull(artifact.ID)
	if err != nil {
		respond(w, nil, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"artifact": artifact, "validation": validation})
}

func (a *API) validateFullBackup(w http.ResponseWriter, r *http.Request) {
	svc := a.backupService(w)
	if svc == nil {
		return
	}
	var body backupRefRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	validation, err := svc.ValidateFull(body.BackupID)
	respond(w, validation, err)
}

func (a *API) fullRestorePlan(w http.ResponseWriter, r *http.Request) {
	svc := a.backupService(w)
	if svc == nil {
		return
	}
	var body backupRefRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	plan, err := svc.RestorePlan(body.BackupID)
	respond(w, plan, err)
}

func (a *API) exportAppBackup(w http.ResponseWriter, r *http.Request) {
	svc := a.backupService(w)
	if svc == nil {
		return
	}
	var body backupRefRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	artifact, err := svc.ExportApp(r.Context(), body.Bundles)
	respondCreated(w, artifact, err)
}

func (a *API) validateAppBackup(w http.ResponseWriter, r *http.Request) {
	svc := a.backupService(w)
	if svc == nil {
		return
	}
	body, doc, ok := a.readAppBackupRef(w, r, svc)
	if !ok {
		return
	}
	respond(w, svc.ValidateApp(doc, body.Bundles), nil)
}

func (a *API) dryRunAppBackup(w http.ResponseWriter, r *http.Request) {
	svc := a.backupService(w)
	if svc == nil {
		return
	}
	body, doc, ok := a.readAppBackupRef(w, r, svc)
	if !ok {
		return
	}
	result, err := svc.DryRunApp(r.Context(), doc, body.Bundles)
	respond(w, result, err)
}

func (a *API) importAppBackup(w http.ResponseWriter, r *http.Request) {
	svc := a.backupService(w)
	if svc == nil {
		return
	}
	body, doc, ok := a.readAppBackupRef(w, r, svc)
	if !ok {
		return
	}
	result, err := svc.ImportApp(r.Context(), doc, body.Bundles, body.Confirm)
	respond(w, result, err)
}

func (a *API) readAppBackupRef(w http.ResponseWriter, r *http.Request, svc *backup.Service) (backupRefRequest, backup.AppExport, bool) {
	var body backupRefRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return body, backup.AppExport{}, false
	}
	doc, err := svc.ReadAppArtifact(body.BackupID)
	if err != nil {
		respond(w, nil, err)
		return body, backup.AppExport{}, false
	}
	return body, doc, true
}
