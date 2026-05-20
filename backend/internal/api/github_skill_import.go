package api

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"mini-paperclip/backend/internal/httpx"
	"mini-paperclip/backend/internal/store"
)

type githubSkillImportRequest struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

type parsedGitHubSkillURL struct {
	CloneURL    string
	Ref         string
	PathFilter  string
	DefaultName string
}

func (a *API) importGitHubSkillSource(w http.ResponseWriter, r *http.Request) {
	var body githubSkillImportRequest
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	parsed, err := parseGitHubSkillURL(body.URL)
	if err != nil {
		respond(w, nil, err)
		return
	}
	if err := a.cfg.ValidateSkillSource(parsed.CloneURL, parsed.Ref); err != nil {
		respond(w, nil, err)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = parsed.DefaultName
	}
	source, err := a.store.CreateSkillSource(r.Context(), store.SkillSourceInput{
		Name:        name,
		UpstreamURL: parsed.CloneURL,
		PinnedSHA:   parsed.Ref,
		PathFilter:  parsed.PathFilter,
		Kind:        "custom",
	})
	if err != nil {
		respond(w, nil, err)
		return
	}
	err = a.bootstrap.SyncSource(r.Context(), source.ID)
	if err != nil {
		respond(w, nil, err)
		return
	}
	source, err = a.store.GetSkillSource(r.Context(), source.ID)
	respondCreated(w, source, err)
}

func parseGitHubSkillURL(raw string) (parsedGitHubSkillURL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return parsedGitHubSkillURL{}, err
	}
	host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	if u.Scheme != "https" || host != "github.com" {
		return parsedGitHubSkillURL{}, fmt.Errorf("expected an https://github.com/... skill URL")
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			return parsedGitHubSkillURL{}, err
		}
		parts[i] = decoded
	}
	if len(parts) < 5 {
		return parsedGitHubSkillURL{}, fmt.Errorf("expected a GitHub tree or blob URL to a skill directory")
	}
	owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
	mode, ref := parts[2], parts[3]
	rest := parts[4:]
	if owner == "" || repo == "" || ref == "" {
		return parsedGitHubSkillURL{}, fmt.Errorf("invalid GitHub skill URL")
	}
	var filter string
	switch mode {
	case "tree":
		filter = path.Join(rest...)
		if filter == "." || filter == "" {
			return parsedGitHubSkillURL{}, fmt.Errorf("tree URL must point to a skill directory")
		}
		filter = path.Join(filter, "SKILL.md")
	case "blob":
		filter = path.Join(rest...)
		if path.Base(filter) != "SKILL.md" {
			return parsedGitHubSkillURL{}, fmt.Errorf("blob URL must point to a SKILL.md file")
		}
	default:
		return parsedGitHubSkillURL{}, fmt.Errorf("expected a GitHub tree or blob URL")
	}
	skillName := path.Base(path.Dir(filter))
	return parsedGitHubSkillURL{
		CloneURL:    fmt.Sprintf("https://github.com/%s/%s.git", owner, repo),
		Ref:         ref,
		PathFilter:  filter,
		DefaultName: sanitizeSourceName(fmt.Sprintf("%s-%s-%s", owner, repo, skillName)),
	}, nil
}

func sanitizeSourceName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
