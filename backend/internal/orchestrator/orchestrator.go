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
	"time"

	"mini-paperclip/backend/internal/config"
	"mini-paperclip/backend/internal/models"
	"mini-paperclip/backend/internal/store"
)

type Orchestrator struct {
	cfg     config.Config
	store   *store.Store
	runners map[string]Runner
}

func New(cfg config.Config, st *store.Store) *Orchestrator {
	runners := map[string]Runner{
		"claude": ClaudeRunner{},
		"codex":  CodexRunner{},
	}
	if cfg.RunnerMode == "mock" {
		runners["claude"] = MockRunner{}
		runners["codex"] = MockRunner{}
	}
	return &Orchestrator{
		cfg:     cfg,
		store:   st,
		runners: runners,
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
	return o.store.DispatchQueuedRuns(ctx, o.cfg.MaxTasksPerTick)
}

func (o *Orchestrator) EnqueueTask(ctx context.Context, taskID string) (models.Run, error) {
	return o.store.EnqueueTaskRun(ctx, taskID)
}

func (o *Orchestrator) worker(ctx context.Context, index int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		run, ok, err := o.store.ClaimQueuedRun(ctx)
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
	agent, task, repo, comments, err := o.store.TaskContext(ctx, run)
	if err != nil {
		o.failRun(ctx, run, "", fmt.Errorf("load task context: %w", err))
		return
	}
	runner := o.runners[run.CLIKind]
	if runner == nil {
		o.failRun(ctx, run, "", fmt.Errorf("runner %q is not available", run.CLIKind))
		return
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
	prompt := BuildPrompt(agent, task, repo, comments)
	result, err := runner.Run(ctx, RunRequest{
		Prompt:       prompt,
		SystemPrompt: agent.RolePrompt,
		WorktreePath: worktree,
		Profile:      deref(agent.CLIProfile),
		Timeout:      o.cfg.CLITimeout,
		MaxCostUSD:   o.cfg.RunMaxUSD,
		OnEvent: func(level, msg string) {
			o.store.AppendRunEvent(ctx, run.ID, level, msg)
		},
	})
	if err != nil {
		o.failRun(ctx, run, prompt, err)
		return
	}
	shouldCleanup = !result.Parsed.TaskUpdates.KeepWorktree
	tx, err := o.store.DB().BeginTxx(ctx, nil)
	if err != nil {
		o.failRun(ctx, run, prompt, err)
		return
	}
	if err := o.store.ApplyAgentResponse(ctx, tx, task, agent, result.Parsed); err != nil {
		_ = tx.Rollback()
		o.failRun(ctx, run, prompt, err)
		return
	}
	if err := tx.Commit(); err != nil {
		o.failRun(ctx, run, prompt, err)
		return
	}
	o.store.Notify(ctx, "task", task.ID)
	_ = o.store.FinishRun(ctx, run.ID, "done", result.ExitCode, result.TokensIn, result.TokensOut, result.CostUSD, prompt, worktree)
}

func (o *Orchestrator) failRun(ctx context.Context, run models.Run, prompt string, err error) {
	o.store.AppendRunEvent(ctx, run.ID, "error", err.Error())
	_, _ = o.store.DB().ExecContext(ctx, `UPDATE tasks SET status='blocked', retry_count=retry_count+1, updated_at=now() WHERE id=$1`, run.TaskID)
	_, _ = o.store.AddComment(ctx, run.TaskID, "system", "Run failed: "+err.Error())
	_ = o.store.FinishRun(ctx, run.ID, "error", 1, 0, 0, 0, prompt, "")
}

func (o *Orchestrator) prepareWorktree(ctx context.Context, runID string, repo *models.Repo) (string, func(), error) {
	if repo == nil {
		return "", func() {}, nil
	}
	target := filepath.Join(o.cfg.WorktreesDir, runID)
	if err := os.MkdirAll(o.cfg.WorktreesDir, 0o755); err != nil {
		return "", func() {}, err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repo.LocalPath, "worktree", "add", "--detach", target, repo.DefaultBranch)
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
