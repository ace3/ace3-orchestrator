package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"mini-paperclip/backend/internal/models"
	"mini-paperclip/backend/internal/repoconfig"
)

type SkillDriftReport struct {
	OK        bool               `json:"ok"`
	CheckedAt time.Time          `json:"checked_at"`
	CacheDir  string             `json:"cache_dir"`
	Sources   []SourceDriftState `json:"sources"`
	Issues    []SkillDriftIssue  `json:"issues"`
}

type SourceDriftState struct {
	SourceID       string `json:"source_id"`
	SourceName     string `json:"source_name"`
	PinnedSHA      string `json:"pinned_sha"`
	CachePath      string `json:"cache_path"`
	CachePresent   bool   `json:"cache_present"`
	DBSkillCount   int    `json:"db_skill_count"`
	FileSkillCount int    `json:"file_skill_count"`
}

type SkillDriftIssue struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	SourceID    string `json:"source_id,omitempty"`
	SourceName  string `json:"source_name,omitempty"`
	SkillID     string `json:"skill_id,omitempty"`
	SkillName   string `json:"skill_name,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	LifecycleID string `json:"lifecycle_id,omitempty"`
	Path        string `json:"path,omitempty"`
	Message     string `json:"message"`
}

type assignedSkillState struct {
	AgentID      string `db:"agent_id"`
	SkillID      string `db:"skill_id"`
	SkillName    string `db:"skill_name"`
	Archived     bool   `db:"archived"`
	Ignored      bool   `db:"ignored"`
	SourceID     string `db:"source_id"`
	SourceName   string `db:"source_name"`
	PathInSource string `db:"path_in_source"`
}

func (s *Service) CheckSkillDrift(ctx context.Context) (SkillDriftReport, error) {
	report := SkillDriftReport{
		OK:        true,
		CheckedAt: time.Now().UTC(),
		CacheDir:  s.skillsCacheDir,
	}

	sources, err := s.store.ListSkillSources(ctx)
	if err != nil {
		return report, err
	}
	skills, err := s.store.ListInstalledSkills(ctx, true)
	if err != nil {
		return report, err
	}

	sourcesByID := make(map[string]models.SkillSource, len(sources))
	for _, source := range sources {
		sourcesByID[source.ID] = source
	}
	dbSkillsBySource := make(map[string][]models.Skill)
	dbSkillsByName := make(map[string]models.Skill, len(skills))
	for _, skill := range skills {
		dbSkillsBySource[skill.SourceID] = append(dbSkillsBySource[skill.SourceID], skill)
		if _, exists := dbSkillsByName[skill.Name]; !exists {
			dbSkillsByName[skill.Name] = skill
		}
	}

	for _, source := range sources {
		target := s.sourceCachePath(source)
		state := SourceDriftState{
			SourceID:     source.ID,
			SourceName:   source.Name,
			PinnedSHA:    source.PinnedSHA,
			CachePath:    target,
			DBSkillCount: len(dbSkillsBySource[source.ID]),
		}
		if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
			state.CachePresent = false
			report.addIssue(SkillDriftIssue{
				Code:       "source_cache_missing",
				Severity:   "error",
				SourceID:   source.ID,
				SourceName: source.Name,
				Path:       target,
				Message:    fmt.Sprintf("cached checkout is missing for source %q at pinned ref %q", source.Name, source.PinnedSHA),
			})
			report.Sources = append(report.Sources, state)
			continue
		}
		state.CachePresent = true
		discovered, err := discoverSkills(target, source.ID, source.PathFilter)
		if err != nil {
			report.addIssue(SkillDriftIssue{
				Code:       "source_cache_unreadable",
				Severity:   "error",
				SourceID:   source.ID,
				SourceName: source.Name,
				Path:       target,
				Message:    err.Error(),
			})
			report.Sources = append(report.Sources, state)
			continue
		}
		state.FileSkillCount = len(discovered)
		dbByPath := make(map[string]models.Skill)
		for _, skill := range dbSkillsBySource[source.ID] {
			dbByPath[filepath.ToSlash(filepath.Clean(skill.PathInSource))] = skill
			if _, err := os.Stat(filepath.Join(target, skill.PathInSource)); err != nil {
				report.addIssue(SkillDriftIssue{
					Code:       "db_skill_file_missing",
					Severity:   "error",
					SourceID:   source.ID,
					SourceName: source.Name,
					SkillID:    skill.ID,
					SkillName:  skill.Name,
					Path:       skill.PathInSource,
					Message:    fmt.Sprintf("DB skill %q points to a missing cached file", skill.Name),
				})
			}
		}
		for _, skill := range discovered {
			clean := filepath.ToSlash(filepath.Clean(skill.PathInSource))
			if _, ok := dbByPath[clean]; !ok {
				report.addIssue(SkillDriftIssue{
					Code:       "cache_skill_missing_db",
					Severity:   "error",
					SourceID:   source.ID,
					SourceName: source.Name,
					SkillName:  skill.Name,
					Path:       skill.PathInSource,
					Message:    fmt.Sprintf("cached skill %q is not reflected in DB metadata", skill.Name),
				})
			}
		}
		report.Sources = append(report.Sources, state)
	}

	for _, skill := range skills {
		if _, ok := sourcesByID[skill.SourceID]; !ok {
			report.addIssue(SkillDriftIssue{
				Code:      "db_skill_source_missing",
				Severity:  "error",
				SkillID:   skill.ID,
				SkillName: skill.Name,
				Path:      skill.PathInSource,
				Message:   fmt.Sprintf("DB skill %q references a missing source", skill.Name),
			})
		}
	}

	if cfg, err := repoconfig.Load(); err == nil {
		for _, skill := range cfg.Skills {
			if _, ok := dbSkillsByName[skill.Name]; !ok {
				report.addIssue(SkillDriftIssue{
					Code:      "repo_skill_missing_db",
					Severity:  "error",
					SkillName: skill.Name,
					Path:      skill.PathInSource,
					Message:   fmt.Sprintf("repo-defined skill %q is not installed in DB metadata", skill.Name),
				})
			}
		}
		for _, agent := range cfg.Agents {
			for _, skillName := range agent.Skills {
				if _, ok := dbSkillsByName[skillName]; !ok {
					report.addIssue(SkillDriftIssue{
						Code:      "repo_agent_skill_missing_db",
						Severity:  "error",
						AgentID:   agent.ID,
						SkillName: skillName,
						Message:   fmt.Sprintf("repo-defined agent %q requires missing skill %q", agent.ID, skillName),
					})
				}
			}
		}
	} else {
		report.addIssue(SkillDriftIssue{
			Code:     "repo_config_unreadable",
			Severity: "error",
			Message:  err.Error(),
		})
	}

	if err := s.addAssignmentDrift(ctx, &report); err != nil {
		return report, err
	}
	if err := s.addLifecycleDrift(ctx, &report); err != nil {
		return report, err
	}

	sort.Slice(report.Issues, func(i, j int) bool {
		if report.Issues[i].Code != report.Issues[j].Code {
			return report.Issues[i].Code < report.Issues[j].Code
		}
		return report.Issues[i].Message < report.Issues[j].Message
	})
	sort.Slice(report.Sources, func(i, j int) bool {
		return report.Sources[i].SourceName < report.Sources[j].SourceName
	})
	report.OK = len(report.Issues) == 0
	return report, nil
}

func (s *Service) addLifecycleDrift(ctx context.Context, report *SkillDriftReport) error {
	lifecycles, err := s.store.ListLifecycles(ctx)
	if err != nil {
		return err
	}
	var agents []struct {
		ID      string `db:"id"`
		Enabled bool   `db:"enabled"`
	}
	if err := s.store.DB().SelectContext(ctx, &agents, "SELECT id, enabled FROM agents"); err != nil {
		return err
	}
	enabledAgents := make(map[string]bool, len(agents))
	for _, agent := range agents {
		enabledAgents[agent.ID] = agent.Enabled
	}
	for _, lifecycle := range lifecycles {
		for _, step := range lifecycle.Steps {
			if enabledAgents[step.AgentID] {
				continue
			}
			report.addIssue(SkillDriftIssue{
				Code:        "lifecycle_agent_not_enabled",
				Severity:    "error",
				AgentID:     step.AgentID,
				LifecycleID: lifecycle.ID,
				Message:     fmt.Sprintf("lifecycle %q references missing or disabled agent %q", lifecycle.ID, step.AgentID),
			})
		}
	}
	return nil
}

func (s *Service) sourceCachePath(source models.SkillSource) string {
	return filepath.Join(s.skillsCacheDir, source.Name, source.PinnedSHA)
}

func (s *Service) addAssignmentDrift(ctx context.Context, report *SkillDriftReport) error {
	var assigned []assignedSkillState
	if err := s.store.DB().SelectContext(ctx, &assigned, `SELECT a.agent_id, sk.id AS skill_id, sk.name AS skill_name, sk.archived, sk.ignored, sk.source_id, ss.name AS source_name, sk.path_in_source
		FROM agent_skills a
		JOIN skills sk ON sk.id=a.skill_id
		LEFT JOIN skill_sources ss ON ss.id=sk.source_id
		WHERE sk.archived=true OR sk.ignored=true`); err != nil {
		return err
	}
	for _, item := range assigned {
		code := "agent_skill_not_active"
		reason := "archived"
		if item.Ignored {
			reason = "ignored"
		}
		report.addIssue(SkillDriftIssue{
			Code:       code,
			Severity:   "error",
			SourceID:   item.SourceID,
			SourceName: item.SourceName,
			SkillID:    item.SkillID,
			SkillName:  item.SkillName,
			AgentID:    item.AgentID,
			Path:       item.PathInSource,
			Message:    fmt.Sprintf("agent %q is assigned to %s skill %q", item.AgentID, reason, item.SkillName),
		})
	}
	return nil
}

func (r *SkillDriftReport) addIssue(issue SkillDriftIssue) {
	r.Issues = append(r.Issues, issue)
	r.OK = false
}
