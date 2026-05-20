package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"mini-paperclip/backend/internal/httpx"
	"mini-paperclip/backend/internal/store"
)

func (a *API) createDraft(w http.ResponseWriter, r *http.Request) {
	var in store.DraftCreateInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	result, err := a.store.CreateDraft(r.Context(), in)
	respondCreated(w, result, err)
}

func (a *API) draftTurn(w http.ResponseWriter, r *http.Request) {
	var in store.DraftTurnInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	result, err := a.store.DraftTurn(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, result, err)
}

func (a *API) finalizeDraft(w http.ResponseWriter, r *http.Request) {
	result, err := a.store.FinalizeDraft(r.Context(), chi.URLParam(r, "id"))
	respond(w, result, err)
}

func (a *API) submitDraft(w http.ResponseWriter, r *http.Request) {
	var in store.DraftSubmitInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	task, err := a.store.SubmitDraft(r.Context(), chi.URLParam(r, "id"), in)
	respondCreated(w, task, err)
}

func (a *API) discardDraft(w http.ResponseWriter, r *http.Request) {
	err := a.store.DiscardDraft(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
