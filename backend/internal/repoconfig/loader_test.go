package repoconfig

import "testing"

func TestLoadEmbeddedConfig(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Agents) == 0 {
		t.Fatal("expected at least one agent")
	}
	if len(cfg.Skills) == 0 {
		t.Fatal("expected at least one skill")
	}
	if _, ok := cfg.LifecycleByID(DefaultLifecycleID); !ok {
		t.Fatalf("missing %q lifecycle", DefaultLifecycleID)
	}
}

func TestLoadIsCached(t *testing.T) {
	a, _ := Load()
	b, _ := Load()
	if a != b {
		t.Fatal("Load must return the same cached pointer")
	}
}

func TestAgentSkillsResolveToCatalog(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	catalog := make(map[string]bool, len(cfg.Skills))
	for _, sk := range cfg.Skills {
		catalog[sk.Name] = true
	}
	for _, agent := range cfg.Agents {
		for _, name := range agent.Skills {
			if !catalog[name] {
				t.Fatalf("agent %q references skill %q not in catalog", agent.ID, name)
			}
		}
	}
}

func TestLifecycleStepsReferenceKnownAgents(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, lc := range cfg.Lifecycles {
		for i, step := range lc.Steps {
			if _, ok := cfg.AgentByID(step.Agent); !ok {
				t.Fatalf("lifecycle %q step %d references unknown agent %q", lc.ID, i, step.Agent)
			}
		}
	}
}

func TestValidateRejectsUnknownSkillOnAgent(t *testing.T) {
	cfg := &Config{
		Agents: []Agent{
			{ID: "x", Name: "X", Role: "x", CLIKind: "claude", BasePrompt: "p", Skills: []string{"missing"}},
		},
		Skills: []Skill{},
		Lifecycles: []Lifecycle{
			{ID: DefaultLifecycleID, Steps: []LifecycleStep{{Agent: "x"}}},
		},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for unknown skill")
	}
}

func TestValidateRequiresDefaultLifecycle(t *testing.T) {
	cfg := &Config{
		Agents: []Agent{
			{ID: "x", Name: "X", Role: "x", CLIKind: "claude", BasePrompt: "p"},
		},
		Lifecycles: []Lifecycle{
			{ID: "custom", Steps: []LifecycleStep{{Agent: "x"}}},
		},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error when default lifecycle is missing")
	}
}

func TestValidateRejectsUnknownAgentInLifecycle(t *testing.T) {
	cfg := &Config{
		Agents: []Agent{
			{ID: "x", Name: "X", Role: "x", CLIKind: "claude", BasePrompt: "p"},
		},
		Lifecycles: []Lifecycle{
			{ID: DefaultLifecycleID, Steps: []LifecycleStep{{Agent: "ghost"}}},
		},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for unknown agent in lifecycle")
	}
}
