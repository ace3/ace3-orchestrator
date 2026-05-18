package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"mini-paperclip/backend/internal/agentdefs"
	"mini-paperclip/backend/internal/models"
	"mini-paperclip/backend/internal/repoconfig"
	"mini-paperclip/backend/internal/store"
)

type Service struct {
	store          *store.Store
	skillsCacheDir string
}

func New(store *store.Store, skillsCacheDir string) *Service {
	return &Service{store: store, skillsCacheDir: skillsCacheDir}
}

type Status struct {
	Bootstrapped bool `json:"bootstrapped"`
	AgentsCount  int  `json:"agents_count"`
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	count, err := s.store.CountAgents(ctx)
	return Status{Bootstrapped: count > 0, AgentsCount: count}, err
}

func (s *Service) Run(ctx context.Context) (Status, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return status, err
	}
	cfg, err := repoconfig.Load()
	if err != nil {
		return status, err
	}
	if err := s.upsertSkillSourcesFromConfig(ctx, cfg); err != nil {
		return status, err
	}
	sources, err := s.store.ListSkillSources(ctx)
	if err != nil {
		return status, err
	}
	for _, source := range sources {
		if err := s.SyncSource(ctx, source.ID); err != nil {
			slog.Warn("skill source sync failed; continuing with JSON catalog", "source", source.Name, "error", err)
		}
	}
	if err := s.upsertMissingSkillsFromConfig(ctx, cfg); err != nil {
		return status, err
	}
	skillsByName, err := s.skillsByName(ctx)
	if err != nil {
		return status, err
	}
	if err := s.SyncAgentDefinitions(ctx, skillsByName); err != nil {
		return status, err
	}
	return s.Status(ctx)
}

