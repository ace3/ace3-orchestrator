package repoconfig

import "testing"

func newTestConfig() *Config {
	return &Config{
		Agents: []Agent{
			{ID: "pm", Name: "PM", Role: "pm", CLIKind: "claude", BasePrompt: "p"},
			{ID: "em", Name: "EM", Role: "em", CLIKind: "claude", BasePrompt: "p"},
			{ID: "backend", Name: "BE", Role: "backend", CLIKind: "claude", BasePrompt: "p"},
			{ID: "frontend", Name: "FE", Role: "frontend", CLIKind: "claude", BasePrompt: "p"},
			{ID: "qa", Name: "QA", Role: "qa", CLIKind: "claude", BasePrompt: "p"},
		},
		Lifecycles: []Lifecycle{
			{
				ID: DefaultLifecycleID,
				Steps: []LifecycleStep{
					{Agent: "pm"},
					{Agent: "em"},
					{Agent: "backend", SkipWhen: []string{"frontend-only", "no-backend"}},
					{Agent: "frontend", SkipWhen: []string{"backend-only", "no-frontend"}},
					{Agent: "qa", SkipWhen: []string{"skip-qa"}},
				},
			},
		},
	}
}

func TestNextAgentLinearFlow(t *testing.T) {
	cfg := newTestConfig()
	steps := []struct {
		current string
		want    string
		wantEnd bool
	}{
		{"", "pm", false},
		{"pm", "em", false},
		{"em", "backend", false},
		{"backend", "frontend", false},
		{"frontend", "qa", false},
		{"qa", "", true},
	}
	for _, tc := range steps {
		next, done, err := cfg.NextAgent(DefaultLifecycleID, tc.current, nil)
		if err != nil {
			t.Fatalf("from %q: %v", tc.current, err)
		}
		if done != tc.wantEnd || next != tc.want {
			t.Fatalf("from %q: got (%q,%v), want (%q,%v)", tc.current, next, done, tc.want, tc.wantEnd)
		}
	}
}

func TestNextAgentSkipsFrontendWhenTagged(t *testing.T) {
	cfg := newTestConfig()
	next, done, err := cfg.NextAgent(DefaultLifecycleID, "backend", []string{"backend-only"})
	if err != nil {
		t.Fatal(err)
	}
	if done || next != "qa" {
		t.Fatalf("got (%q,%v), want (qa,false)", next, done)
	}
}

func TestNextAgentSkipsMultipleStepsWithMultiTags(t *testing.T) {
	cfg := newTestConfig()
	next, done, err := cfg.NextAgent(DefaultLifecycleID, "em", []string{"frontend-only", "skip-qa"})
	if err != nil {
		t.Fatal(err)
	}
	if done || next != "frontend" {
		t.Fatalf("got (%q,%v), want (frontend,false)", next, done)
	}
	next, done, err = cfg.NextAgent(DefaultLifecycleID, "frontend", []string{"frontend-only", "skip-qa"})
	if err != nil {
		t.Fatal(err)
	}
	if !done || next != "" {
		t.Fatalf("after frontend: got (%q,%v), want done", next, done)
	}
}

func TestNextAgentSkipAlwaysToken(t *testing.T) {
	cfg := &Config{
		Agents: []Agent{
			{ID: "a", Name: "A", Role: "a", CLIKind: "claude", BasePrompt: "p"},
			{ID: "b", Name: "B", Role: "b", CLIKind: "claude", BasePrompt: "p"},
		},
		Lifecycles: []Lifecycle{
			{ID: DefaultLifecycleID, Steps: []LifecycleStep{
				{Agent: "a", SkipWhen: []string{SkipAlways}},
				{Agent: "b"},
			}},
		},
	}
	next, done, err := cfg.NextAgent(DefaultLifecycleID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if done || next != "b" {
		t.Fatalf("got (%q,%v), want (b,false)", next, done)
	}
}

func TestNextAgentUnknownLifecycle(t *testing.T) {
	cfg := newTestConfig()
	if _, _, err := cfg.NextAgent("nope", "", nil); err == nil {
		t.Fatal("expected error for unknown lifecycle")
	}
}

func TestNextAgentEmptyLifecycleIDUsesDefault(t *testing.T) {
	cfg := newTestConfig()
	next, done, err := cfg.NextAgent("", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if done || next != "pm" {
		t.Fatalf("got (%q,%v), want (pm,false)", next, done)
	}
}

func TestNextAgentUnknownCurrentRestartsFromTop(t *testing.T) {
	cfg := newTestConfig()
	next, done, err := cfg.NextAgent(DefaultLifecycleID, "custom-agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if done || next != "pm" {
		t.Fatalf("got (%q,%v), want (pm,false) when current agent is not in lifecycle", next, done)
	}
}

func TestRemainingStepsRespectsTags(t *testing.T) {
	cfg := newTestConfig()
	got := cfg.RemainingSteps(DefaultLifecycleID, "em", []string{"backend-only"})
	if len(got) != 2 || got[0].Agent != "backend" || got[1].Agent != "qa" {
		t.Fatalf("unexpected remaining steps: %+v", got)
	}
}
