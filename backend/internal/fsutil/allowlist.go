package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrOutsideAllowlist = errors.New("path is outside MP_REPO_ALLOWLIST")

type PathAlias struct {
	From string
	To   string
}

func CleanUnderAllowlist(path string, allowlist []string) (string, error) {
	return CleanUnderAllowlistWithAliases(path, allowlist, nil)
}

func CleanUnderAllowlistWithAliases(path string, allowlist []string, aliases []PathAlias) (string, error) {
	path = applyPathAliases(path, aliases)
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

func applyPathAliases(path string, aliases []PathAlias) string {
	clean, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return path
	}
	for _, alias := range aliases {
		from, err := filepath.Abs(filepath.Clean(alias.From))
		if err != nil {
			continue
		}
		to, err := filepath.Abs(filepath.Clean(alias.To))
		if err != nil {
			continue
		}
		if clean == from {
			return to
		}
		if strings.HasPrefix(clean, from+string(os.PathSeparator)) {
			return filepath.Join(to, strings.TrimPrefix(clean, from+string(os.PathSeparator)))
		}
	}
	return path
}

type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

func Browse(path string, allowlist []string) ([]Entry, error) {
	return BrowseWithAliases(path, allowlist, nil)
}

func BrowseWithAliases(path string, allowlist []string, aliases []PathAlias) ([]Entry, error) {
	clean, err := CleanUnderAllowlistWithAliases(path, allowlist, aliases)
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
