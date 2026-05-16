package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrOutsideAllowlist = errors.New("path is outside MP_REPO_ALLOWLIST")

func CleanUnderAllowlist(path string, allowlist []string) (string, error) {
	clean, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	for _, root := range allowlist {
		rootAbs, err := filepath.Abs(filepath.Clean(root))
		if err != nil {
			continue
		}
		if clean == rootAbs || strings.HasPrefix(clean, rootAbs+string(os.PathSeparator)) {
			return clean, nil
		}
	}
	return "", ErrOutsideAllowlist
}

type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

func Browse(path string, allowlist []string) ([]Entry, error) {
	clean, err := CleanUnderAllowlist(path, allowlist)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(clean)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		out = append(out, Entry{
			Name:  entry.Name(),
			Path:  filepath.Join(clean, entry.Name()),
			IsDir: entry.IsDir(),
		})
	}
	return out, nil
}
