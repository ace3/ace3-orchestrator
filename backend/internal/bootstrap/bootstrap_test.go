package bootstrap

import (
	"context"
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
