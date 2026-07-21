package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefinitionKind  = "vessica.agent/v1"
	DefaultModel    = "gpt-5.6-terra"
	DefaultBudgetUS = "5.00"
)

var toolCatalog = map[string]bool{
	"openai.web_search": true, "openai.code_interpreter": true,
	"repository.list": true, "knowledge.retrieve": true,
	"artifact.list": true, "artifact.get": true, "artifact.create": true,
	"artifact.version": true, "artifact.activate": true, "artifact.supersede": true,
	"memory.list": true, "memory.get": true, "memory.search": true,
	"memory.create": true, "memory.version": true, "memory.supersede": true, "memory.archive": true,
	"entity.get": true, "entity.resolve": true, "entity.create": true,
	"epic.list": true, "epic.view": true, "epic.create": true,
	"coding_run.start": true, "coding_run.status": true, "coding_run.events": true,
	"agent.invoke": true,
}

type Definition struct {
	Kind              string               `json:"kind"`
	Name              string               `json:"name"`
	Purpose           string               `json:"purpose"`
	SystemPrompt      string               `json:"system_prompt"`
	Model             Model                `json:"model"`
	Tools             []Tool               `json:"tools,omitempty"`
	Knowledge         []KnowledgeReference `json:"knowledge,omitempty"`
	Heartbeat         *Heartbeat           `json:"heartbeat,omitempty"`
	Budget            *Budget              `json:"budget,omitempty"`
	EvalCriticAgentID string               `json:"eval_critic_agent_id,omitempty"`
}

type Model struct {
	ID              string `json:"id"`
	ReasoningEffort string `json:"reasoning_effort"`
}
type Tool struct {
	ID     string          `json:"id"`
	Config json.RawMessage `json:"config,omitempty"`
}
type KnowledgeReference struct {
	ArtifactID  string `json:"artifact_id"`
	Description string `json:"description"`
	Version     string `json:"version"`
}
type Heartbeat struct {
	Enabled  bool   `json:"enabled"`
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
}
type Budget struct {
	DailyUSD string `json:"daily_usd"`
	Timezone string `json:"timezone"`
}

func (d *Definition) Defaults(timezone string) {
	if d.Kind == "" {
		d.Kind = DefinitionKind
	}
	if d.Model.ID == "" {
		d.Model.ID = DefaultModel
	}
	if d.Model.ReasoningEffort == "" {
		d.Model.ReasoningEffort = "medium"
	}
	if timezone == "" {
		timezone = "UTC"
	}
	if d.Budget == nil {
		d.Budget = &Budget{DailyUSD: DefaultBudgetUS, Timezone: timezone}
	}
	if d.Budget.Timezone == "" {
		d.Budget.Timezone = timezone
	}
	if d.Heartbeat != nil && d.Heartbeat.Timezone == "" {
		d.Heartbeat.Timezone = timezone
	}
	for i := range d.Knowledge {
		if d.Knowledge[i].Version == "" {
			d.Knowledge[i].Version = "latest"
		}
	}
}

func (d Definition) Validate(modelAvailable func(string) bool) error {
	if d.Kind != DefinitionKind {
		return fmt.Errorf("kind must be %q", DefinitionKind)
	}
	if utf8.RuneCountInString(d.Name) < 1 || utf8.RuneCountInString(d.Name) > 64 {
		return fmt.Errorf("name must be 1-64 characters")
	}
	for _, r := range d.Name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("name cannot contain whitespace or control characters")
		}
	}
	if strings.TrimSpace(d.Purpose) == "" || utf8.RuneCountInString(d.Purpose) > 2000 {
		return fmt.Errorf("purpose is required and limited to 2000 characters")
	}
	if strings.TrimSpace(d.SystemPrompt) == "" || len([]byte(d.SystemPrompt)) > 64*1024 {
		return fmt.Errorf("system_prompt is required and limited to 64 KiB")
	}
	if modelAvailable != nil && !modelAvailable(d.Model.ID) {
		return fmt.Errorf("model %q is not available", d.Model.ID)
	}
	switch d.Model.ReasoningEffort {
	case "low", "medium", "high", "xhigh":
	default:
		return fmt.Errorf("invalid reasoning_effort %q", d.Model.ReasoningEffort)
	}
	seen := map[string]bool{}
	for _, t := range d.Tools {
		if !toolCatalog[t.ID] {
			return fmt.Errorf("unknown tool %q", t.ID)
		}
		if seen[t.ID] {
			return fmt.Errorf("duplicate tool %q", t.ID)
		}
		seen[t.ID] = true
	}
	for _, k := range d.Knowledge {
		if strings.TrimSpace(k.ArtifactID) == "" || strings.TrimSpace(k.Description) == "" {
			return fmt.Errorf("knowledge references require artifact_id and description")
		}
	}
	return nil
}

func ToolCatalog() []string {
	out := make([]string, 0, len(toolCatalog))
	for id := range toolCatalog {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
