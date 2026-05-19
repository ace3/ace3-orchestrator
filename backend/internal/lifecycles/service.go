package lifecycles

import (
	"context"
	"errors"
	"sort"
	"strings"

	"mini-paperclip/backend/internal/models"
	"mini-paperclip/backend/internal/repoconfig"
	"mini-paperclip/backend/internal/store"
)

const DefaultModelSetting = "default_model"

type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) Exists(ctx context.Context, lifecycleID string) (bool, error) {
	if strings.TrimSpace(lifecycleID) == "" {
		lifecycleID = repoconfig.DefaultLifecycleID
	}
	if _, err := s.store.GetLifecycle(ctx, lifecycleID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Service) NextAgent(ctx context.Context, lifecycleID, currentAgent string, taskTags []string) (next string, done bool, err error) {
	lifecycle, err := s.lifecycle(ctx, lifecycleID)
	if err != nil {
		return "", false, err
	}
	startIdx := 0
	currentAgent = strings.TrimSpace(currentAgent)
	if currentAgent != "" {
		found := false
		for i, step := range lifecycle.Steps {
			if step.AgentID == currentAgent {
				startIdx = i + 1
				found = true
				break
			}
		}
		if !found {
			startIdx = 0
		}
	}
	tagSet := tagSet(taskTags)
	for i := startIdx; i < len(lifecycle.Steps); i++ {
		if runnable(lifecycle.Steps[i], tagSet) {
			return lifecycle.Steps[i].AgentID, false, nil
		}
	}
	return "", true, nil
}

func (s *Service) RemainingSteps(ctx context.Context, lifecycleID, currentAgent string, taskTags []string) ([]models.LifecycleStep, error) {
	lifecycle, err := s.lifecycle(ctx, lifecycleID)
	if err != nil {
		return nil, err
	}
	startIdx := 0
	currentAgent = strings.TrimSpace(currentAgent)
	if currentAgent != "" {
		for i, step := range lifecycle.Steps {
			if step.AgentID == currentAgent {
				startIdx = i + 1
				break
			}
		}
	}
	tags := tagSet(taskTags)
	out := make([]models.LifecycleStep, 0, len(lifecycle.Steps)-startIdx)
	for i := startIdx; i < len(lifecycle.Steps); i++ {
		if runnable(lifecycle.Steps[i], tags) {
			out = append(out, lifecycle.Steps[i])
		}
	}
	return out, nil
}

func (s *Service) ModelForStep(ctx context.Context, lifecycleID, agentID string) (string, error) {
	lifecycle, err := s.lifecycle(ctx, lifecycleID)
	if err != nil {
		return "", err
	}
	for _, step := range lifecycle.Steps {
		if step.AgentID == agentID && strings.TrimSpace(step.ModelID) != "" {
			return strings.TrimSpace(step.ModelID), nil
		}
	}
	return s.defaultModel(ctx)
}

func (s *Service) CLIKindForStep(ctx context.Context, lifecycleID, agentID string) (string, error) {
	lifecycle, err := s.lifecycle(ctx, lifecycleID)
	if err != nil {
		return "", err
	}
	for _, step := range lifecycle.Steps {
		if step.AgentID == agentID && strings.TrimSpace(step.CLIKind) != "" {
			return strings.TrimSpace(step.CLIKind), nil
		}
	}
	return "", nil
}

func (s *Service) TagVocabulary(ctx context.Context, lifecycleID string) ([]string, error) {
	lifecycle, err := s.lifecycle(ctx, lifecycleID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, step := range lifecycle.Steps {
		for _, tag := range append([]string(step.SkipWhen), []string(step.IncludeWhen)...) {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
		}
	}
	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Service) DefaultModel(ctx context.Context) (string, error) {
	return s.defaultModel(ctx)
}

func (s *Service) lifecycle(ctx context.Context, lifecycleID string) (models.Lifecycle, error) {
	if strings.TrimSpace(lifecycleID) == "" {
		lifecycleID = repoconfig.DefaultLifecycleID
	}
	return s.store.GetLifecycle(ctx, lifecycleID)
}

func (s *Service) defaultModel(ctx context.Context) (string, error) {
	value, err := s.store.GetSetting(ctx, DefaultModelSetting)
	if errors.Is(err, store.ErrNotFound) {
		return "claude-sonnet-4-6", nil
	}
	return value, err
}

func runnable(step models.LifecycleStep, tags map[string]bool) bool {
	return !shouldSkip([]string(step.SkipWhen), tags) && shouldInclude([]string(step.IncludeWhen), tags)
}

func shouldSkip(skipWhen []string, tags map[string]bool) bool {
	for _, token := range skipWhen {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == repoconfig.SkipAlways {
			return true
		}
		if tags[token] {
			return true
		}
	}
	return false
}

func shouldInclude(includeWhen []string, tags map[string]bool) bool {
	if len(includeWhen) == 0 {
		return true
	}
	for _, token := range includeWhen {
		if tags[strings.ToLower(strings.TrimSpace(token))] {
			return true
		}
	}
	return false
}

func tagSet(taskTags []string) map[string]bool {
	out := make(map[string]bool, len(taskTags))
	for _, tag := range taskTags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" {
			out[tag] = true
		}
	}
	return out
}
