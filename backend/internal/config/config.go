package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"mini-paperclip/backend/internal/fsutil"
)

type Config struct {
	DBDSN            string
	Port             string
	APIToken         string
	RepoAllowlist    []string
	RepoPathAliases  []fsutil.PathAlias
	HostCodeDir      string
	ContainerCodeDir string
	SkillsCacheDir   string
	WorktreesDir     string
	BackupDir        string
	RunnerMode       string
	CLITimeout       time.Duration
	RunMaxUSD        float64
	MonthMaxUSD      float64
	Heartbeat        time.Duration
	Workers          int
	MaxTasksPerTick  int
}

func Load() Config {
	hostCodeDir := env("MP_HOST_CODE_DIR", "")
	containerCodeDir := env("MP_CONTAINER_CODE_DIR", "/host/code")
	return Config{
		DBDSN:            env("MP_DB_DSN", "postgres://mp:mp_dev_password@localhost:5432/mini_paperclip?sslmode=disable"),
		Port:             env("MP_PORT", "8081"),
		APIToken:         env("MP_API_TOKEN", "dev-token"),
		RepoAllowlist:    splitPaths(env("MP_REPO_ALLOWLIST", "/host/code")),
		RepoPathAliases:  repoPathAliases(hostCodeDir, containerCodeDir),
		HostCodeDir:      hostCodeDir,
		ContainerCodeDir: containerCodeDir,
		SkillsCacheDir:   env("MP_SKILLS_CACHE_DIR", "/tmp/mini-paperclip/skills-cache"),
		WorktreesDir:     env("MP_WORKTREES_DIR", "/tmp/mini-paperclip/worktrees"),
		BackupDir:        env("MP_BACKUP_DIR", "/tmp/mini-paperclip/backups"),
		RunnerMode:       env("MP_RUNNER_MODE", "cli"),
		CLITimeout:       durationEnv("MP_CLI_TIMEOUT", 600*time.Second),
		RunMaxUSD:        floatEnv("MP_RUN_MAX_USD", 1.0),
		MonthMaxUSD:      floatEnv("MP_MONTH_MAX_USD", 100.0),
		Heartbeat:        durationEnv("MP_HEARTBEAT_INTERVAL", 60*time.Second),
		Workers:          nonNegativeIntEnv("MP_WORKERS", 4),
		MaxTasksPerTick:  intEnv("MP_MAX_TASKS_PER_HEARTBEAT", 10),
	}
}

func repoPathAliases(hostCodeDir, containerCodeDir string) []fsutil.PathAlias {
	hostCodeDir = strings.TrimSpace(hostCodeDir)
	containerCodeDir = strings.TrimSpace(containerCodeDir)
	if hostCodeDir == "" || containerCodeDir == "" || hostCodeDir == containerCodeDir {
		return nil
	}
	return []fsutil.PathAlias{{From: hostCodeDir, To: containerCodeDir}}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitPaths(value string) []string {
	parts := strings.Split(value, ":")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	if d, err := time.ParseDuration(value); err == nil {
		return d
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func nonNegativeIntEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func floatEnv(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
