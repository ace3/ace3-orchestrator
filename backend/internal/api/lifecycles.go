package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"mini-paperclip/backend/internal/httpx"
	"mini-paperclip/backend/internal/lifecycles"
	"mini-paperclip/backend/internal/store"
)

func (a *API) listLifecycles(w http.ResponseWriter, r *http.Request) {
	lifecycles, err := a.store.ListLifecycles(r.Context())
	respond(w, lifecycles, err)
}

func (a *API) getLifecycle(w http.ResponseWriter, r *http.Request) {
	lifecycle, err := a.store.GetLifecycle(r.Context(), chi.URLParam(r, "id"))
	respond(w, lifecycle, err)
}

func (a *API) createLifecycle(w http.ResponseWriter, r *http.Request) {
	var in store.LifecycleInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	lifecycle, err := a.store.CreateLifecycle(r.Context(), in)
	respondCreated(w, lifecycle, err)
}

func (a *API) updateLifecycle(w http.ResponseWriter, r *http.Request) {
	var in store.LifecycleInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	lifecycle, err := a.store.UpdateLifecycle(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, lifecycle, err)
}

func (a *API) deleteLifecycle(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteLifecycle(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrLifecycleIsDefault) {
		httpx.Error(w, http.StatusConflict, "lifecycle_is_default", "default lifecycles cannot be deleted")
		return
	}
	if err != nil {
		respond(w, nil, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (a *API) lifecycleTagVocabulary(w http.ResponseWriter, r *http.Request) {
	if a.lifecycles == nil {
		a.lifecycles = lifecycles.New(a.store)
	}
	tags, err := a.lifecycles.TagVocabulary(r.Context(), chi.URLParam(r, "id"))
	respond(w, map[string][]string{"tags": tags}, err)
}

func (a *API) getDefaultModel(w http.ResponseWriter, r *http.Request) {
	value, err := a.store.GetSetting(r.Context(), lifecycles.DefaultModelSetting)
	if errors.Is(err, store.ErrNotFound) {
		value, err = a.store.SetSetting(r.Context(), lifecycles.DefaultModelSetting, "claude-sonnet-4-6")
	}
	respond(w, map[string]string{"value": value}, err)
}

func (a *API) setDefaultModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value string `json:"value"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	value, err := a.store.SetSetting(r.Context(), lifecycles.DefaultModelSetting, body.Value)
	respond(w, map[string]string{"value": value}, err)
}
