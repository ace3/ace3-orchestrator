# Simulation Task Catalog

Use these examples from the project board's **Add task** form. They are meant to exercise orchestration behavior without touching files or running implementation commands.

Default form values unless a scenario says otherwise:

- **Assignee:** `PM Agent`
- **Lifecycle:** `default`
- **Priority:** `0`
- **Tags:** `skip-planning, no-backend, no-frontend, skip-qa`

Human-in-loop rules:

- `approval_request` and `request_confirmation` show **Accept** and **Reject**.
- `ask_user_questions` shows **Answer**.
- Human interaction tasks should land in `waiting`.
- A normal comment is only history. It does not resolve an interaction.
- If the interaction uses `continuation_policy: wake_assignee`, resolving it queues the agent again.

## 1. Basic Approval Request

**Title**

```text
Simulation: basic approval request
```

**Description**

```text
Simulation only. Do not modify files or run implementation commands.

Create one human_interactions item:
kind: approval_request
title: Continue simulation?
summary: Ask the human to approve before this simulated workflow continues.
payload.question: Should the orchestrator continue this simulation?
continuation_policy: wake_assignee
idempotency_key: simulation-basic-approval

Set the task status to waiting until the human responds.
```

Expected human action:

- Click **Accept** to continue.
- Click **Reject** to send it back to the agent.

## 2. Rejection and Artifact Revision

**Title**

```text
Simulation: reject artifact and request revision
```

**Description**

```text
Simulation only. Do not modify files or run implementation commands.

Create an artifact:
kind: pm_document
title: Approval Draft v1
format: markdown
body: A short approval draft that intentionally needs human review.

Then create one human_interactions item:
kind: approval_request
title: Review approval draft
summary: Ask the human to approve or reject the Approval Draft v1 artifact.
payload.question: Should this approval draft be accepted as-is?
continuation_policy: wake_assignee
idempotency_key: simulation-reject-artifact-v1

Set the task status to waiting until the human responds.
```

Reject with:

```text
Not approved. Revise the artifact with clear acceptance criteria, risks, and rejection-handling steps. Then request approval again.
```

Expected agent behavior after rejection:

- Read the rejection note.
- Create or update an artifact, for example `Approval Draft v2`.
- Create another `approval_request`.
- Keep the task in `waiting`.

## 3. Human Question

**Title**

```text
Simulation: ask human a routing question
```

**Description**

```text
Simulation only. Do not modify files or run implementation commands.

Create one human_interactions item:
kind: ask_user_questions
title: Choose simulation path
summary: Ask which path the simulated workflow should take next.
payload.question: Should the simulated task continue as approve-only, reject-and-revise, or stop?
continuation_policy: wake_assignee
idempotency_key: simulation-routing-question

Set the task status to waiting until the human answers.
```

Answer with:

```text
Use reject-and-revise. Create a draft artifact, ask for approval, and wait for human review.
```

## 4. Confirmation Before Continuing

**Title**

```text
Simulation: confirmation gate
```

**Description**

```text
Simulation only. Do not modify files or run implementation commands.

Create one human_interactions item:
kind: request_confirmation
title: Confirm simulated continuation
summary: Confirm whether the simulated workflow should continue to the next step.
payload.question: Confirm that the simulation should continue to a final done state.
continuation_policy: wake_assignee
idempotency_key: simulation-confirm-continuation

Set the task status to waiting until the human confirms or rejects.
```

Expected human action:

- Click **Accept** to continue.
- Click **Reject** with a note to change direction.

## 5. Approval With Explicit Stop on Rejection

**Title**

```text
Simulation: approval with stop option
```

**Description**

```text
Simulation only. Do not modify files or run implementation commands.

Create one human_interactions item:
kind: approval_request
title: Approve final simulated result
summary: Ask the human whether the simulated result should be accepted or stopped.
payload.question: Approve the simulated result, or reject it and ask the agent to stop?
continuation_policy: wake_assignee
idempotency_key: simulation-approval-stop-option

Set the task status to waiting until the human responds.
```

