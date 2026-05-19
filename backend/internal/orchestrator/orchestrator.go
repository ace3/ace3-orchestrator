package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mini-paperclip/backend/internal/config"
	"mini-paperclip/backend/internal/lifecycles"
	"mini-paperclip/backend/internal/models"
	"mini-paperclip/backend/internal/repoconfig"
	"mini-paperclip/backend/internal/store"
)

type Orchestrator struct {
	cfg        config.Config
	store      *store.Store
	lifecycles *lifecycles.Service
	runners    map[string]Runner
}

func New(cfg config.Config, st *store.Store, lifecycleService *lifecycles.Service) *Orchestrator {
	runners := map[string]Runner{
		"claude": ClaudeRunner{},
		"codex":  CodexRunner{},
	}
	if cfg.RunnerMode == "mock" {
		runners["claude"] = MockRunner{}
		runners["codex"] = MockRunner{}
	}
	return &Orchestrator{
		cfg:        cfg,
		store:      st,
		lifecycles: lifecycleService,
		runners:    runners,
	}
}

func (o *Orchestrator) Start(ctx context.Context) {
	if err := o.RecoverInterrupted(ctx); err != nil {
		slog.Error("startup recovery failed", "error", err)
	}
	for i := 0; i < o.cfg.Workers; i++ {
		go o.worker(ctx, i)
	}
	go func() {
		ticker := time.NewTicker(o.cfg.Heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := o.DispatchOnce(ctx); err != nil {
					slog.Error("dispatcher failed", "error", err)
				}
			}
		}
	}()
}

func (o *Orchestrator) RecoverInterrupted(ctx context.Context) error {
	if err := o.store.RecoverRunningRuns(ctx); err != nil {
		return err
	}
	return o.cleanupOrphanWorktrees(ctx)
}

func (o *Orchestrator) DispatchOnce(ctx context.Context) (int, error) {
	spent, err := o.store.MonthCostUSD(ctx)
	if err != nil {
		return 0, err
	}
	if o.cfg.MonthMaxUSD > 0 && spent >= o.cfg.MonthMaxUSD {
		slog.Warn("monthly cost ceiling reached", "spent_usd", spent, "limit_usd", o.cfg.MonthMaxUSD)
		return 0, nil
	}
	return o.store.DispatchWakeups(ctx, o.cfg.MaxTasksPerTick)
}

func (o *Orchestrator) EnqueueTask(ctx context.Context, taskID string) (models.AgentWakeup, error) {
	waiting, err := o.store.HasOpenInteraction(ctx, taskID)
	if err != nil {
		return models.AgentWakeup{}, err
	}
	if waiting {
		return models.AgentWakeup{}, store.ErrConflict
	}
	return o.store.EnqueueTaskWakeup(ctx, taskID, store.WakeupInput{
		Source:        "manual",
		Reason:        "manual_run",
		RequesterType: "human",
		RequesterID:   stringPtr("ignas"),
	})
}

func (o *Orchestrator) worker(ctx context.Context, index int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		run, ok, err := o.store.ClaimQueuedWakeup(ctx)
		if err != nil {
			slog.Error("worker claim failed", "worker", index, "error", err)
			time.Sleep(time.Second)
			continue
		}
		if !ok {
			time.Sleep(time.Second)
			continue
		}
		o.executeRun(ctx, run)
	}
}

