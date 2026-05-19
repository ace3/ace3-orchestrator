package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"mini-paperclip/backend/internal/models"
)

const maxSkillPreviewBytes = 512 * 1024

type skillTreeEntry struct {
	Name     string           `json:"name"`
	Path     string           `json:"path"`
	Type     string           `json:"type"`
	Children []skillTreeEntry `json:"children,omitempty"`
}

type skillContentResponse struct {
	SkillID string `json:"skill_id"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (a *API) getSkillTree(w http.ResponseWriter, r *http.Request) {
	skill, source, root, err := a.skillCacheRoot(r, chi.URLParam(r, "id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	tree, err := buildSkillTree(root)
	respond(w, map[string]any{"skill": skill, "source": source, "root": tree}, err)
}

func (a *API) getSkillContent(w http.ResponseWriter, r *http.Request) {
	skill, _, root, err := a.skillCacheRoot(r, chi.URLParam(r, "id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	rel := strings.TrimSpace(r.URL.Query().Get("path"))
	if rel == "" {
		rel = "SKILL.md"
	}
	clean, path, err := safeSkillPath(root, rel)
	if err != nil {
		respond(w, nil, err)
		return
	}
	content, err := readTextPreview(path)
	respond(w, skillContentResponse{SkillID: skill.ID, Path: clean, Content: content}, err)
}

func (a *API) skillCacheRoot(r *http.Request, id string) (models.Skill, models.SkillSource, string, error) {
	skill, err := a.store.GetSkill(r.Context(), id)
	if err != nil {
		return skill, models.SkillSource{}, "", err
	}
	source, err := a.store.GetSkillSource(r.Context(), skill.SourceID)
	if err != nil {
		return skill, source, "", err
	}
	rel := filepath.Clean(skill.PathInSource)
	if filepath.IsAbs(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return skill, source, "", fmt.Errorf("invalid skill path %q", skill.PathInSource)
	}
	root := filepath.Join(a.cfg.SkillsCacheDir, source.Name, source.PinnedSHA, filepath.Dir(rel))
	return skill, source, root, nil
}

func buildSkillTree(root string) (skillTreeEntry, error) {
	info, err := os.Stat(root)
	if err != nil {
		return skillTreeEntry{}, err
	}
	if !info.IsDir() {
		return skillTreeEntry{}, fmt.Errorf("skill root is not a directory")
	}
	count := 0
	return buildTreeNode(root, root, &count)
}

func buildTreeNode(root, path string, count *int) (skillTreeEntry, error) {
	if *count > 500 {
		return skillTreeEntry{}, fmt.Errorf("skill tree has too many entries")
	}
	*count++
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return skillTreeEntry{}, err
	}
	if rel == "." {
		rel = ""
	}
	info, err := os.Lstat(path)
	if err != nil {
		return skillTreeEntry{}, err
	}
	entry := skillTreeEntry{Name: info.Name(), Path: filepath.ToSlash(rel), Type: "file"}
	if rel == "" {
		entry.Name = filepath.Base(root)
		entry.Type = "directory"
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return entry, nil
	}
	if !info.IsDir() {
		return entry, nil
	}
	entry.Type = "directory"
	children, err := os.ReadDir(path)
	if err != nil {
		return entry, err
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].IsDir() != children[j].IsDir() {
			return children[i].IsDir()
		}
		return strings.ToLower(children[i].Name()) < strings.ToLower(children[j].Name())
	})
	for _, child := range children {
		if child.Name() == ".git" || child.Name() == "node_modules" {
			continue
		}
		node, err := buildTreeNode(root, filepath.Join(path, child.Name()), count)
		if err != nil {
			return entry, err
		}
		entry.Children = append(entry.Children, node)
	}
	return entry, nil
}

func safeSkillPath(root, rel string) (string, string, error) {
	clean := filepath.Clean(strings.TrimSpace(rel))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("invalid skill content path %q", rel)
	}
	path := filepath.Join(root, clean)
	resolvedRel, err := filepath.Rel(root, path)
	if err != nil {
		return "", "", err
	}
	if resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("skill content path escapes skill root")
	}
	return filepath.ToSlash(clean), path, nil
}

func readTextPreview(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("skill content path is a directory")
	}
	if info.Size() > maxSkillPreviewBytes {
		return "", fmt.Errorf("skill content exceeds %d bytes", maxSkillPreviewBytes)
	}
	if !allowedTextPreview(path) {
		return "", fmt.Errorf("skill content path is not a supported text file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if strings.ContainsRune(string(body), '\x00') {
		return "", fmt.Errorf("skill content appears to be binary")
	}
	return string(body), nil
}

func allowedTextPreview(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".json", ".yaml", ".yml", ".toml", ".go", ".ts", ".tsx", ".js", ".jsx", ".css":
		return true
	default:
		return false
	}
}

type orchestratorMapResponse struct {
	Sources    []models.SkillSource   `json:"sources"`
	Skills     []orchestratorMapSkill `json:"skills"`
	Agents     []orchestratorMapAgent `json:"agents"`
	Lifecycles []models.Lifecycle     `json:"lifecycles"`
}

type orchestratorMapSkill struct {
	models.Skill
	SourceName     string   `json:"source_name"`
	AssignedAgents []string `json:"assigned_agents"`
}

type orchestratorMapAgent struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Role           string   `json:"role"`
	CLIKind        string   `json:"cli_kind"`
	BasePrompt     string   `json:"base_prompt"`
	AssignedSkills []string `json:"assigned_skills"`
}

func (a *API) orchestratorMap(w http.ResponseWriter, r *http.Request) {
	sources, err := a.store.ListSkillSources(r.Context())
	if err != nil {
		respond(w, nil, err)
		return
	}
	skills, err := a.store.ListInstalledSkills(r.Context(), false)
	if err != nil {
		respond(w, nil, err)
		return
	}
	sourceNames := make(map[string]string, len(sources))
	for _, source := range sources {
		sourceNames[source.ID] = source.Name
	}
	dbAgents, err := a.store.ListAgents(r.Context())
	if err != nil {
		respond(w, nil, err)
		return
	}
	assignedBySkill := map[string][]string{}
	agents := make([]orchestratorMapAgent, 0, len(dbAgents))
	for _, agent := range dbAgents {
		assignedSkills := make([]string, 0, len(agent.Skills))
		for _, skill := range agent.Skills {
			assignedSkills = append(assignedSkills, skill.Name)
			assignedBySkill[skill.ID] = append(assignedBySkill[skill.ID], agent.ID)
		}
		agents = append(agents, orchestratorMapAgent{
			ID:             agent.ID,
			Name:           agent.Name,
			Role:           agent.Role,
			CLIKind:        agent.CLIKind,
			BasePrompt:     agent.RolePrompt,
			AssignedSkills: assignedSkills,
		})
	}
	mapSkills := make([]orchestratorMapSkill, 0, len(skills))
	for _, skill := range skills {
		mapSkills = append(mapSkills, orchestratorMapSkill{
			Skill:          skill,
			SourceName:     sourceNames[skill.SourceID],
			AssignedAgents: uniqueStrings(assignedBySkill[skill.ID]),
		})
	}
	lifecycles, err := a.store.ListLifecycles(r.Context())
	if err != nil {
		respond(w, nil, err)
		return
	}
	respond(w, orchestratorMapResponse{Sources: sources, Skills: mapSkills, Agents: agents, Lifecycles: lifecycles}, nil)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
