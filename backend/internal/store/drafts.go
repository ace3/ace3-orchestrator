package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"mini-paperclip/backend/internal/models"
)

type DraftMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	TS      string `json:"ts"`
}

type DraftBrief struct {
	Title              string   `json:"title"`
	Goal               string   `json:"goal"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	TargetFiles        []string `json:"target_files"`
	Notes              string   `json:"notes"`
}

type DraftCreateInput struct {
	Author string  `json:"author"`
	RepoID *string `json:"repo_id"`
}

type DraftTurnInput struct {
	UserMessage string `json:"user_message"`
}

type DraftSubmitInput struct {
	ProjectID       string  `json:"project_id"`
	AssigneeAgentID *string `json:"assignee_agent_id"`
	Priority        int     `json:"priority"`
}

type DraftTurnResult struct {
	Draft            models.TaskDraft `json:"draft"`
	Conversation     []DraftMessage   `json:"conversation"`
	PreviewBrief     DraftBrief       `json:"preview_brief"`
	AssistantMessage string           `json:"assistant_message"`
}

func (s *Store) CreateDraft(ctx context.Context, in DraftCreateInput) (DraftTurnResult, error) {
	author := strings.TrimSpace(in.Author)
	if author == "" {
		author = "human:ignas"
	}
	conversation := []DraftMessage{draftMessage("assistant", "What's the goal?")}
	convJSON, _ := json.Marshal(conversation)
	brief := emptyDraftBrief()
	briefJSON, _ := json.Marshal(brief)
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO task_drafts (id, author, repo_id, conversation, preview_brief)
		VALUES ($1,$2,$3,$4,$5)`, id, author, in.RepoID, convJSON, briefJSON); err != nil {
		return DraftTurnResult{}, err
	}
	draft, err := s.GetDraft(ctx, id)
	if err != nil {
		return DraftTurnResult{}, err
	}
	return DraftTurnResult{Draft: draft, Conversation: conversation, PreviewBrief: brief, AssistantMessage: conversation[0].Content}, nil
}

func (s *Store) GetDraft(ctx context.Context, id string) (models.TaskDraft, error) {
	var draft models.TaskDraft
	if err := s.db.GetContext(ctx, &draft, "SELECT * FROM task_drafts WHERE id=$1", id); err != nil {
		return draft, mapNotFound(err)
	}
	return draft, nil
}

func (s *Store) DraftTurn(ctx context.Context, id string, in DraftTurnInput) (DraftTurnResult, error) {
	draft, err := s.GetDraft(ctx, id)
	if err != nil {
		return DraftTurnResult{}, err
	}
	if draft.Status != "open" {
		return DraftTurnResult{}, ErrConflict
	}
	msg := strings.TrimSpace(in.UserMessage)
	if msg == "" {
		return DraftTurnResult{}, errors.New("user_message is required")
	}
	conversation := decodeDraftConversation(draft.Conversation)
	conversation = append(conversation, draftMessage("user", msg))
	brief := buildDraftBrief(conversation)
	assistant := nextDraftQuestion(conversation, brief)
	conversation = append(conversation, draftMessage("assistant", assistant))
	convJSON, _ := json.Marshal(conversation)
	briefJSON, _ := json.Marshal(brief)
	if _, err := s.db.ExecContext(ctx, `UPDATE task_drafts
		SET conversation=$2, preview_brief=$3, updated_at=now()
		WHERE id=$1`, id, convJSON, briefJSON); err != nil {
		return DraftTurnResult{}, err
	}
	draft, err = s.GetDraft(ctx, id)
	if err != nil {
		return DraftTurnResult{}, err
	}
	s.Notify(ctx, "task_draft", id)
	return DraftTurnResult{Draft: draft, Conversation: conversation, PreviewBrief: brief, AssistantMessage: assistant}, nil
}

func (s *Store) FinalizeDraft(ctx context.Context, id string) (DraftTurnResult, error) {
	draft, err := s.GetDraft(ctx, id)
	if err != nil {
		return DraftTurnResult{}, err
	}
	conversation := decodeDraftConversation(draft.Conversation)
	brief := buildDraftBrief(conversation)
	briefJSON, _ := json.Marshal(brief)
	if _, err := s.db.ExecContext(ctx, `UPDATE task_drafts SET preview_brief=$2, updated_at=now() WHERE id=$1`, id, briefJSON); err != nil {
		return DraftTurnResult{}, err
	}
	draft, err = s.GetDraft(ctx, id)
	if err != nil {
		return DraftTurnResult{}, err
	}
	return DraftTurnResult{Draft: draft, Conversation: conversation, PreviewBrief: brief, AssistantMessage: "Brief is ready to submit."}, nil
}

