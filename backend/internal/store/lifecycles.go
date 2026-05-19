package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"mini-paperclip/backend/internal/models"
)

type LifecycleInput struct {
	ID          string               `json:"id"`
	Description string               `json:"description"`
	IsDefault   bool                 `json:"is_default"`
	Steps       []LifecycleStepInput `json:"steps"`
}

type LifecycleStepInput struct {
	ID          string   `json:"id"`
	AgentID     string   `json:"agent_id"`
	CLIKind     string   `json:"cli_kind"`
	SkipWhen    []string `json:"skip_when"`
	IncludeWhen []string `json:"include_when"`
	ModelID     string   `json:"model_id"`
}

func (s *Store) CountLifecycles(ctx context.Context) (int, error) {
	var count int
	return count, s.db.GetContext(ctx, &count, "SELECT count(*) FROM lifecycles")
}

func (s *Store) ListLifecycles(ctx context.Context) ([]models.Lifecycle, error) {
	lifecycles := []models.Lifecycle{}
	if err := s.db.SelectContext(ctx, &lifecycles, "SELECT * FROM lifecycles ORDER BY is_default DESC, id"); err != nil {
		return nil, err
	}
	if err := s.attachLifecycleSteps(ctx, lifecycles); err != nil {
		return nil, err
	}
	return lifecycles, nil
}

func (s *Store) GetLifecycle(ctx context.Context, id string) (models.Lifecycle, error) {
	var lifecycle models.Lifecycle
	if err := s.db.GetContext(ctx, &lifecycle, "SELECT * FROM lifecycles WHERE id=$1", strings.TrimSpace(id)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return lifecycle, ErrNotFound
		}
		return lifecycle, err
	}
	steps, err := s.lifecycleSteps(ctx, lifecycle.ID)
	if err != nil {
		return lifecycle, err
	}
	lifecycle.Steps = steps
	return lifecycle, nil
}

func (s *Store) GetLifecycleBySlug(ctx context.Context, id string) (models.Lifecycle, error) {
	return s.GetLifecycle(ctx, id)
}

func (s *Store) CreateLifecycle(ctx context.Context, in LifecycleInput) (models.Lifecycle, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return models.Lifecycle{}, errors.New("lifecycle id is required")
	}
	if err := validateLifecycleInput(in); err != nil {
		return models.Lifecycle{}, err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return models.Lifecycle{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO lifecycles (id, description, is_default)
		VALUES ($1,$2,false)`, id, strings.TrimSpace(in.Description)); err != nil {
		_ = tx.Rollback()
		return models.Lifecycle{}, err
	}
	if err := replaceLifecycleSteps(ctx, tx, id, in.Steps); err != nil {
		_ = tx.Rollback()
		return models.Lifecycle{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.Lifecycle{}, err
	}
	return s.GetLifecycle(ctx, id)
}

func (s *Store) UpsertDefaultLifecycle(ctx context.Context, in LifecycleInput) error {
	if strings.TrimSpace(in.ID) == "" {
		return errors.New("lifecycle id is required")
	}
	if err := validateLifecycleInput(in); err != nil {
		return err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(in.ID)
	if _, err := tx.ExecContext(ctx, `INSERT INTO lifecycles (id, description, is_default)
		VALUES ($1,$2,true)
		ON CONFLICT (id) DO UPDATE SET description=$2, is_default=true, updated_at=now()`, id, strings.TrimSpace(in.Description)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM lifecycle_steps WHERE lifecycle_id=$1", id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := replaceLifecycleSteps(ctx, tx, id, in.Steps); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateLifecycle(ctx context.Context, id string, in LifecycleInput) (models.Lifecycle, error) {
	current, err := s.GetLifecycle(ctx, id)
	if err != nil {
		return current, err
	}
	if strings.TrimSpace(in.ID) != "" && strings.TrimSpace(in.ID) != id {
		return current, errors.New("lifecycle id cannot be changed")
	}
	if err := validateLifecycleInput(in); err != nil {
		return current, err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return current, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE lifecycles SET description=$2, updated_at=now() WHERE id=$1`, id, strings.TrimSpace(in.Description)); err != nil {
		_ = tx.Rollback()
		return current, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM lifecycle_steps WHERE lifecycle_id=$1", id); err != nil {
		_ = tx.Rollback()
		return current, err
	}
	if err := replaceLifecycleSteps(ctx, tx, id, in.Steps); err != nil {
		_ = tx.Rollback()
		return current, err
	}
	if err := tx.Commit(); err != nil {
		return current, err
	}
	return s.GetLifecycle(ctx, id)
}

func (s *Store) DeleteLifecycle(ctx context.Context, id string) error {
	lifecycle, err := s.GetLifecycle(ctx, id)
	if err != nil {
		return err
	}
	if lifecycle.IsDefault {
		return ErrLifecycleIsDefault
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM lifecycles WHERE id=$1", id)
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.GetContext(ctx, &value, "SELECT value FROM app_settings WHERE key=$1", strings.TrimSpace(key))
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return value, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) (string, error) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return "", errors.New("setting key is required")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO app_settings (key, value)
		VALUES ($1,$2)
		ON CONFLICT (key) DO UPDATE SET value=$2, updated_at=now()`, key, value); err != nil {
		return "", err
	}
	return s.GetSetting(ctx, key)
}

func (s *Store) attachLifecycleSteps(ctx context.Context, lifecycles []models.Lifecycle) error {
	for i := range lifecycles {
		steps, err := s.lifecycleSteps(ctx, lifecycles[i].ID)
		if err != nil {
			return err
		}
		lifecycles[i].Steps = steps
	}
	return nil
}

func (s *Store) lifecycleSteps(ctx context.Context, lifecycleID string) ([]models.LifecycleStep, error) {
	var steps []models.LifecycleStep
	return steps, s.db.SelectContext(ctx, &steps, "SELECT * FROM lifecycle_steps WHERE lifecycle_id=$1 ORDER BY position", lifecycleID)
}

type lifecycleStepWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func replaceLifecycleSteps(ctx context.Context, q lifecycleStepWriter, lifecycleID string, steps []LifecycleStepInput) error {
	for position, step := range steps {
		agentID := strings.TrimSpace(step.AgentID)
		if agentID == "" {
			return fmt.Errorf("lifecycle step %d agent_id is required", position+1)
		}
		id := strings.TrimSpace(step.ID)
		if id == "" {
			id = uuid.NewString()
		}
		if err := validateOptionalCLIKind(step.CLIKind); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO lifecycle_steps (id, lifecycle_id, position, agent_id, cli_kind, skip_when, include_when, model_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id, lifecycleID, position, agentID, strings.TrimSpace(step.CLIKind), pq.StringArray(normalizeLifecycleTags(step.SkipWhen)), pq.StringArray(normalizeLifecycleTags(step.IncludeWhen)), strings.TrimSpace(step.ModelID)); err != nil {
			return err
		}
	}
	return nil
}

func validateLifecycleInput(in LifecycleInput) error {
	if len(in.Steps) == 0 {
		return errors.New("lifecycle requires at least one step")
	}
	for i, step := range in.Steps {
		if strings.TrimSpace(step.AgentID) == "" {
			return fmt.Errorf("lifecycle step %d agent_id is required", i+1)
		}
		if err := validateOptionalCLIKind(step.CLIKind); err != nil {
			return err
		}
	}
	return nil
}

func validateOptionalCLIKind(kind string) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil
	}
	if kind == "claude" || kind == "codex" {
		return nil
	}
	return fmt.Errorf("cli_kind must be claude, codex, or empty")
}

func normalizeLifecycleTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}
