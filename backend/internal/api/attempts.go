package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"mini-paperclip/backend/internal/httpx"
)

type attemptDiffResponse struct {
	RunID        string     `json:"run_id"`
	AttemptIndex *int       `json:"attempt_index"`
	AttemptLabel string     `json:"attempt_label"`
	Files        []diffFile `json:"files"`
	Raw          string     `json:"raw"`
	Truncated    bool       `json:"truncated"`
	Error        string     `json:"error,omitempty"`
}

func (a *API) listAttempts(w http.ResponseWriter, r *http.Request) {
	runs, err := a.store.ListAttemptRuns(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "groupID"))
	respond(w, runs, err)
}

func (a *API) attemptDiffs(w http.ResponseWriter, r *http.Request) {
	sources, err := a.store.AttemptDiffSources(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "groupID"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	out := make([]attemptDiffResponse, 0, len(sources))
	for _, source := range sources {
		baseRef := source.BaseRef
		if baseRef == "" {
			baseRef = "main"
		}
		raw, truncated, err := gitDiff(r.Context(), source.WorktreePath, baseRef)
		item := attemptDiffResponse{RunID: source.RunID, AttemptIndex: source.AttemptIndex, AttemptLabel: source.AttemptLabel, Truncated: truncated}
		if err != nil {
			item.Error = err.Error()
		} else {
			item.Raw = raw
			item.Files = parseUnifiedDiff(raw)
		}
		out = append(out, item)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) selectAttempt(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RunID string `json:"run_id"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	task, err := a.orch.SelectAttempt(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "groupID"), in.RunID)
	respond(w, task, err)
}