func (s *Store) SubmitDraft(ctx context.Context, id string, in DraftSubmitInput) (models.Task, error) {
	draft, err := s.GetDraft(ctx, id)
	if err != nil {
		return models.Task{}, err
	}
	if draft.Status != "open" {
		return models.Task{}, ErrConflict
	}
	projectID := strings.TrimSpace(in.ProjectID)
	if projectID == "" {
		return models.Task{}, errors.New("project_id is required")
	}
	brief := decodeDraftBrief(draft.PreviewBrief)
	if strings.TrimSpace(brief.Goal) == "" || len(brief.AcceptanceCriteria) == 0 {
		return models.Task{}, errors.New("draft preview requires goal and acceptance criteria before submit")
	}
	description := draftTaskDescription(brief)
	task, err := s.CreateTask(ctx, projectID, TaskInput{
		RepoID:          draft.RepoID,
		Title:           brief.Title,
		Description:     description,
		Status:          "todo",
		AssigneeAgentID: in.AssigneeAgentID,
		Priority:        in.Priority,
	})
	if err != nil {
		return models.Task{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE task_drafts
		SET status='submitted', finalized_task_id=$2, updated_at=now()
		WHERE id=$1`, id, task.ID); err != nil {
		return models.Task{}, err
	}
	s.Notify(ctx, "task_draft", id)
	return task, nil
}

func (s *Store) DiscardDraft(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE task_drafts SET status='discarded', updated_at=now() WHERE id=$1 AND status='open'", id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	s.Notify(ctx, "task_draft", id)
	return nil
}

func draftMessage(role, content string) DraftMessage {
	return DraftMessage{Role: role, Content: content, TS: time.Now().UTC().Format(time.RFC3339)}
}

func decodeDraftConversation(raw json.RawMessage) []DraftMessage {
	var conversation []DraftMessage
	_ = json.Unmarshal(raw, &conversation)
	return conversation
}

func decodeDraftBrief(raw json.RawMessage) DraftBrief {
	brief := emptyDraftBrief()
	_ = json.Unmarshal(raw, &brief)
	if brief.AcceptanceCriteria == nil {
		brief.AcceptanceCriteria = []string{}
	}
	if brief.TargetFiles == nil {
		brief.TargetFiles = []string{}
	}
	return brief
}

func emptyDraftBrief() DraftBrief {
	return DraftBrief{
		AcceptanceCriteria: []string{},
		TargetFiles:        []string{},
	}
}

func buildDraftBrief(conversation []DraftMessage) DraftBrief {
	userMessages := make([]string, 0, len(conversation))
	for _, item := range conversation {
		if item.Role == "user" && strings.TrimSpace(item.Content) != "" {
			userMessages = append(userMessages, strings.TrimSpace(item.Content))
		}
	}
	if len(userMessages) == 0 {
		return emptyDraftBrief()
	}
	goal := userMessages[0]
	if len(userMessages) > 1 {
		goal = userMessages[0] + "\n\nContext: " + strings.Join(userMessages[1:], "\n")
	}
	title := summarizeTitle(userMessages[0])
	criteria := extractAcceptanceCriteria(userMessages)
	return DraftBrief{
		Title:              title,
		Goal:               goal,
		AcceptanceCriteria: criteria,
		TargetFiles:        extractTargetFiles(userMessages),
		Notes:              "Generated from conversational task builder.",
	}
}

func nextDraftQuestion(conversation []DraftMessage, brief DraftBrief) string {
	userTurns := 0
	for _, item := range conversation {
		if item.Role == "user" {
			userTurns++
		}
	}
	if userTurns <= 1 {
		return "What does done look like?"
	}
	if len(brief.TargetFiles) == 0 {
		return "Are there target files or areas I should include?"
	}
	return "Brief updated. Add more context or submit when ready."
}

func summarizeTitle(text string) string {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) == 0 {
		return "New task"
	}
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Trim(strings.Join(words, " "), ".,:;")
}

func extractAcceptanceCriteria(messages []string) []string {
	criteria := []string{}
	for _, msg := range messages {
		for _, line := range strings.Split(msg, "\n") {
			line = strings.Trim(strings.TrimSpace(line), "-* ")
			lower := strings.ToLower(line)
			if line == "" {
				continue
			}
			if strings.Contains(lower, "done") || strings.Contains(lower, "accept") || strings.Contains(lower, "should") || strings.Contains(lower, "must") || strings.Contains(lower, "verify") {
				criteria = append(criteria, line)
			}
		}
	}
	if len(criteria) == 0 {
		criteria = append(criteria, "Implementation satisfies the stated goal.", "Relevant tests or smoke checks pass.")
	}
	if len(criteria) > 8 {
		return criteria[:8]
	}
	return criteria
}

func extractTargetFiles(messages []string) []string {
	seen := map[string]bool{}
	files := []string{}
	for _, msg := range messages {
		for _, token := range strings.Fields(msg) {
			token = strings.Trim(token, ".,:;()[]{}'\"`")
			if strings.Contains(token, "/") || strings.Contains(token, ".go") || strings.Contains(token, ".tsx") || strings.Contains(token, ".ts") || strings.Contains(token, ".md") {
				if !seen[token] {
					seen[token] = true
					files = append(files, token)
				}
			}
		}
	}
	return files
}

func draftTaskDescription(brief DraftBrief) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Goal:\n%s\n\nAcceptance Criteria:\n", strings.TrimSpace(brief.Goal))
	for _, item := range brief.AcceptanceCriteria {
		fmt.Fprintf(&b, "- %s\n", item)
	}
	if len(brief.TargetFiles) > 0 {
		fmt.Fprintf(&b, "\nTarget Files:\n")
		for _, item := range brief.TargetFiles {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	if strings.TrimSpace(brief.Notes) != "" {
		fmt.Fprintf(&b, "\nNotes:\n%s\n", brief.Notes)
	}
	return b.String()
}
