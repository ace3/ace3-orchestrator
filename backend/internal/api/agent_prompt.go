package api

import (
	"net/http"

	"mini-paperclip/backend/internal/httpx"
)

func (a *API) improveAgentPrompt(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, http.StatusMethodNotAllowed, "repo_locked_agents", "agent prompts are defined in the repo and cannot be improved through the API")
}
