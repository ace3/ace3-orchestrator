package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"

	"mini-paperclip/backend/internal/httpx"
	"mini-paperclip/backend/internal/orchestrator"
	"mini-paperclip/backend/internal/store"
)

func (a *API) listTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := a.store.ListTasks(r.Context(), chi.URLParam(r, "id"))
	respond(w, tasks, err)
}

func (a *API) createTask(w http.ResponseWriter, r *http.Request) {
	var in store.TaskInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	task, err := a.store.CreateTask(r.Context(), chi.URLParam(r, "id"), in)
	respondCreated(w, task, err)
}

func (a *API) getTask(w http.ResponseWriter, r *http.Request) {
	task, err := a.store.GetTask(r.Context(), chi.URLParam(r, "id"))
	respond(w, task, err)
}

func (a *API) updateTask(w http.ResponseWriter, r *http.Request) {
	var in store.TaskInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	task, err := a.store.UpdateTask(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, task, err)
}

func (a *API) listComments(w http.ResponseWriter, r *http.Request) {
	comments, err := a.store.ListComments(r.Context(), chi.URLParam(r, "id"))
	respond(w, comments, err)
}

func (a *API) addComment(w http.ResponseWriter, r *http.Request) {
	var in store.CommentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	comment, err := a.store.AddComment(r.Context(), chi.URLParam(r, "id"), "human:ignas", in.Body)
	respondCreated(w, comment, err)
}

func (a *API) runTask(w http.ResponseWriter, r *http.Request) {
	run, err := a.orch.EnqueueTask(r.Context(), chi.URLParam(r, "id"))
	respondCreated(w, run, err)
}

func (a *API) listRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := a.store.ListRuns(r.Context(), chi.URLParam(r, "id"))
	respond(w, runs, err)
}

func (a *API) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := a.store.GetRun(r.Context(), chi.URLParam(r, "id"))
	respond(w, run, err)
}

func (a *API) listRunEvents(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	events, err := a.store.ListRunEvents(r.Context(), chi.URLParam(r, "id"), since)
	respond(w, events, err)
}

func (a *API) heartbeat(w http.ResponseWriter, r *http.Request) {
	count, err := a.orch.DispatchOnce(r.Context())
	respond(w, map[string]int{"queued": count}, err)
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "sse_unavailable", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	listener := pq.NewListener(a.cfg.DBDSN, 5*time.Second, time.Minute, nil)
	defer listener.Close()
	if err := listener.Listen("mp_events"); err != nil {
		fmt.Fprint(w, orchestrator.FormatSSE("error", map[string]string{"message": err.Error()}))
		flusher.Flush()
		return
	}
	fmt.Fprint(w, orchestrator.FormatSSE("ready", map[string]string{"status": "ok"}))
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case notification := <-listener.Notify:
			if notification != nil {
				fmt.Fprintf(w, "event: mp_events\ndata: %s\n\n", notification.Extra)
				flusher.Flush()
			}
		}
	}
}
