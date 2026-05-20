package api

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"mini-paperclip/backend/internal/httpx"
	"mini-paperclip/backend/internal/store"
)

const maxDiffBytes = 2 * 1024 * 1024

type taskDiffResponse struct {
	RunID     *string    `json:"run_id"`
	Files     []diffFile `json:"files"`
	Raw       string     `json:"raw"`
	Truncated bool       `json:"truncated"`
}

type diffFile struct {
	Path      string     `json:"path"`
	OldPath   string     `json:"old_path"`
	NewPath   string     `json:"new_path"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Hunks     []diffHunk `json:"hunks"`
}

type diffHunk struct {
	Header   string     `json:"header"`
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []diffLine `json:"lines"`
}

type diffLine struct {
	Kind    string `json:"kind"`
	OldLine *int   `json:"old_line"`
	NewLine *int   `json:"new_line"`
	Content string `json:"content"`
}

func (a *API) taskDiff(w http.ResponseWriter, r *http.Request) {
	source, ok, err := a.store.ReviewDiffSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	if !ok {
		if a.cfg.RunnerMode == "mock" {
			raw := mockReviewDiff()
			httpx.JSON(w, http.StatusOK, taskDiffResponse{Files: parseUnifiedDiff(raw), Raw: raw})
			return
		}
		httpx.Error(w, http.StatusNotFound, "diff_not_available", "no reviewable run worktree found for task")
		return
	}
	raw, truncated, err := gitDiff(r.Context(), source.WorktreePath, source.BaseRef)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "diff_failed", err.Error())
		return
	}
	runID := source.RunID
	httpx.JSON(w, http.StatusOK, taskDiffResponse{RunID: &runID, Files: parseUnifiedDiff(raw), Raw: raw, Truncated: truncated})
}

func (a *API) listReviewComments(w http.ResponseWriter, r *http.Request) {
	comments, err := a.store.ListReviewComments(r.Context(), chi.URLParam(r, "id"))
	respond(w, comments, err)
}

func (a *API) createReviewComment(w http.ResponseWriter, r *http.Request) {
	var in store.ReviewCommentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	comment, err := a.store.CreateReviewComment(r.Context(), chi.URLParam(r, "id"), "human:ignas", in)
	respondCreated(w, comment, err)
}

func (a *API) updateReviewComment(w http.ResponseWriter, r *http.Request) {
	var in store.ReviewCommentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	comment, err := a.store.UpdateReviewComment(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "commentID"), in)
	respond(w, comment, err)
}

func (a *API) reviewTask(w http.ResponseWriter, r *http.Request) {
	var in store.ReviewInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	task, err := a.store.ApplyTaskReview(r.Context(), chi.URLParam(r, "id"), in)
	respond(w, task, err)
}

func gitDiff(ctx context.Context, worktreePath, baseRef string) (string, bool, error) {
	diffCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := gitDiffCommand(diffCtx, worktreePath, baseRef+"...HEAD")
	if err != nil {
		raw, err = gitDiffCommand(diffCtx, worktreePath, baseRef)
		if err != nil {
			return "", false, err
		}
	} else {
		worktreeRaw, worktreeErr := gitDiffCommand(diffCtx, worktreePath, baseRef)
		if worktreeErr == nil && strings.TrimSpace(worktreeRaw) != "" {
			raw = worktreeRaw
		}
	}
	if len(raw) <= maxDiffBytes {
		return raw, false, nil
	}
	return raw[:maxDiffBytes], true, nil
}

func gitDiffCommand(ctx context.Context, worktreePath, ref string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", worktreePath, "diff", ref).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

var hunkHeaderRE = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

func parseUnifiedDiff(raw string) []diffFile {
	files := []diffFile{}
	var current *diffFile
	var hunk *diffHunk
	oldLine := 0
	newLine := 0
	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files = append(files, diffFile{})
			current = &files[len(files)-1]
			oldPath, newPath := parseDiffGitPaths(line)
			current.OldPath = oldPath
			current.NewPath = newPath
			current.Path = displayDiffPath(oldPath, newPath)
			hunk = nil
		case current != nil && strings.HasPrefix(line, "--- "):
			current.OldPath = cleanDiffPath(strings.TrimSpace(strings.TrimPrefix(line, "--- ")))
			current.Path = displayDiffPath(current.OldPath, current.NewPath)
		case current != nil && strings.HasPrefix(line, "+++ "):
			current.NewPath = cleanDiffPath(strings.TrimSpace(strings.TrimPrefix(line, "+++ ")))
			current.Path = displayDiffPath(current.OldPath, current.NewPath)
		case current != nil && strings.HasPrefix(line, "@@ "):
			next := parseHunkHeader(line)
			current.Hunks = append(current.Hunks, next)
			hunk = &current.Hunks[len(current.Hunks)-1]
			oldLine = hunk.OldStart
			newLine = hunk.NewStart
		case current != nil && hunk != nil:
			switch {
			case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
				lineNo := newLine
				hunk.Lines = append(hunk.Lines, diffLine{Kind: "add", NewLine: &lineNo, Content: strings.TrimPrefix(line, "+")})
				current.Additions++
				newLine++
			case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
				lineNo := oldLine
				hunk.Lines = append(hunk.Lines, diffLine{Kind: "del", OldLine: &lineNo, Content: strings.TrimPrefix(line, "-")})
				current.Deletions++
				oldLine++
			case strings.HasPrefix(line, " "):
				oldNo := oldLine
				newNo := newLine
				hunk.Lines = append(hunk.Lines, diffLine{Kind: "context", OldLine: &oldNo, NewLine: &newNo, Content: strings.TrimPrefix(line, " ")})
				oldLine++
				newLine++
			}
		}
	}
	return files
}

func parseDiffGitPaths(line string) (string, string) {
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return "", ""
	}
	return cleanDiffPath(parts[2]), cleanDiffPath(parts[3])
}

func parseHunkHeader(line string) diffHunk {
	match := hunkHeaderRE.FindStringSubmatch(line)
	if len(match) == 0 {
		return diffHunk{Header: line}
	}
	return diffHunk{
		Header:   line,
		OldStart: atoiDefault(match[1], 0),
		OldLines: atoiDefault(match[2], 1),
		NewStart: atoiDefault(match[3], 0),
		NewLines: atoiDefault(match[4], 1),
	}
}

func atoiDefault(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func cleanDiffPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "/dev/null" {
		return ""
	}
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	return path
}

func displayDiffPath(oldPath, newPath string) string {
	if newPath != "" {
		return newPath
	}
	return oldPath
}

func mockReviewDiff() string {
	return strings.TrimSpace(`diff --git a/backend/internal/api/example.go b/backend/internal/api/example.go
--- a/backend/internal/api/example.go
+++ b/backend/internal/api/example.go
@@ -1,3 +1,4 @@
 package api
 
+func reviewFixture() string { return "mock" }
 func existing() {}
diff --git a/frontend/src/ReviewFixture.tsx b/frontend/src/ReviewFixture.tsx
new file mode 100644
--- /dev/null
+++ b/frontend/src/ReviewFixture.tsx
@@ -0,0 +1,3 @@
+export function ReviewFixture() {
+  return <p>mock diff</p>;
+}
diff --git a/docs/review.md b/docs/review.md
deleted file mode 100644
--- a/docs/review.md
+++ /dev/null
@@ -1,2 +0,0 @@
-# Old review notes
-Remove this fixture.
`) + "\n"
}
