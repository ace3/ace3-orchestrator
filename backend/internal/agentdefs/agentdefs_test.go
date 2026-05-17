package agentdefs

import (
	"testing"
)

func TestLoadReturnsRepoAgents(t *testing.T) {
	defs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) == 0 {
		t.Fatal("expected at least one agent definition")
	}
}

func TestFindByID(t *testing.T) {
	def, err := Find("qa")
	if err != nil {
		t.Fatal(err)
	}
	if def.ID != "qa" || def.BasePrompt == "" {
		t.Fatalf("unexpected definition: %+v", def)
	}
}

func TestFindUnknownAgent(t *testing.T) {
	if _, err := Find("does-not-exist"); err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestHashIsStable(t *testing.T) {
	def, err := Find("pm")
	if err != nil {
		t.Fatal(err)
	}
	a := Hash(def)
	b := Hash(def)
	if a != b {
		t.Fatal("hash must be deterministic")
	}
}
