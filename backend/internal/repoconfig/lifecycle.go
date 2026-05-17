package repoconfig

import "fmt"

// SkipAlways disables a lifecycle step entirely without removing it from the file.
const SkipAlways = "always"

// NextAgent returns the next agent id in the lifecycle after currentAgent,
// honoring per-step skip_when rules against taskTags. If currentAgent is empty,
// the search starts at the first step. done=true means no remaining step
// applies — the lifecycle is finished for this task.
func (c *Config) NextAgent(lifecycleID, currentAgent string, taskTags []string) (next string, done bool, err error) {
	if lifecycleID == "" {
		lifecycleID = DefaultLifecycleID
	}
	lc, ok := c.LifecycleByID(lifecycleID)
	if !ok {
		return "", false, fmt.Errorf("unknown lifecycle %q", lifecycleID)
	}
	startIdx := 0
	if currentAgent != "" {
		found := false
		for i, step := range lc.Steps {
			if step.Agent == currentAgent {
				startIdx = i + 1
				found = true
				break
			}
		}
		// If the current agent isn't in this lifecycle (e.g. a custom assignee),
		// walk from the beginning so the next runnable step can still pick up.
		if !found {
			startIdx = 0
		}
	}
	tagSet := make(map[string]bool, len(taskTags))
	for _, t := range taskTags {
		tagSet[t] = true
	}
	for i := startIdx; i < len(lc.Steps); i++ {
		if shouldSkip(lc.Steps[i].SkipWhen, tagSet) {
			continue
		}
		return lc.Steps[i].Agent, false, nil
	}
	return "", true, nil
}

// RemainingSteps returns the slice of steps from (and including) currentAgent's
// successor that would not be skipped given taskTags. Used to surface the planned
// downstream flow to the agent prompt.
func (c *Config) RemainingSteps(lifecycleID, currentAgent string, taskTags []string) []LifecycleStep {
	if lifecycleID == "" {
		lifecycleID = DefaultLifecycleID
	}
	lc, ok := c.LifecycleByID(lifecycleID)
	if !ok {
		return nil
	}
	startIdx := 0
	if currentAgent != "" {
		for i, step := range lc.Steps {
			if step.Agent == currentAgent {
				startIdx = i + 1
				break
			}
		}
	}
	tagSet := make(map[string]bool, len(taskTags))
	for _, t := range taskTags {
		tagSet[t] = true
	}
	out := make([]LifecycleStep, 0, len(lc.Steps)-startIdx)
	for i := startIdx; i < len(lc.Steps); i++ {
		if shouldSkip(lc.Steps[i].SkipWhen, tagSet) {
			continue
		}
		out = append(out, lc.Steps[i])
	}
	return out
}

func shouldSkip(skipWhen []string, tagSet map[string]bool) bool {
	for _, token := range skipWhen {
		if token == SkipAlways {
			return true
		}
		if tagSet[token] {
			return true
		}
	}
	return false
}
