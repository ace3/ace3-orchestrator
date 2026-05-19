package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"mini-paperclip/backend/internal/fsutil"
	"mini-paperclip/backend/internal/models"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

type Store struct {
	db          *sqlx.DB
	allowlist   []string
	pathAliases []fsutil.PathAlias
}

func New(db *sqlx.DB, allowlist []string, pathAliases ...fsutil.PathAlias) *Store {
	return &Store{db: db, allowlist: allowlist, pathAliases: pathAliases}
}

func (s *Store) DB() *sqlx.DB {
	return s.db
}

func (s *Store) CountAgents(ctx context.Context) (int, error) {
	var count int
	return count, s.db.GetContext(ctx, &count, "SELECT count(*) FROM agents")
}

func (s *Store) ListAgents(ctx context.Context) ([]models.Agent, error) {
	var agents []models.Agent
	if err := s.db.SelectContext(ctx, &agents, "SELECT * FROM agents ORDER BY role, name"); err != nil {
		return nil, err
	}
	for i := range agents {
		skills, err := s.AgentSkills(ctx, agents[i].ID)
		if err != nil {
			return nil, err
		}
		agents[i].Skills = skills
	}
	return agents, nil
}

func (s *Store) GetAgent(ctx context.Context, id string) (models.Agent, error) {
	var agent models.Agent
	if err := s.db.GetContext(ctx, &agent, "SELECT * FROM agents WHERE id=$1", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent, ErrNotFound
		}
		return agent, err
	}
	skills, err := s.AgentSkills(ctx, id)
	if err != nil {
		return agent, err
	}
	agent.Skills = skills
	return agent, nil
}

type AgentInput struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Role       string   `json:"role"`
	RolePrompt string   `json:"role_prompt"`
	CLIKind    string   `json:"cli_kind"`
	CLIProfile *string  `json:"cli_profile"`
	Enabled    *bool    `json:"enabled"`
	SkillIDs   []string `json:"skill_ids"`
}