Reject with:

```text
Rejected. Do not revise. Mark the simulation blocked with a short explanation.
```

Expected agent behavior after rejection:

- Stop revising.
- Mark the task `blocked`.
- Record the reason in the comment.

## 6. Multi-Artifact Review

**Title**

```text
Simulation: review multiple artifacts
```

**Description**

```text
Simulation only. Do not modify files or run implementation commands.

Create two artifacts:
1. kind: pm_document, title: Simulation Scope, format: markdown, body: A short scope document.
2. kind: em_document, title: Simulation Implementation Notes, format: markdown, body: A short implementation-note document.

Then create one human_interactions item:
kind: approval_request
title: Review simulation artifacts
summary: Ask the human to approve or reject both artifacts together.
payload.question: Are both simulation artifacts acceptable?
continuation_policy: wake_assignee
idempotency_key: simulation-multi-artifact-review

Set the task status to waiting until the human responds.
```

Reject with:

```text
Rejecting both artifacts. The scope needs acceptance criteria and the implementation notes need a rollback section.
```

## 7. Human Review Fallback

This tests the fallback path where the agent uses `request_human_review` instead of a richer `human_interactions` item.

**Title**

```text
Simulation: generic human review fallback
```

**Description**

```text
Simulation only. Do not modify files or run implementation commands.

Respond with task_updates.request_human_review=true, do not create a human_interactions item, and explain that generic approval is required.

The orchestrator should convert this into an approval interaction and set the task to waiting.
```

Expected human action:

- Use **Accept** or **Reject** on the generated approval interaction.

## 8. Revision Loop Until Approved

**Title**

```text
Simulation: revision loop until approved
```

**Description**

```text
Simulation only. Do not modify files or run implementation commands.

Create an artifact:
kind: pm_document
title: Revision Loop Draft v1
format: markdown
body: A deliberately incomplete draft for review.

Create one human_interactions item:
kind: approval_request
title: Review revision loop draft
summary: Ask the human to approve or reject the draft. If rejected, revise the artifact and request approval again with the next version.
payload.question: Is Revision Loop Draft v1 approved?
continuation_policy: wake_assignee
idempotency_key: simulation-revision-loop-v1

Set the task status to waiting until the human responds.
```

Reject with:

```text
Not approved. Add measurable acceptance criteria and rename the revised artifact to Revision Loop Draft v2.
```

Expected agent behavior after rejection:

- Create `Revision Loop Draft v2`.
- Create a new approval interaction with a new idempotency key, for example `simulation-revision-loop-v2`.
- Keep the task waiting.

Approve with:

```text
Approved. Mark the simulation done.
```

## 9. Final Approval to Done

**Title**

```text
Simulation: final approval to done
```

**Description**

```text
Simulation only. Do not modify files or run implementation commands.

Create one artifact:
kind: implementation_note
title: Final Simulation Result
format: markdown
body: A concise final result summary.

Create one human_interactions item:
kind: approval_request
title: Final approval
summary: Ask the human to approve completion.
payload.question: Can this simulation be marked done?
continuation_policy: wake_assignee
idempotency_key: simulation-final-approval

Set the task status to waiting until the human responds.
```

Accept with:

```text
Approved. Mark this task done.
```

Expected agent behavior after acceptance:

- Mark the task `done`.

## Troubleshooting

If you only see an agent comment like `[HUMAN REVIEW REQUESTED]` and no interaction card:

1. Restart the backend so the latest code is running.
2. Click **Run now** once.
3. Wait until the run finishes.
4. Reopen or refresh the task drawer.

If the task shows `running`, wait for the run to finish before looking for the interaction controls.

If there is still no interaction card, inspect the latest log. The agent may have returned a comment/artifact only and failed to include a valid `human_interactions` item.
