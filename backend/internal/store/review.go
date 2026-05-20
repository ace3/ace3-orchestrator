package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"mini-paperclip/backend/internal/models"
)

type ReviewCommentInput struct {
	RunID     *string `json:"run_id"`
	FilePath  string  `json:"file_path"`
	LineStart *int    `json:"line_start"`
	LineEnd   *int    `json:"line_end"`
	Body      string  `json:"body"`
	Status    string  `json:"status"`
}

type ReviewInput struct {
	Action          string `json:"action"`
	FeedbackToAgent bool   `json:"feed_back_to_agent"`
}

type ReviewDiffSource struct {
	RunID        string `db:"run_id"`
	WorktreePath string `db:"worktree_path"`
	BaseRef      string `db:"base_ref"`
}

func (s *Store) ListReviewComments(ctx context.Context, taskID string) ([]models.TaskReviewComment, error) {
	comments := []models.TaskReviewComment{}
	return comments, s.db.SelectContext(ctx, &comments, `SELECT * FROM task_review_comments
		WHERE task_id=$1
		ORDER BY file_path, COALESCE(line_start, 0), created_at`, taskID)
}

func (s *Store) CreateReviewComment(ctx context.Context, taskID, author string, in ReviewCommentInput) (models.TaskReviewComment, error) {
	if _, err := s.GetTask(ctx, taskID); err != nil {
		return models.TaskReviewComment{}, err
	}
	filePath, body, status, err := normalizeReviewComment(in)
	if err != nil {
		return models.TaskReviewComment{}, err
	}
	if status == "" {
		status = "open"
	}
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO task_review_comments
		(id, task_id, run_id, file_path, line_start, line_end, body, author, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		id, taskID, in.RunID, filePath, in.LineStart, in.LineEnd, body, strings.TrimSpace(author), status); err != nil {
		return models.TaskReviewComment{}, err
	}
	s.Notify(ctx, "review_comment", taskID)
	return s.GetReviewComment(ctx, id)
}

func (s *Store) GetReviewComment(ctx context.Context, id string) (models.TaskReviewComment, error) {
	var comment models.TaskReviewComment
	if err := s.db.GetContext(ctx, &comment, "SELECT * FROM task_review_comments WHERE id=$1", id); err != nil {
		return comment, mapNotFound(err)
	}
	return comment, nil
}

func (s *Store) UpdateReviewComment(ctx context.Context, taskID, id string, in ReviewCommentInput) (models.TaskReviewComment, error) {
	comment, err := s.GetReviewComment(ctx, id)
	if err != nil {
		return comment, err
	}
	if comment.TaskID != taskID {
		return comment, ErrNotFound
	}
	if strings.TrimSpace(in.Body) != "" {
		comment.Body = strings.TrimSpace(in.Body)
	}
	if strings.TrimSpace(in.Status) != "" {
		status := strings.TrimSpace(in.Status)
		if err := validateReviewCommentStatus(status); err != nil {
			return comment, err
		}
		comment.Status = status
	}
	resolvedExpr := "resolved_at"
	if comment.Status == "resolved" {
		resolvedExpr = "COALESCE(resolved_at, now())"
	} else {
		resolvedExpr = "NULL"
	}
	query := fmt.Sprintf(`UPDATE task_review_comments
		SET body=$2, status=$3, resolved_at=%s
		WHERE id=$1`, resolvedExpr)
	if _, err := s.db.ExecContext(ctx, query, id, comment.Body, comment.Status); err != nil {
		return comment, err
	}
	s.Notify(ctx, "review_comment", taskID)
	return s.GetReviewComment(ctx, id)
}

