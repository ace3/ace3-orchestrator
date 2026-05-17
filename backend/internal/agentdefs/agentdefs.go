package agentdefs

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"mini-paperclip/backend/internal/models"
)

//go:embed agents.yaml
var definitionsFile embed.FS

type Definition struct {
	ID         string   `yaml:"id"`
	Name       string   `yaml:"name"`
	Role       string   `yaml:"role"`
	CLIKind    string   `yaml:"cli_kind"`
	BasePrompt string   `yaml:"base_prompt"`
	Skills     []string `yaml:"skills"`
}

type fileConfig struct {
	Agents []Definition `yaml:"agents"`
}

func Load() ([]Definition, error) {
	body, err := definitionsFile.ReadFile("agents.yaml")
	if err != nil {
		return nil, err
	}
	return Parse(body)
}

func Parse(body []byte) ([]Definition, error) {
	var cfg fileConfig
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range cfg.Agents {
		def := &cfg.Agents[i]
		def.ID = strings.TrimSpace(def.ID)
		def.Name = strings.TrimSpace(def.Name)
		def.Role = strings.TrimSpace(def.Role)
		def.CLIKind = strings.TrimSpace(def.CLIKind)
		def.BasePrompt = strings.TrimSpace(def.BasePrompt)
		if def.ID == "" || def.Name == "" || def.Role == "" || def.BasePrompt == "" {
			return nil, fmt.Errorf("agent definition %d is missing id, name, role, or base_prompt", i)
		}
		if def.CLIKind != "claude" && def.CLIKind != "codex" {
			return nil, fmt.Errorf("agent definition %q has invalid cli_kind %q", def.ID, def.CLIKind)
		}
		if seen[def.ID] {
			return nil, fmt.Errorf("duplicate agent definition id %q", def.ID)
		}
		seen[def.ID] = true
		for j := range def.Skills {
			def.Skills[j] = strings.TrimSpace(def.Skills[j])
			if def.Skills[j] == "" {
				return nil, fmt.Errorf("agent definition %q has an empty skill name", def.ID)
			}
		}
	}
	if len(cfg.Agents) == 0 {
		return nil, errors.New("agent definitions file has no agents")
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
	defs, err := Load()
	if err != nil {
		return Definition{}, err
	}
	def, ok := ByID(defs)[id]
	if !ok {
		return Definition{}, fmt.Errorf("agent %q is not repo-defined", id)
	}
	return def, nil
}

func Hash(def Definition) string {
	body, _ := yaml.Marshal(def)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
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
