// Package repoconfig loads the flat-file, repo-locked orchestrator configuration:
// agents (base prompts + default skill bindings), the skill catalog, and lifecycle
// templates with per-step skip rules driven by task tags. Files are embedded into
// the binary so the runtime cannot drift from the committed config.
package repoconfig

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed agents.json skills.json lifecycles.json
var files embed.FS

const DefaultLifecycleID = "default"

type Agent struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Role       string   `json:"role"`
	CLIKind    string   `json:"cli_kind"`
	BasePrompt string   `json:"base_prompt"`
	Skills     []string `json:"skills"`
}

type SkillSource struct {
	Name        string `json:"name"`
	UpstreamURL string `json:"upstream_url"`
	PinnedSHA   string `json:"pinned_sha"`
	Kind        string `json:"kind"`
}

type Skill struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	PathInSource string `json:"path_in_source"`
	Version      string `json:"version"`
	Archived     bool   `json:"archived"`
}

type LifecycleStep struct {
	Agent    string   `json:"agent"`
	SkipWhen []string `json:"skip_when"`
}

type Lifecycle struct {
	ID          string          `json:"id"`
	Description string          `json:"description"`
	Steps       []LifecycleStep `json:"steps"`
}

type Config struct {
	Agents       []Agent
	SkillSources []SkillSource
	Skills       []Skill
	Lifecycles   []Lifecycle
}

type agentsFile struct {
	Agents []Agent `json:"agents"`
}

type skillsFile struct {
	Sources []SkillSource `json:"sources"`
	Skills  []Skill       `json:"skills"`
}

type lifecyclesFile struct {
	Lifecycles []Lifecycle `json:"lifecycles"`
}

var (
	cacheOnce sync.Once
	cacheCfg  *Config
	cacheErr  error
)

// Load parses and validates the embedded config files. The result is cached;
// the embedded JSON cannot change at runtime so repeated calls are free.
func Load() (*Config, error) {
	cacheOnce.Do(func() {
		cacheCfg, cacheErr = parseAll()
	})
	return cacheCfg, cacheErr
}

// MustLoad panics on error. Use only at startup wiring.
func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		panic(fmt.Errorf("repoconfig: %w", err))
	}
	return cfg
}

func parseAll() (*Config, error) {
	agents, err := parseAgents()
	if err != nil {
		return nil, err
	}
	sources, skills, err := parseSkills()
	if err != nil {
		return nil, err
	}
	lifecycles, err := parseLifecycles()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Agents:       agents,
		SkillSources: sources,
		Skills:       skills,
		Lifecycles:   lifecycles,
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseAgents() ([]Agent, error) {
	body, err := files.ReadFile("agents.json")
	if err != nil {
		return nil, err
	}
	var f agentsFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("agents.json: %w", err)
	}
	for i := range f.Agents {
		a := &f.Agents[i]
		a.ID = strings.TrimSpace(a.ID)
		a.Name = strings.TrimSpace(a.Name)
		a.Role = strings.TrimSpace(a.Role)
		a.CLIKind = strings.TrimSpace(a.CLIKind)
		a.BasePrompt = strings.TrimSpace(a.BasePrompt)
		for j := range a.Skills {
			a.Skills[j] = strings.TrimSpace(a.Skills[j])
		}
	}
	return f.Agents, nil
}

func parseSkills() ([]SkillSource, []Skill, error) {
	body, err := files.ReadFile("skills.json")
	if err != nil {
		return nil, nil, err
	}
	var f skillsFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, nil, fmt.Errorf("skills.json: %w", err)
	}
	for i := range f.Sources {
		f.Sources[i].Name = strings.TrimSpace(f.Sources[i].Name)
		f.Sources[i].Kind = strings.TrimSpace(f.Sources[i].Kind)
	}
	for i := range f.Skills {
		f.Skills[i].Name = strings.TrimSpace(f.Skills[i].Name)
		f.Skills[i].Source = strings.TrimSpace(f.Skills[i].Source)
	}
	return f.Sources, f.Skills, nil
}

func parseLifecycles() ([]Lifecycle, error) {
	body, err := files.ReadFile("lifecycles.json")
	if err != nil {
		return nil, err
	}
	var f lifecyclesFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("lifecycles.json: %w", err)
	}
	for i := range f.Lifecycles {
		f.Lifecycles[i].ID = strings.TrimSpace(f.Lifecycles[i].ID)
		for j := range f.Lifecycles[i].Steps {
			f.Lifecycles[i].Steps[j].Agent = strings.TrimSpace(f.Lifecycles[i].Steps[j].Agent)
			for k := range f.Lifecycles[i].Steps[j].SkipWhen {
				f.Lifecycles[i].Steps[j].SkipWhen[k] = strings.TrimSpace(f.Lifecycles[i].Steps[j].SkipWhen[k])
			}
		}
	}
	return f.Lifecycles, nil
}

