package config

import (
	"strings"
	"testing"
)

func TestValidateAllowsDevelopmentDefaults(t *testing.T) {
	cfg := Config{
		Env:           "development",
		APIToken:      "dev-token",
		DBDSN:         "postgres://mp:mp_dev_password@localhost:5432/mini_paperclip?sslmode=disable",
		RunnerMode:    "cli",
		RepoAllowlist: []string{"/"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("development defaults should remain valid: %v", err)
	}
}

func TestValidateRejectsUnsafeProductionDefaults(t *testing.T) {
	cfg := Config{
		Env:           "production",
		APIToken:      "dev-token",
		DBDSN:         "postgres://mp:mp_dev_password@db:5432/mini_paperclip?sslmode=disable",
		RunnerMode:    "cli",
		RepoAllowlist: []string{"/"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected production validation errors")
	}
	text := err.Error()
	for _, want := range []string{"MP_API_TOKEN", "MP_DB_DSN", "MP_ENABLE_REAL_CLI", "MP_REPO_ALLOWLIST"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in error %q", want, text)
		}
	}
}

func TestValidateAcceptsProductionGuardrails(t *testing.T) {
	cfg := Config{
		Env:           "production",
		APIToken:      strings.Repeat("a", 32),
		DBDSN:         "postgres://mp:strong-password@db:5432/mini_paperclip?sslmode=disable",
		RunnerMode:    "cli",
		EnableRealCLI: true,
		RepoAllowlist: []string{"/host/code/project"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected production config to pass: %v", err)
	}
}

func TestValidateSkillSourceRequiresPinnedGitHubInProduction(t *testing.T) {
	cfg := Config{Env: "production"}
	if err := cfg.ValidateSkillSource("https://github.com/ace3/skills", "main"); err == nil {
		t.Fatal("expected branch ref to be rejected")
	}
	if err := cfg.ValidateSkillSource("git@github.com:ace3/skills.git", strings.Repeat("a", 40)); err == nil {
		t.Fatal("expected non-https GitHub URL to be rejected")
	}
	if err := cfg.ValidateSkillSource("https://github.com/ace3/skills", strings.Repeat("a", 40)); err != nil {
		t.Fatalf("expected pinned https GitHub URL to pass: %v", err)
	}
}