func (o *Orchestrator) executeRun(ctx context.Context, run models.Run) {
	agent, task, repo, comments, artifacts, control, err := o.store.TaskRunContext(ctx, run)
	if err != nil {
		o.failRun(ctx, run, "", fmt.Errorf("load task context: %w", err))
		return
	}
	runner := o.runners[run.CLIKind]
	if runner == nil {
		o.failRun(ctx, run, "", fmt.Errorf("runner %q is not available", run.CLIKind))
		return
	}
	o.store.AppendRunEvent(ctx, run.ID, "info", "agent prompt source: database")
	runSkills, err := o.runSkillSelections(ctx, agent, task)
	if err != nil {
		o.failRun(ctx, run, "", fmt.Errorf("select skill docs: %w", err))
		return
	}
	skillDocs, warnings, err := o.loadSkillDocs(ctx, runSkills)
	if err != nil {
		o.failRun(ctx, run, "", fmt.Errorf("load skill docs: %w", err))
		return
	}
	for _, warning := range warnings {
		o.store.AppendRunEvent(ctx, run.ID, "warn", "skill docs warning: "+warning)
	}
	worktree, cleanup, err := o.prepareWorktree(ctx, run.ID, repo)
	if err != nil {
		o.failRun(ctx, run, "", fmt.Errorf("prepare worktree: %w", err))
		return
	}
	shouldCleanup := true
	defer func() {
		if shouldCleanup {
			cleanup()
		}
	}()
	lifecyclePrompt, err := o.lifecyclePromptContext(ctx, task, agent.ID, run.CLIKind)
	if err != nil {
		o.failRun(ctx, run, "", fmt.Errorf("load lifecycle context: %w", err))
		return
	}
	prompt := BuildPromptWithLifecycle(agent, task, repo, comments, artifacts, skillDocs, control, lifecyclePrompt)
	var sessionID string
	if control.RuntimeState != nil && control.RuntimeState.SessionID != nil {
		sessionID = *control.RuntimeState.SessionID
	}
	result, err := runner.Run(ctx, RunRequest{
		Prompt:       prompt,
		SystemPrompt: agent.RolePrompt,
		WorktreePath: worktree,
		Profile:      deref(agent.CLIProfile),
		SessionID:    sessionID,
		Model:        lifecyclePrompt.CurrentModel,
		Timeout:      o.cfg.CLITimeout,
		MaxCostUSD:   o.cfg.RunMaxUSD,
		OnEvent: func(level, msg string) {
			o.store.AppendRunEvent(ctx, run.ID, level, msg)
		},
	})
	if err != nil {
		o.failRun(ctx, run, hashablePrompt(agent.RolePrompt, prompt), err)
		return
	}
	if result.SessionID == nil {
		o.store.AppendRunEvent(ctx, run.ID, "warn", "runner did not report a session id; future runs will start without CLI resume continuity")
	}
	shouldCleanup = !result.Parsed.TaskUpdates.KeepWorktree
	tx, err := o.store.DB().BeginTxx(ctx, nil)
	if err != nil {
		o.failRun(ctx, run, hashablePrompt(agent.RolePrompt, prompt), err)
		return
	}
	if err := o.store.ApplyAgentResponse(ctx, tx, task, agent, result.Parsed, &run.ID); err != nil {
		_ = tx.Rollback()
		o.failRun(ctx, run, hashablePrompt(agent.RolePrompt, prompt), err)
		return
	}
	if err := tx.Commit(); err != nil {
		o.failRun(ctx, run, hashablePrompt(agent.RolePrompt, prompt), err)
		return
	}
	o.store.Notify(ctx, "task", task.ID)
	_ = o.store.UpdateRuntimeState(ctx, run, result.SessionID, runtimeStateJSON(result), "done")
	_ = o.store.FinishRun(ctx, run.ID, "done", result.ExitCode, result.TokensIn, result.TokensOut, result.CostUSD, hashablePrompt(agent.RolePrompt, prompt), worktree)
}

func (o *Orchestrator) lifecyclePromptContext(ctx context.Context, task models.Task, agentID, fallbackCLIKind string) (LifecyclePromptContext, error) {
	lifecycleID := task.LifecycleID
	if lifecycleID == "" {
		lifecycleID = repoconfig.DefaultLifecycleID
	}
	currentModel, err := o.lifecycles.ModelForStep(ctx, lifecycleID, agentID)
	if err != nil {
		return LifecyclePromptContext{}, err
	}
	currentCLIKind, err := o.lifecycles.CLIKindForStep(ctx, lifecycleID, agentID)
	if err != nil {
		return LifecyclePromptContext{}, err
	}
	if currentCLIKind == "" {
		currentCLIKind = fallbackCLIKind
	}
	remaining, err := o.lifecycles.RemainingSteps(ctx, lifecycleID, agentID, []string(task.Tags))
	if err != nil {
		return LifecyclePromptContext{}, err
	}
	steps := make([]LifecyclePromptStep, 0, len(remaining))
	for _, step := range remaining {
		modelID := strings.TrimSpace(step.ModelID)
		if modelID == "" {
			modelID, err = o.lifecycles.ModelForStep(ctx, lifecycleID, step.AgentID)
			if err != nil {
				return LifecyclePromptContext{}, err
			}
		}
		cliKind := strings.TrimSpace(step.CLIKind)
		if cliKind == "" {
			cliKind, err = o.lifecycles.CLIKindForStep(ctx, lifecycleID, step.AgentID)
			if err != nil {
				return LifecyclePromptContext{}, err
			}
		}
		steps = append(steps, LifecyclePromptStep{AgentID: step.AgentID, CLIKind: cliKind, ModelID: modelID})
	}
	return LifecyclePromptContext{
		ID:             lifecycleID,
		CurrentAgent:   agentID,
		CurrentCLIKind: currentCLIKind,
		CurrentModel:   currentModel,
		Remaining:      steps,
	}, nil
}

func (o *Orchestrator) failRun(ctx context.Context, run models.Run, prompt string, err error) {
	o.store.AppendRunEvent(ctx, run.ID, "error", err.Error())
	_, _ = o.store.DB().ExecContext(ctx, `UPDATE tasks SET status='blocked', retry_count=retry_count+1, updated_at=now() WHERE id=$1`, run.TaskID)
	_, _ = o.store.AddComment(ctx, run.TaskID, "system", "Run failed: "+err.Error())
	_ = o.store.UpdateRuntimeState(ctx, run, nil, nil, "error")
	_ = o.store.FinishRun(ctx, run.ID, "error", 1, 0, 0, 0, prompt, "")
	_ = o.store.FinishWakeupForRun(ctx, run.ID, "error", err.Error())
}