func (s *Service) upsertSkillSourcesFromConfig(ctx context.Context, cfg *repoconfig.Config) error {
	existing, err := s.store.ListSkillSources(ctx)
	if err != nil {
		return err
	}
	byName := make(map[string]models.SkillSource, len(existing))
	for _, src := range existing {
		byName[src.Name] = src
	}
	for _, src := range cfg.SkillSources {
		if byName[src.Name].ID != "" {
			continue
		}
		if err := s.store.UpsertSkillSource(ctx, models.SkillSource{
			ID:          uuid.NewString(),
			Name:        src.Name,
			UpstreamURL: src.UpstreamURL,
			PinnedSHA:   src.PinnedSHA,
			Kind:        src.Kind,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) upsertMissingSkillsFromConfig(ctx context.Context, cfg *repoconfig.Config) error {
	sources, err := s.store.ListSkillSources(ctx)
	if err != nil {
		return err
	}
	sourceIDByName := make(map[string]string, len(sources))
	for _, src := range sources {
		sourceIDByName[src.Name] = src.ID
	}
	counts, err := s.store.ActiveSkillCountsBySource(ctx)
	if err != nil {
		return err
	}
	bySource := make(map[string][]models.Skill)
	existing, err := s.allSkillsByName(ctx)
	if err != nil {
		return err
	}
	for _, sk := range cfg.Skills {
		sourceID := sourceIDByName[sk.Source]
		if sourceID == "" {
			return fmt.Errorf("skills.json: skill %q references source %q which is not loaded", sk.Name, sk.Source)
		}
		if counts[sourceID] > 0 {
			continue
		}
		id := existing[sk.Name].ID
		if id == "" {
			id = uuid.NewString()
		}
		bySource[sourceID] = append(bySource[sourceID], models.Skill{
			ID:           id,
			SourceID:     sourceID,
			Name:         sk.Name,
			PathInSource: sk.PathInSource,
			Version:      sk.Version,
			Archived:     sk.Archived,
		})
	}
	for sourceID, skills := range bySource {
		if err := s.store.UpsertSkillsForSource(ctx, sourceID, skills); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) allSkillsByName(ctx context.Context) (map[string]models.Skill, error) {
	var skills []models.Skill
	if err := s.store.DB().SelectContext(ctx, &skills, "SELECT * FROM skills"); err != nil {
		return nil, err
	}
	out := make(map[string]models.Skill, len(skills))
	for _, sk := range skills {
		out[sk.Name] = sk
	}
	return out, nil
}

func (s *Service) SyncAgentDefinitions(ctx context.Context, skillsByName map[string]models.Skill) error {
	defs, err := agentdefs.Load()
	if err != nil {
		return err
	}
	if skillsByName == nil {
		skillsByName, err = s.skillsByName(ctx)
		if err != nil {
			return err
		}
	}
	for _, def := range defs {
		enabled := true
		skillIDs := make([]string, 0, len(def.Skills))
		for _, name := range def.Skills {
			skill, ok := skillsByName[name]
			if !ok {
				return fmt.Errorf("agent %q requires missing skill %q", def.ID, name)
			}
			skillIDs = append(skillIDs, skill.ID)
		}
		if _, err := s.store.GetAgent(ctx, def.ID); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if _, err := s.store.SyncRepoAgent(ctx, store.AgentInput{
			ID:         def.ID,
			Name:       def.Name,
			Role:       def.Role,
			RolePrompt: def.BasePrompt,
			CLIKind:    def.CLIKind,
			Enabled:    &enabled,
			SkillIDs:   skillIDs,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SyncSource(ctx context.Context, id string) error {
	source, err := s.store.GetSkillSource(ctx, id)
	if err != nil {
		return err
	}
	target := filepath.Join(s.skillsCacheDir, source.Name, source.PinnedSHA)
	if _, err := os.Stat(filepath.Join(target, ".git")); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		tmp := target + ".tmp"
		_ = os.RemoveAll(tmp)
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			return err
		}
		if err := exec.CommandContext(ctx, "git", "-C", tmp, "init").Run(); err != nil {
			return fmt.Errorf("init %s: %w", tmp, err)
		}
		if err := exec.CommandContext(ctx, "git", "-C", tmp, "remote", "add", "origin", source.UpstreamURL).Run(); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("remote add %s: %w", source.UpstreamURL, err)
		}
		if err := exec.CommandContext(ctx, "git", "-C", tmp, "fetch", "--depth", "1", "origin", source.PinnedSHA).Run(); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("fetch %s@%s: %w", source.UpstreamURL, source.PinnedSHA, err)
		}
		if err := exec.CommandContext(ctx, "git", "-C", tmp, "checkout", "--detach", "FETCH_HEAD").Run(); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("checkout %s: %w", source.PinnedSHA, err)
		}
		_ = os.RemoveAll(target)
		if err := os.Rename(tmp, target); err != nil {
			return err
		}
	}
	skills, err := discoverSkills(target, source.ID)
	if err != nil {
		return err
	}
	return s.store.UpsertSkillsForSource(ctx, source.ID, skills)
}

func (s *Service) PinSource(ctx context.Context, id, sha string) (models.SkillSource, error) {
	source, err := s.store.PinSkillSource(ctx, id, sha)
	if err != nil {
		return source, err
	}
	return source, s.SyncSource(ctx, id)
}

func (s *Service) StartUpdatePoller(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			if err := s.CheckUpdates(ctx); err != nil {
				slog.Warn("skill source update check failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Service) CheckUpdates(ctx context.Context) error {
	sources, err := s.store.ListSkillSources(ctx)
	if err != nil {
		return err
	}
	for _, source := range sources {
		head, err := remoteHead(ctx, source.UpstreamURL)
		if err != nil {
			return err
		}
		hasUpdate := isPinnedSHA(source.PinnedSHA) && head != source.PinnedSHA
		if err := s.store.SetSkillSourceUpdate(ctx, source.ID, hasUpdate); err != nil {
			return err
		}
	}
	return nil
}

func remoteHead(ctx context.Context, url string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "ls-remote", url, "HEAD").Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty ls-remote response for %s", url)
	}
	return fields[0], nil
}

func isPinnedSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func discoverSkills(root, sourceID string) ([]models.Skill, error) {
	var skills []models.Skill
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.Base(filepath.Dir(path))
		skills = append(skills, models.Skill{
			ID:           uuid.NewString(),
			SourceID:     sourceID,
			Name:         name,
			PathInSource: rel,
			Version:      "",
		})
		return nil
	})
	return skills, err
}

func (s *Service) skillsByName(ctx context.Context) (map[string]models.Skill, error) {
	var skills []models.Skill
	if err := s.store.DB().SelectContext(ctx, &skills, "SELECT * FROM skills WHERE archived=false"); err != nil {
		return nil, err
	}
	out := make(map[string]models.Skill, len(skills))
	for _, skill := range skills {
		out[skill.Name] = skill
	}
	return out, nil
}
