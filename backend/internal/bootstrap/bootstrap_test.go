package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mini-paperclip/backend/internal/models"
)

func TestIsPinnedSHA(t *testing.T) {
	if !isPinnedSHA("0123456789abcdef0123456789abcdef01234567") {
		t.Fatal("expected 40-char hex string to be treated as pinned SHA")
	}
	if isPinnedSHA("main") {
		t.Fatal("branch name should not be treated as pinned SHA")
	}
}

func TestSyncAgentDefinitionsFailsOnMissingSkill(t *testing.T) {
	err := (&Service{}).SyncAgentDefinitions(context.Background(), map[string]models.Skill{})
	if err == nil || !strings.Contains(err.Error(), `requires missing skill`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverSkillsWithPathFilter(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills", "wanted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "wanted", "SKILL.md"), []byte("# Wanted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "ignored", "SKILL.md"), []byte("# Ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills, err := discoverSkills(root, "source-1", "skills/wanted/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "wanted" || skills[0].PathInSource != "skills/wanted/SKILL.md" {
		t.Fatalf("unexpected skills: %+v", skills)
	}
}
