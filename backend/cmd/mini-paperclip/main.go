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
	"mini-paperclip/backend/internal/orchestrator"
	"mini-paperclip/backend/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg := config.Load()
	conn, err := db.Open(cfg.DBDSN)
	if err != nil {
		slog.Error("open db failed", "error", err)
		os.Exit(1)
	}
	if err := db.Migrate(conn); err != nil {
		slog.Error("migrate db failed", "error", err)
		os.Exit(1)
	}
	st := store.New(conn, cfg.RepoAllowlist)
	bs := bootstrap.New(st, cfg.SkillsCacheDir)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	orch := orchestrator.New(cfg, st)
	bs.StartUpdatePoller(ctx)
	orch.Start(ctx)
	handler := api.NewRouter(cfg, st, bs, orch)
	slog.Info("mini-paperclip backend listening", "port", cfg.Port, "workers", cfg.Workers)
	if err := http.ListenAndServe(":"+cfg.Port, handler); err != nil {
		slog.Error("http server failed", "error", err)
		os.Exit(1)
	}
}
