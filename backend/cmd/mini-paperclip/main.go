package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"mini-paperclip/backend/internal/api"
	"mini-paperclip/backend/internal/bootstrap"
	"mini-paperclip/backend/internal/config"
	"mini-paperclip/backend/internal/db"
	"mini-paperclip/backend/internal/lifecycles"
	"mini-paperclip/backend/internal/orchestrator"
	"mini-paperclip/backend/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	conn, err := db.Open(cfg.DBDSN)
	if err != nil {
		slog.Error("open db failed", "error", err)
		os.Exit(1)
	}
	if err := db.Migrate(conn); err != nil {
		slog.Error("migrate db failed", "error", err)
		os.Exit(1)
	}
	st := store.New(conn, cfg.RepoAllowlist, cfg.RepoPathAliases...)
	lifecycleService := lifecycles.New(st)
	st.SetLifecycleRouter(lifecycleService)
	bs := bootstrap.New(st, cfg.SkillsCacheDir, cfg.ValidateSkillSource)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	status, err := bs.Status(ctx)
	if err != nil {
		slog.Error("bootstrap status failed", "error", err)
		os.Exit(1)
	}
	if status.Bootstrapped {
		if err := bs.ValidateSources(ctx); err != nil {
			slog.Error("validate skill sources failed", "error", err)
			os.Exit(1)
		}
		if err := bs.SyncAgentDefinitions(ctx, nil); err != nil {
			slog.Error("sync repo-defined agents failed", "error", err)
			os.Exit(1)
		}
		if err := bs.EnsureLifecycles(ctx); err != nil {
			slog.Error("seed lifecycles failed", "error", err)
			os.Exit(1)
		}
	}
	orch := orchestrator.New(cfg, st, lifecycleService)
	bs.StartUpdatePoller(ctx)
	orch.Start(ctx)
	handler := api.NewRouter(cfg, st, bs, orch, lifecycleService)
	slog.Info("mini-paperclip backend listening", "port", cfg.Port, "workers", cfg.Workers)
	if err := http.ListenAndServe(":"+cfg.Port, handler); err != nil {
		slog.Error("http server failed", "error", err)
		os.Exit(1)
	}
}
