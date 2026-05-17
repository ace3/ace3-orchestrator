package agentdefs

import (
	"strings"
	"testing"
)

func TestParseValidDefinitions(t *testing.T) {
	defs, err := Parse([]byte(`
agents:
  - id: qa
    name: QA Agent
    role: qa
    cli_kind: codex
    base_prompt: Verify behavior.
    skills: [qa-tester]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].ID != "qa" || defs[0].BasePrompt != "Verify behavior." {
		t.Fatalf("unexpected definitions: %+v", defs)
	}
}

func TestParseRejectsDuplicateIDs(t *testing.T) {
	_, err := Parse([]byte(`
agents:
  - id: qa
    name: QA Agent
    role: qa
    cli_kind: codex
    base_prompt: Verify behavior.
  - id: qa
    name: QA Copy
    role: qa
    cli_kind: codex
    base_prompt: Verify behavior again.
`))
	if err == nil || !strings.Contains(err.Error(), `duplicate agent definition id "qa"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRejectsMissingRequiredFields(t *testing.T) {
	_, err := Parse([]byte(`
agents:
  - id: qa
    name: QA Agent
    role: qa
    cli_kind: codex
`))
	if err == nil || !strings.Contains(err.Error(), "missing id, name, role, or base_prompt") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRejectsInvalidCLIKind(t *testing.T) {
	_, err := Parse([]byte(`
agents:
  - id: qa
    name: QA Agent
    role: qa
    cli_kind: other
    base_prompt: Verify behavior.
`))
	if err == nil || !strings.Contains(err.Error(), `invalid cli_kind "other"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
