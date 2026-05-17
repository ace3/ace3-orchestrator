package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSkillTreeAndReadContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# Skill\nUse it."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "references", "guide.md"), []byte("guide"), 0o644); err != nil {
		t.Fatal(err)
	}

	tree, err := buildSkillTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Type != "directory" || len(tree.Children) != 2 {
		t.Fatalf("unexpected tree: %+v", tree)
	}
	clean, path, err := safeSkillPath(root, "references/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	content, err := readTextPreview(path)
	if err != nil {
		t.Fatal(err)
	}
	if clean != "references/guide.md" || content != "guide" {
		t.Fatalf("unexpected content %q at %q", content, clean)
	}
}

func TestSkillContentRejectsUnsafePathAndBinary(t *testing.T) {
	root := t.TempDir()
	if _, _, err := safeSkillPath(root, "../secret.md"); err == nil {
		t.Fatal("expected path traversal rejection")
	}
	binaryPath := filepath.Join(root, "image.bin")
	if err := os.WriteFile(binaryPath, []byte{0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readTextPreview(binaryPath); err == nil || !strings.Contains(err.Error(), "supported text") {
		t.Fatalf("expected unsupported text error, got %v", err)
	}
}

func TestSkillContentRejectsLargeFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", maxSkillPreviewBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readTextPreview(path); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected large file error, got %v", err)
	}
}
