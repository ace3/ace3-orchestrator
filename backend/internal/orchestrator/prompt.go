package orchestrator

import (
	"fmt"
	"strings"

	"mini-paperclip/backend/internal/models"
	"mini-paperclip/backend/internal/repoconfig"
)

func BuildPrompt(agent models.Agent, task models.Task, repo *models.Repo, comments []models.Comment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== AGENT ===\nID: %s\nName: %s\nRole: %s\n\n", agent.ID, agent.Name, agent.Role)
	fmt.Fprintf(&b, "=== ACTIVE SKILLS ===\n")
	for _, skill := range agent.Skills {
		fmt.Fprintf(&b, "- %s\n", skill.Name)
	}
	fmt.Fprintf(&b, "\n=== TASK ===\nID: %s\nTitle: %s\nDescription: %s\nStatus: %s\nPriority: %d\n", task.ID, task.Title, task.Description, task.Status, task.Priority)
	if task.ParentID != nil {
		fmt.Fprintf(&b, "Parent: %s\n", *task.ParentID)
	}
	if len(task.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join([]string(task.Tags), ", "))
	}
	writeLifecycleSection(&b, task, agent.ID)
	if repo != nil {
		fmt.Fprintf(&b, "\n=== WORKING REPO ===\n%s\nDefault branch: %s\n", repo.LocalPath, repo.DefaultBranch)
	}
	fmt.Fprintf(&b, "\n=== RECENT COMMENTS ===\n")
	for _, comment := range comments {
		fmt.Fprintf(&b, "[%s] %s\n", comment.Author, comment.Body)
	}
	b.WriteString(`

Do not run or suggest shell forms that fetch remote code or bypass local review, including curl, wget, curl|sh, python -c, python3 -c, perl -e, sudo, or Docker socket access.

Respond ONLY with a single JSON object matching this schema. No markdown, prose, or code fences:
{
  "task_updates": {
    "status": "todo|in_progress|in_review|blocked|done",
    "comment": "required summary, max 2000 chars",
    "reassign_to": "pm|em|backend|frontend|qa|<custom>|null",
    "request_human_review": false,
    "keep_worktree": false,
    "create_subtasks": [
      {
        "title": "string",
        "description": "string",
        "assignee_agent_id": "backend",
        "initial_comment": "string"
      }
    ],
    "attachments": [
      {"kind": "file|log", "path": "relative/path/in/worktree", "note": "string"}
    ]
  }
}

Routing notes:
- If you set "reassign_to", that explicit choice overrides the lifecycle.
- If you omit "reassign_to" and set status to "done", the task auto-advances to
  the next non-skipped step shown under "PLANNED REMAINING STEPS" below.
- Use "request_human_review" when you need a human to inspect before advancing.`)
	return b.String()
}

func writeLifecycleSection(b *strings.Builder, task models.Task, currentAgent string) {
	cfg, err := repoconfig.Load()
	if err != nil {
		return
	}
	lifecycleID := task.LifecycleID
	if lifecycleID == "" {
		lifecycleID = repoconfig.DefaultLifecycleID
	}
	remaining := cfg.RemainingSteps(lifecycleID, currentAgent, []string(task.Tags))
	fmt.Fprintf(b, "\n=== LIFECYCLE ===\nID: %s\nCurrent agent: %s\n", lifecycleID, currentAgent)
	if len(remaining) == 0 {
		b.WriteString("Planned remaining steps: (none — this is the final step)\n")
		return
	}
	b.WriteString("Planned remaining steps (after you finish):\n")
	for i, step := range remaining {
		fmt.Fprintf(b, "  %d. %s\n", i+1, step.Agent)
	}
}
