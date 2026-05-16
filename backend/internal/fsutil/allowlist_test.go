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

func TestCleanUnderAllowlistWithAliases(t *testing.T) {
	got, err := CleanUnderAllowlistWithAliases(
		"/Users/ignasius/_PROJECT/_NOBI/repo",
		[]string{"/host/code"},
		[]PathAlias{{From: "/Users/ignasius/_PROJECT", To: "/host/code"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "/host/code/_NOBI/repo"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
