package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanUnderAllowlist(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "repo")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := CleanUnderAllowlist(child, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if got != child {
		t.Fatalf("got %q want %q", got, child)
	}
	if _, err := CleanUnderAllowlist(filepath.Dir(root), []string{child}); !errors.Is(err, ErrOutsideAllowlist) {
		t.Fatalf("got %v want ErrOutsideAllowlist", err)
	}
}
