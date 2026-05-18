package api

import (
	"net/http"

	"mini-paperclip/backend/internal/httpx"
)

func (a *API) improveAgentPrompt(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, http.StatusMethodNotAllowed, "unsupported_agent_prompt_improve", "agent prompt generation is not supported; edit role_prompt directly")
}
