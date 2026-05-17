// Package agentdefs is a thin shim over repoconfig that preserves the legacy
// surface used by bootstrap and the orchestrator. New code should import
// repoconfig directly. This package will be removed once all callers migrate.
package agentdefs

import (
	"fmt"

	"mini-paperclip/backend/internal/models"
	"mini-paperclip/backend/internal/repoconfig"
)

type Definition = repoconfig.Agent

func Load() ([]Definition, error) {
	cfg, err := repoconfig.Load()
	if err != nil {
		return nil, err
	}
	return cfg.Agents, nil
}

func ByID(defs []Definition) map[string]Definition {
	out := make(map[string]Definition, len(defs))
	for _, def := range defs {
		out[def.ID] = def
	}
	return out
}

func Find(id string) (Definition, error) {
	cfg, err := repoconfig.Load()
	if err != nil {
		return Definition{}, err
	}
	def, ok := cfg.AgentByID(id)
	if !ok {
		return Definition{}, fmt.Errorf("agent %q is not repo-defined", id)
	}
	return def, nil
}

func Hash(def Definition) string {
	return repoconfig.AgentHash(def)
}

func Apply(agent models.Agent, def Definition, skills []models.Skill) models.Agent {
	agent.ID = def.ID
	agent.Name = def.Name
	agent.Role = def.Role
	agent.CLIKind = def.CLIKind
	agent.RolePrompt = def.BasePrompt
	agent.BasePrompt = def.BasePrompt
	agent.DefinitionHash = Hash(def)
	agent.Skills = skills
	return agent
}