func (s *Store) CreateAgent(ctx context.Context, in AgentInput) (models.Agent, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = uuid.NewString()
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if err := validateCLIKind(in.CLIKind); err != nil {
		return models.Agent{}, err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return models.Agent{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agents (id, name, role, role_prompt, cli_kind, cli_profile, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, id, strings.TrimSpace(in.Name), strings.TrimSpace(in.Role), in.RolePrompt, in.CLIKind, in.CLIProfile, enabled); err != nil {
		_ = tx.Rollback()
		return models.Agent{}, err
	}
	if err := replaceAgentSkills(ctx, tx, id, in.SkillIDs); err != nil {
		_ = tx.Rollback()
		return models.Agent{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.Agent{}, err
	}
	return s.GetAgent(ctx, id)
}

func (s *Store) UpdateAgent(ctx context.Context, id string, in AgentInput) (models.Agent, error) {
	agent, err := s.GetAgent(ctx, id)
	if err != nil {
		return agent, err
	}
	if in.Name != "" {
		agent.Name = strings.TrimSpace(in.Name)
	}
	if in.Role != "" {
		agent.Role = strings.TrimSpace(in.Role)
	}
	if in.RolePrompt != "" {
		agent.RolePrompt = in.RolePrompt
	}
	if in.CLIKind != "" {
		if err := validateCLIKind(in.CLIKind); err != nil {
			return agent, err
		}
		agent.CLIKind = in.CLIKind
	}
	if in.Enabled != nil {
		agent.Enabled = *in.Enabled
	}
	agent.CLIProfile = in.CLIProfile
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return agent, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET name=$2, role=$3, role_prompt=$4, cli_kind=$5, cli_profile=$6, enabled=$7, updated_at=now() WHERE id=$1`,
		id, agent.Name, agent.Role, agent.RolePrompt, agent.CLIKind, agent.CLIProfile, agent.Enabled); err != nil {
		_ = tx.Rollback()
		return agent, err
	}
	if in.SkillIDs != nil {
		if err := replaceAgentSkills(ctx, tx, id, in.SkillIDs); err != nil {
			_ = tx.Rollback()
			return agent, err
		}
	}
	if err := tx.Commit(); err != nil {
		return agent, err
	}
	return s.GetAgent(ctx, id)
}

func (s *Store) UpdateAgentRuntime(ctx context.Context, id string, in AgentInput) (models.Agent, error) {
	agent, err := s.GetAgent(ctx, id)
	if err != nil {
		return agent, err
	}
	if in.Enabled != nil {
		agent.Enabled = *in.Enabled
	}
	agent.CLIProfile = in.CLIProfile
	if _, err := s.db.ExecContext(ctx, `UPDATE agents SET cli_profile=$2, enabled=$3, updated_at=now() WHERE id=$1`,
		id, agent.CLIProfile, agent.Enabled); err != nil {
		return agent, err
	}
	return s.GetAgent(ctx, id)
}

func (s *Store) SyncRepoAgent(ctx context.Context, in AgentInput) (models.Agent, error) {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if err := validateCLIKind(in.CLIKind); err != nil {
		return models.Agent{}, err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return models.Agent{}, err
	}
	var exists bool
	if err := tx.GetContext(ctx, &exists, "SELECT EXISTS (SELECT 1 FROM agents WHERE id=$1)", in.ID); err != nil {
		_ = tx.Rollback()
		return models.Agent{}, err
	}
	if exists {
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET name=$2, role=$3, role_prompt=$4, cli_kind=$5, updated_at=now() WHERE id=$1`,
			in.ID, strings.TrimSpace(in.Name), strings.TrimSpace(in.Role), in.RolePrompt, in.CLIKind); err != nil {
			_ = tx.Rollback()
			return models.Agent{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `INSERT INTO agents (id, name, role, role_prompt, cli_kind, cli_profile, enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`, in.ID, strings.TrimSpace(in.Name), strings.TrimSpace(in.Role), in.RolePrompt, in.CLIKind, in.CLIProfile, enabled); err != nil {
			_ = tx.Rollback()
			return models.Agent{}, err
		}
	}
	if err := replaceAgentSkills(ctx, tx, in.ID, in.SkillIDs); err != nil {
		_ = tx.Rollback()
		return models.Agent{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.Agent{}, err
	}
	return s.GetAgent(ctx, in.ID)
}

func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	var exists bool
	if err := s.db.GetContext(ctx, &exists, "SELECT EXISTS (SELECT 1 FROM agents WHERE id=$1)", id); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	var hasTasksTable bool
	if err := s.db.GetContext(ctx, &hasTasksTable, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='tasks')"); err != nil {
		return err
	}
	if !hasTasksTable {
		return ErrConflict
	}
	var openTasks int
	if err := s.db.GetContext(ctx, &openTasks, "SELECT count(*) FROM tasks WHERE assignee_agent_id=$1 AND status NOT IN ('done','cancelled')", id); err != nil {
		return err
	}
	if openTasks > 0 {
		return ErrConflict
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM agents WHERE id=$1", id)
	return err
}

func (s *Store) DuplicateAgent(ctx context.Context, id string) (models.Agent, error) {
	agent, err := s.GetAgent(ctx, id)
	if err != nil {
		return agent, err
	}
	skillIDs := make([]string, 0, len(agent.Skills))
	for _, skill := range agent.Skills {
		skillIDs = append(skillIDs, skill.ID)
	}
	return s.CreateAgent(ctx, AgentInput{
		Name:       agent.Name + " (copy)",
		Role:       agent.Role,
		RolePrompt: agent.RolePrompt,
		CLIKind:    agent.CLIKind,
		CLIProfile: agent.CLIProfile,
		Enabled:    &agent.Enabled,
		SkillIDs:   skillIDs,
	})
}

func (s *Store) SetAgentEnabled(ctx context.Context, id string, enabled bool) (models.Agent, error) {
	if _, err := s.db.ExecContext(ctx, "UPDATE agents SET enabled=$2, updated_at=now() WHERE id=$1", id, enabled); err != nil {
		return models.Agent{}, err
	}
	return s.GetAgent(ctx, id)
}

func (s *Store) AgentSkills(ctx context.Context, agentID string) ([]models.Skill, error) {
	var skills []models.Skill
	return skills, s.db.SelectContext(ctx, &skills, `SELECT s.* FROM skills s JOIN agent_skills a ON a.skill_id=s.id WHERE a.agent_id=$1 ORDER BY s.name`, agentID)
}

func (s *Store) SkillsByID(ctx context.Context, ids []string) ([]models.Skill, error) {
	skills := make([]models.Skill, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		var skill models.Skill
		if err := s.db.GetContext(ctx, &skill, "SELECT * FROM skills WHERE id=$1 AND archived=false AND ignored=false", id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

func (s *Store) SkillsByName(ctx context.Context, names []string) ([]models.Skill, error) {
	skills := make([]models.Skill, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var skill models.Skill
		if err := s.db.GetContext(ctx, &skill, "SELECT * FROM skills WHERE name=$1 AND archived=false AND ignored=false ORDER BY source_id, path_in_source LIMIT 1", name); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("repo-defined skill %q is not installed", name)
			}
			return nil, err
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

func replaceAgentSkills(ctx context.Context, tx *sqlx.Tx, agentID string, skillIDs []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM agent_skills WHERE agent_id=$1", agentID); err != nil {
		return err
	}
	for _, skillID := range skillIDs {
		if strings.TrimSpace(skillID) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO agent_skills (agent_id, skill_id) VALUES ($1,$2) ON CONFLICT DO NOTHING", agentID, skillID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListSkillSources(ctx context.Context) ([]models.SkillSource, error) {
	var sources []models.SkillSource
	return sources, s.db.SelectContext(ctx, &sources, "SELECT * FROM skill_sources ORDER BY name")
}

func (s *Store) ListInstalledSkills(ctx context.Context, includeIgnored bool) ([]models.Skill, error) {
	var skills []models.Skill
	query := `SELECT s.* FROM skills s JOIN skill_sources ss ON ss.id=s.source_id WHERE s.archived=false`
	if !includeIgnored {
		query += ` AND s.ignored=false`
	}
	query += ` ORDER BY ss.name, s.name`
	return skills, s.db.SelectContext(ctx, &skills, query)
}

func (s *Store) GetSkill(ctx context.Context, id string) (models.Skill, error) {
	var skill models.Skill
	if err := s.db.GetContext(ctx, &skill, "SELECT * FROM skills WHERE id=$1 AND archived=false", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return skill, ErrNotFound
		}
		return skill, err
	}
	return skill, nil
}

func (s *Store) GetSkillSource(ctx context.Context, id string) (models.SkillSource, error) {
	var source models.SkillSource
	if err := s.db.GetContext(ctx, &source, "SELECT * FROM skill_sources WHERE id=$1", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return source, ErrNotFound
		}
		return source, err
	}
	return source, nil
}

type SkillSourceInput struct {
	Name        string `json:"name"`
	UpstreamURL string `json:"upstream_url"`
	PinnedSHA   string `json:"pinned_sha"`
	PathFilter  string `json:"path_filter"`
	Kind        string `json:"kind"`
}

func (s *Store) CreateSkillSource(ctx context.Context, in SkillSourceInput) (models.SkillSource, error) {
	source := models.SkillSource{
		ID:          uuid.NewString(),
		Name:        strings.TrimSpace(in.Name),
		UpstreamURL: strings.TrimSpace(in.UpstreamURL),
		PinnedSHA:   strings.TrimSpace(in.PinnedSHA),
		PathFilter:  strings.TrimSpace(in.PathFilter),
		Kind:        strings.TrimSpace(in.Kind),
	}
	if source.PinnedSHA == "" {
		source.PinnedSHA = "main"
	}
	if source.Kind == "" {
		source.Kind = "custom"
	}
	if source.Name == "" || source.UpstreamURL == "" {
		return models.SkillSource{}, fmt.Errorf("name and upstream_url are required")
	}
	if err := validateSkillSourceKind(source.Kind); err != nil {
		return models.SkillSource{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO skill_sources (id, name, upstream_url, pinned_sha, path_filter, kind)
		VALUES ($1,$2,$3,$4,$5,$6)`, source.ID, source.Name, source.UpstreamURL, source.PinnedSHA, source.PathFilter, source.Kind); err != nil {
		return models.SkillSource{}, err
	}
	return s.GetSkillSource(ctx, source.ID)
}

func (s *Store) UpsertSkillSource(ctx context.Context, source models.SkillSource) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO skill_sources (id, name, upstream_url, pinned_sha, path_filter, kind)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (name) DO UPDATE SET upstream_url=excluded.upstream_url, pinned_sha=excluded.pinned_sha, path_filter=excluded.path_filter, kind=excluded.kind, updated_at=now()`,
		source.ID, source.Name, source.UpstreamURL, source.PinnedSHA, source.PathFilter, source.Kind)
	return err
}

func (s *Store) PinSkillSource(ctx context.Context, id, sha string) (models.SkillSource, error) {
	if _, err := s.db.ExecContext(ctx, "UPDATE skill_sources SET pinned_sha=$2, has_update=false, updated_at=now() WHERE id=$1", id, strings.TrimSpace(sha)); err != nil {
		return models.SkillSource{}, err
	}
	return s.GetSkillSource(ctx, id)
}

func (s *Store) DeleteSkillSource(ctx context.Context, id string) error {
	var exists bool
	if err := s.db.GetContext(ctx, &exists, "SELECT EXISTS (SELECT 1 FROM skill_sources WHERE id=$1)", id); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	var assigned int
	if err := s.db.GetContext(ctx, &assigned, `SELECT count(*)
		FROM agent_skills a
		JOIN skills sk ON sk.id=a.skill_id
		WHERE sk.source_id=$1 AND sk.archived=false`, id); err != nil {
		return err
	}
	if assigned > 0 {
		return ErrConflict
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM skill_sources WHERE id=$1", id)
	return err
}

func (s *Store) SetSkillSourceUpdate(ctx context.Context, id string, hasUpdate bool) error {
	_, err := s.db.ExecContext(ctx, "UPDATE skill_sources SET has_update=$2, updated_at=now() WHERE id=$1", id, hasUpdate)
	if err == nil {
		s.Notify(ctx, "skill_source", id)
	}
	return err
}

func (s *Store) SetSkillIgnored(ctx context.Context, id string, ignored bool) (models.Skill, error) {
	if ignored {
		var assigned int
		if err := s.db.GetContext(ctx, &assigned, "SELECT count(*) FROM agent_skills WHERE skill_id=$1", id); err != nil {
			return models.Skill{}, err
		}
		if assigned > 0 {
			return models.Skill{}, ErrConflict
		}
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE skills SET ignored=$2, updated_at=now() WHERE id=$1 AND archived=false", id, ignored); err != nil {
		return models.Skill{}, err
	}
	s.Notify(ctx, "skill", id)
	return s.GetSkill(ctx, id)
}

func (s *Store) UpsertSkillsForSource(ctx context.Context, sourceID string, skills []models.Skill) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE skills SET archived=true, updated_at=now() WHERE source_id=$1", sourceID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, skill := range skills {
		if _, err := tx.ExecContext(ctx, `INSERT INTO skills (id, source_id, name, path_in_source, version, archived, ignored)
			VALUES ($1,$2,$3,$4,$5,false,false)
			ON CONFLICT (source_id, path_in_source) DO UPDATE SET name=excluded.name, version=excluded.version, archived=false, updated_at=now()`,
			skill.ID, sourceID, skill.Name, skill.PathInSource, skill.Version); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE skill_sources SET last_synced_at=now(), updated_at=now() WHERE id=$1", sourceID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) ActiveSkillCountsBySource(ctx context.Context) (map[string]int, error) {
	type row struct {
		SourceID string `db:"source_id"`
		Count    int    `db:"count"`
	}
	var rows []row
	if err := s.db.SelectContext(ctx, &rows, "SELECT source_id, count(*) AS count FROM skills WHERE archived=false GROUP BY source_id"); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.SourceID] = row.Count
	}
	return out, nil
}

type ProjectInput struct {
	Name                  string `json:"name"`
	Description           string `json:"description"`
	DefaultCLIKind        string `json:"default_cli_kind"`
	DefaultBranchStrategy string `json:"default_branch_strategy"`
}

func (s *Store) ListProjects(ctx context.Context) ([]models.Project, error) {
	var projects []models.Project
	if err := s.db.SelectContext(ctx, &projects, "SELECT * FROM projects ORDER BY updated_at DESC"); err != nil {
		return nil, err
	}
	for i := range projects {
		repos, err := s.ListRepos(ctx, projects[i].ID)
		if err != nil {
			return nil, err
		}
		projects[i].Repos = repos
	}
	return projects, nil
}

func (s *Store) GetProject(ctx context.Context, id string) (models.Project, error) {
	var project models.Project
	if err := s.db.GetContext(ctx, &project, "SELECT * FROM projects WHERE id=$1", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return project, ErrNotFound
		}
		return project, err
	}
	repos, err := s.ListRepos(ctx, id)
	if err != nil {
		return project, err
	}
	project.Repos = repos
	return project, nil
}

func (s *Store) CreateProject(ctx context.Context, in ProjectInput) (models.Project, error) {
	if err := validateCLIKind(in.DefaultCLIKind); err != nil {
		return models.Project{}, err
	}
	if in.DefaultBranchStrategy == "" {
		in.DefaultBranchStrategy = "worktree-per-run"
	}
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO projects (id, name, description, default_cli_kind, default_branch_strategy)
		VALUES ($1,$2,$3,$4,$5)`, id, strings.TrimSpace(in.Name), in.Description, in.DefaultCLIKind, in.DefaultBranchStrategy); err != nil {
		return models.Project{}, err
	}
	return s.GetProject(ctx, id)
}

func (s *Store) UpdateProject(ctx context.Context, id string, in ProjectInput) (models.Project, error) {
	project, err := s.GetProject(ctx, id)
	if err != nil {
		return project, err
	}
	if in.Name != "" {
		project.Name = strings.TrimSpace(in.Name)
	}
	project.Description = in.Description
	if in.DefaultCLIKind != "" {
		if err := validateCLIKind(in.DefaultCLIKind); err != nil {
			return project, err
		}
		project.DefaultCLIKind = in.DefaultCLIKind
	}
	if in.DefaultBranchStrategy != "" {
		project.DefaultBranchStrategy = in.DefaultBranchStrategy
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE projects SET name=$2, description=$3, default_cli_kind=$4, default_branch_strategy=$5, updated_at=now() WHERE id=$1`,
		id, project.Name, project.Description, project.DefaultCLIKind, project.DefaultBranchStrategy); err != nil {
		return project, err
	}
	return s.GetProject(ctx, id)
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM projects WHERE id=$1", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

type RepoInput struct {
	LocalPath     string `json:"local_path"`
	DefaultBranch string `json:"default_branch"`
}

func (s *Store) ListRepos(ctx context.Context, projectID string) ([]models.Repo, error) {
	var repos []models.Repo
	return repos, s.db.SelectContext(ctx, &repos, "SELECT * FROM repos WHERE project_id=$1 ORDER BY local_path", projectID)
}

func (s *Store) CreateRepo(ctx context.Context, projectID string, in RepoInput) (models.Repo, error) {
	clean, err := fsutil.CleanUnderAllowlistWithAliases(in.LocalPath, s.allowlist, s.pathAliases)
	if err != nil {
		return models.Repo{}, err
	}
	if err := exec.CommandContext(ctx, "git", "-C", clean, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return models.Repo{}, fmt.Errorf("path is not a git repository: %w", err)
	}
	branch := strings.TrimSpace(in.DefaultBranch)
	if branch == "" {
		branchBytes, err := exec.CommandContext(ctx, "git", "-C", clean, "branch", "--show-current").Output()
		if err == nil {
			branch = strings.TrimSpace(string(branchBytes))
		}
	}
	if branch == "" {
		branch = "main"
	}
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO repos (id, project_id, local_path, default_branch, status)
		VALUES ($1,$2,$3,$4,'ok')`, id, projectID, clean, branch); err != nil {
		return models.Repo{}, err
	}
	var repo models.Repo
	return repo, s.db.GetContext(ctx, &repo, "SELECT * FROM repos WHERE id=$1", id)
}

func (s *Store) DeleteRepo(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM repos WHERE id=$1", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func validateCLIKind(kind string) error {
	if kind == "claude" || kind == "codex" {
		return nil
	}
	return fmt.Errorf("cli_kind must be claude or codex")
}

func validateSkillSourceKind(kind string) error {
	if kind == "verzth" || kind == "ace3" || kind == "custom" {
		return nil
	}
	return fmt.Errorf("kind must be verzth, ace3, or custom")
}
