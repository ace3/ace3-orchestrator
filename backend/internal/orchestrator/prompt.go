package orchestrator

import (
	"fmt"
	"strings"

	"mini-paperclip/backend/internal/models"
	"mini-paperclip/backend/internal/repoconfig"
)

type SkillDoc struct {
	Skill   models.Skill
	Source  string
	Path    string
	Reason  string
	Content string
}

func BuildPrompt(agent models.Agent, task models.Task, repo *models.Repo, comments []models.Comment, artifacts []models.TaskArtifact) string {
	return BuildPromptWithSkillDocs(agent, task, repo, comments, artifacts, nil)
}

func BuildPromptWithSkillDocs(agent models.Agent, task models.Task, repo *models.Repo, comments []models.Comment, artifacts []models.TaskArtifact, skillDocs []SkillDoc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== AGENT ===\nID: %s\nName: %s\nRole: %s\n\n", agent.ID, agent.Name, agent.Role)
	fmt.Fprintf(&b, "=== ACTIVE SKILLS ===\n")
	if len(skillDocs) > 0 {
		for _, doc := range skillDocs {
			reason := doc.Reason
			if reason == "" {
				reason = "assigned"
			}
			fmt.Fprintf(&b, "- %s (%s)\n", doc.Skill.Name, reason)
		}
	} else {
		for _, skill := range agent.Skills {
			fmt.Fprintf(&b, "- %s (assigned)\n", skill.Name)
		}
	}
	writeSkillDocs(&b, skillDocs)
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
	fmt.Fprintf(&b, "\n=== TASK ARTIFACTS ===\n")
	if len(artifacts) == 0 {
		b.WriteString("(none)\n")
	}
	for _, artifact := range artifacts {
		fmt.Fprintf(&b, "[%s] %s by %s\n", artifact.Kind, artifact.Title, artifact.CreatedBy)
		if artifact.Body != "" {
			body := artifact.Body
			if len(body) > 3000 {
				body = body[:3000] + "\n[truncated]"
			}
			fmt.Fprintf(&b, "%s\n", body)
		}
	}
	b.WriteString(`

Use only the active skills and skill instructions embedded in this prompt for this run. Do not invoke, assume, or rely on globally installed Codex or Claude skills/plugins unless the embedded active skill instructions explicitly require it and it is necessary to complete the task.

Do not run or suggest shell forms that fetch remote code or bypass local review, including curl, wget, curl|sh, python -c, python3 -c, perl -e, sudo, or Docker socket access.

Respond ONLY with a single JSON object matching this schema. No markdown, prose, or code fences:
{
  "task_updates": {
    "status": "todo|in_progress|in_review|blocked|done",
    "comment": "required summary, max 2000 chars",
    "reassign_to": "pm|em|backend|frontend|qa|<custom>|null",
    "tags": ["frontend-only|backend-only|no-backend|no-frontend|skip-qa|skip-planning|needs-research|needs-tests|needs-browser"],
    "lifecycle_id": "default|backend-only|frontend-only|null",
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
      {"kind": "pm_document|pm_handoff|em_document|em_handoff|qa_report|implementation_note|run_log|other|file|log", "title": "string", "body": "string", "format": "markdown|text|json", "path": "relative/path/in/worktree", "note": "string", "metadata": {}}
    ]
  }
}

Routing notes:
- If you set "reassign_to", that explicit choice overrides the lifecycle.
- Use "tags" and "lifecycle_id" to make the lifecycle skip irrelevant steps when the task is clearly backend-only, frontend-only, already planned, or does not need QA.
- If you omit "reassign_to" and set status to "done", the task auto-advances to
  the next non-skipped step shown under "PLANNED REMAINING STEPS" below.
- Use "request_human_review" when you need a human to inspect before advancing.
- Use "attachments" to persist PM docs, PM handoffs, EM docs, EM handoffs, QA reports, implementation notes, and run logs as durable task artifacts.
- Legacy "file" and "log" attachments are accepted and stored as metadata-only artifacts when no body is provided.`)
	return b.String()
}

func writeSkillDocs(b *strings.Builder, skillDocs []SkillDoc) {
	fmt.Fprintf(b, "\n=== ACTIVE SKILL INSTRUCTIONS ===\n")
	if len(skillDocs) == 0 {
		b.WriteString("(no SKILL.md content available)\n")
		return
	}
	for _, doc := range skillDocs {
		fmt.Fprintf(b, "--- %s", doc.Skill.Name)
		if doc.Reason != "" {
			fmt.Fprintf(b, " [%s]", doc.Reason)
		}
		if doc.Source != "" {
			fmt.Fprintf(b, " from %s", doc.Source)
		}
		if doc.Path != "" {
			fmt.Fprintf(b, " at %s", doc.Path)
		}
		b.WriteString(" ---\n")
		content := doc.Content
		if len(content) > 12000 {
			content = content[:12000] + "\n[truncated]"
		}
		b.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			b.WriteString("\n")
		}
	}
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