func (c *Config) validate() error {
	if len(c.Agents) == 0 {
		return fmt.Errorf("agents.json: no agents defined")
	}
	agentIDs := map[string]bool{}
	for i, a := range c.Agents {
		if a.ID == "" || a.Name == "" || a.Role == "" || a.BasePrompt == "" {
			return fmt.Errorf("agents.json[%d]: missing id, name, role, or base_prompt", i)
		}
		if a.CLIKind != "claude" && a.CLIKind != "codex" {
			return fmt.Errorf("agents.json[%q]: invalid cli_kind %q", a.ID, a.CLIKind)
		}
		if agentIDs[a.ID] {
			return fmt.Errorf("agents.json: duplicate agent id %q", a.ID)
		}
		agentIDs[a.ID] = true
	}

	sourceNames := map[string]bool{}
	for _, s := range c.SkillSources {
		if s.Name == "" {
			return fmt.Errorf("skills.json: source name required")
		}
		sourceNames[s.Name] = true
	}
	skillNames := map[string]bool{}
	for _, sk := range c.Skills {
		if sk.Name == "" {
			return fmt.Errorf("skills.json: skill name required")
		}
		if sk.Source != "" && !sourceNames[sk.Source] {
			return fmt.Errorf("skills.json: skill %q references unknown source %q", sk.Name, sk.Source)
		}
		if skillNames[sk.Name] {
			return fmt.Errorf("skills.json: duplicate skill name %q", sk.Name)
		}
		skillNames[sk.Name] = true
	}
	for _, a := range c.Agents {
		for _, name := range a.Skills {
			if name == "" {
				return fmt.Errorf("agent %q: empty skill name", a.ID)
			}
			if !skillNames[name] {
				return fmt.Errorf("agent %q: references unknown skill %q", a.ID, name)
			}
		}
	}

	if len(c.Lifecycles) == 0 {
		return fmt.Errorf("lifecycles.json: no lifecycles defined")
	}
	lifecycleIDs := map[string]bool{}
	hasDefault := false
	for _, lc := range c.Lifecycles {
		if lc.ID == "" {
			return fmt.Errorf("lifecycles.json: lifecycle id required")
		}
		if lifecycleIDs[lc.ID] {
			return fmt.Errorf("lifecycles.json: duplicate lifecycle id %q", lc.ID)
		}
		lifecycleIDs[lc.ID] = true
		if lc.ID == DefaultLifecycleID {
			hasDefault = true
		}
		if len(lc.Steps) == 0 {
			return fmt.Errorf("lifecycle %q: must have at least one step", lc.ID)
		}
		for i, step := range lc.Steps {
			if step.Agent == "" {
				return fmt.Errorf("lifecycle %q step %d: agent required", lc.ID, i)
			}
			if !agentIDs[step.Agent] {
				return fmt.Errorf("lifecycle %q step %d: unknown agent %q", lc.ID, i, step.Agent)
			}
		}
	}
	if !hasDefault {
		return fmt.Errorf("lifecycles.json: missing lifecycle %q", DefaultLifecycleID)
	}
	return nil
}

// AgentByID returns the agent definition or false if not found.
func (c *Config) AgentByID(id string) (Agent, bool) {
	for _, a := range c.Agents {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}

// LifecycleByID returns the lifecycle template or false if not found.
func (c *Config) LifecycleByID(id string) (Lifecycle, bool) {
	for _, lc := range c.Lifecycles {
		if lc.ID == id {
			return lc, true
		}
	}
	return Lifecycle{}, false
}

// SkillsByName maps each name in lookup to its config entry. Missing names
// are returned in the second slice so callers can decide whether to fail.
func (c *Config) SkillsByName(lookup []string) (map[string]Skill, []string) {
	idx := make(map[string]Skill, len(c.Skills))
	for _, sk := range c.Skills {
		idx[sk.Name] = sk
	}
	out := make(map[string]Skill, len(lookup))
	var missing []string
	for _, name := range lookup {
		sk, ok := idx[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		out[name] = sk
	}
	return out, missing
}

// AgentHash returns a stable digest of an agent definition, used to record the
// exact prompt the runtime executed under.
func AgentHash(a Agent) string {
	body, _ := json.Marshal(a)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