func (s *Store) ApplyTaskReview(ctx context.Context, taskID string, in ReviewInput) (models.Task, error) {
	action := strings.TrimSpace(in.Action)
	decision, status, err := reviewDecisionAndStatus(action)
	if err != nil {
		return models.Task{}, err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return models.Task{}, err
	}
	var task models.Task
	if err := tx.GetContext(ctx, &task, "SELECT * FROM tasks WHERE id=$1 FOR UPDATE", taskID); err != nil {
		_ = tx.Rollback()
		return models.Task{}, mapNotFound(err)
	}
	if in.FeedbackToAgent && action != "approve" {
		comments := []models.TaskReviewComment{}
		if err := tx.SelectContext(ctx, &comments, `SELECT * FROM task_review_comments
			WHERE task_id=$1 AND status='open'
			ORDER BY file_path, COALESCE(line_start, 0), created_at`, taskID); err != nil {
			_ = tx.Rollback()
			return models.Task{}, err
		}
		if body := reviewerFeedbackBody(comments); body != "" {
			if _, err := s.createTaskArtifact(ctx, tx, taskID, TaskArtifactInput{
				Kind:      "implementation_note",
				Title:     "Reviewer feedback",
				Body:      &body,
				Format:    "markdown",
				Metadata:  json.RawMessage(`{"source":"review_comments"}`),
				CreatedBy: "human:ignas",
			}); err != nil {
				_ = tx.Rollback()
				return models.Task{}, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks
		SET status=$2, last_review_decision=$3, last_review_at=now(), updated_at=now()
		WHERE id=$1`, taskID, status, decision); err != nil {
		_ = tx.Rollback()
		return models.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.Task{}, err
	}
	s.Notify(ctx, "task", taskID)
	return s.GetTask(ctx, taskID)
}

func (s *Store) ReviewDiffSource(ctx context.Context, taskID string) (ReviewDiffSource, bool, error) {
	var source ReviewDiffSource
	err := s.db.GetContext(ctx, &source, `SELECT r.id AS run_id, r.worktree_path, repo.default_branch AS base_ref
		FROM tasks t
		JOIN repos repo ON repo.id=t.repo_id
		JOIN runs r ON r.task_id=t.id
		WHERE t.id=$1
		  AND r.worktree_path IS NOT NULL
		  AND r.status IN ('done','error')
		ORDER BY
		  CASE
		    WHEN t.selected_run_id=r.id THEN 0
		    WHEN t.checkout_run_id=r.id THEN 1
		    WHEN t.execution_run_id=r.id THEN 2
		    ELSE 3
		  END,
		  r.created_at DESC
		LIMIT 1`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		if _, taskErr := s.GetTask(ctx, taskID); taskErr != nil {
			return source, false, taskErr
		}
		return source, false, nil
	}
	if err != nil {
		return source, false, mapNotFound(err)
	}
	if strings.TrimSpace(source.BaseRef) == "" {
		source.BaseRef = "main"
	}
	return source, true, nil
}

func normalizeReviewComment(in ReviewCommentInput) (string, string, string, error) {
	filePath := strings.TrimSpace(in.FilePath)
	if filePath == "" {
		return "", "", "", errors.New("file_path is required")
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return "", "", "", errors.New("body is required")
	}
	if in.LineStart != nil && *in.LineStart <= 0 {
		return "", "", "", errors.New("line_start must be positive")
	}
	if in.LineEnd != nil {
		if *in.LineEnd <= 0 {
			return "", "", "", errors.New("line_end must be positive")
		}
		if in.LineStart == nil {
			return "", "", "", errors.New("line_start is required when line_end is set")
		}
		if *in.LineEnd < *in.LineStart {
			return "", "", "", errors.New("line_end must be greater than or equal to line_start")
		}
	}
	status := strings.TrimSpace(in.Status)
	if status != "" {
		if err := validateReviewCommentStatus(status); err != nil {
			return "", "", "", err
		}
	}
	return filePath, body, status, nil
}

func validateReviewCommentStatus(status string) error {
	switch status {
	case "open", "resolved":
		return nil
	default:
		return fmt.Errorf("invalid review comment status %q", status)
	}
}

func reviewDecisionAndStatus(action string) (string, string, error) {
	switch action {
	case "approve":
		return "approved", "done", nil
	case "request_changes":
		return "changes_requested", "todo", nil
	case "reject":
		return "rejected", "blocked", nil
	default:
		return "", "", fmt.Errorf("invalid review action %q", action)
	}
}

func reviewerFeedbackBody(comments []models.TaskReviewComment) string {
	if len(comments) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Reviewer feedback\n")
	for _, comment := range comments {
		location := comment.FilePath
		if comment.LineStart != nil {
			location = fmt.Sprintf("%s:%d", location, *comment.LineStart)
			if comment.LineEnd != nil && *comment.LineEnd != *comment.LineStart {
				location = fmt.Sprintf("%s-%d", location, *comment.LineEnd)
			}
		}
		body := strings.Join(strings.Fields(comment.Body), " ")
		fmt.Fprintf(&b, "- %s - %q\n", location, body)
	}
	return b.String()
}