func hashablePrompt(systemPrompt, taskPrompt string) string {
	return "System instructions:\n" + systemPrompt + "\n\nTask prompt:\n" + taskPrompt
}

func runtimeStateJSON(result RunResult) json.RawMessage {
	sessionIDFound := result.SessionID != nil
	body, err := json.Marshal(map[string]any{
		"session_id_found": sessionIDFound,
		"tokens_in":        result.TokensIn,
		"tokens_out":       result.TokensOut,
		"cost_usd":         result.CostUSD,
		"exit_code":        result.ExitCode,
	})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return body
}

type runSkillSelection struct {
	Skill  models.Skill
	Reason string
}

func (o *Orchestrator) runSkillSelections(ctx context.Context, agent models.Agent, task models.Task) ([]runSkillSelection, error) {
	cfg, err := repoconfig.Load()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(agent.Skills))
	selections := make([]runSkillSelection, 0, len(agent.Skills))
	for _, skill := range agent.Skills {
		seen[skill.Name] = true
		selections = append(selections, runSkillSelection{Skill: skill, Reason: "assigned"})
	}
	recommended := cfg.RecommendedSkillNames(agent.ID, task.Title, task.Description, task.LifecycleID, []string(task.Tags))
	for _, name := range recommended {
		if seen[name] {
			continue
		}
		skills, err := o.store.SkillsByName(ctx, []string{name})
		if err != nil {
			return nil, err
		}
		if len(skills) == 0 {
			continue
		}
		seen[name] = true
		selections = append(selections, runSkillSelection{Skill: skills[0], Reason: "recommended"})
	}
	return selections, nil
}

func (o *Orchestrator) loadSkillDocs(ctx context.Context, selections []runSkillSelection) ([]SkillDoc, []string, error) {
	if len(selections) == 0 {
		return nil, nil, nil
	}
	sources, err := o.store.ListSkillSources(ctx)
	if err != nil {
		return nil, nil, err
	}
	sourcesByID := make(map[string]models.SkillSource, len(sources))
	for _, source := range sources {
		sourcesByID[source.ID] = source
	}
	docs := make([]SkillDoc, 0, len(selections))
	var warnings []string
	for _, selection := range selections {
		skill := selection.Skill
		source, ok := sourcesByID[skill.SourceID]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("skill %q references missing source %q", skill.Name, skill.SourceID))
			continue
		}
		rel := filepath.Clean(skill.PathInSource)
		if filepath.IsAbs(rel) || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			warnings = append(warnings, fmt.Sprintf("skill %q has invalid path %q", skill.Name, skill.PathInSource))
			continue
		}
		path := filepath.Join(o.cfg.SkillsCacheDir, source.Name, source.PinnedSHA, rel)
		content, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("read skill %q from %s: %v", skill.Name, path, err))
			continue
		}
		docs = append(docs, SkillDoc{
			Skill:   skill,
			Source:  source.Name,
			Path:    rel,
			Reason:  selection.Reason,
			Content: string(content),
		})
	}
	return docs, warnings, nil
}

func (o *Orchestrator) prepareWorktree(ctx context.Context, runID string, repo *models.Repo) (string, func(), error) {
	if repo == nil {
		return "", func() {}, nil
	}
	target := filepath.Join(o.cfg.WorktreesDir, runID)
	if err := os.MkdirAll(o.cfg.WorktreesDir, 0o755); err != nil {
		return "", func() {}, err
	}
	args := []string{"-C", repo.LocalPath, "worktree", "add", "--detach", target, repo.DefaultBranch}
	if err := exec.CommandContext(ctx, "git", "-C", repo.LocalPath, "rev-parse", "--verify", "HEAD").Run(); err != nil {
		branch := "mp-run-" + strings.ReplaceAll(runID, "-", "")
		args = []string{"-C", repo.LocalPath, "worktree", "add", "--orphan", "-b", branch, target}
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", func() {}, fmt.Errorf("%w: %s", err, string(out))
	}
	cleanup := func() {
		_ = exec.Command("git", "-C", repo.LocalPath, "worktree", "remove", "--force", target).Run()
		_ = os.RemoveAll(target)
	}
	return target, cleanup, nil
}

func (o *Orchestrator) cleanupOrphanWorktrees(ctx context.Context) error {
	entries, err := os.ReadDir(o.cfg.WorktreesDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	active, err := o.store.ActiveWorktreePaths(ctx)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(o.cfg.WorktreesDir, entry.Name())
		if active[path] {
			continue
		}
		_ = os.RemoveAll(path)
	}
	return nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func FormatSSE(kind string, payload any) string {
	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte(`{"error":"marshal failed"}`)
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", kind, body)
}

var ErrNoRunner = errors.New("runner unavailable")
