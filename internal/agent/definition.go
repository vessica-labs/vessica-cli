package agent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefinitionKind              = "vessica.agent/v1"
	DefinitionKindV2            = "vessica.agent/v2"
	RuntimeTypeScriptAgentsSDK  = "typescript_agents_sdk"
	DefaultModel                = "gpt-5.6-terra"
	DefaultBudgetUS             = "5.00"
	DefaultConcurrency          = 1
	DefaultTimeoutSeconds       = 3600
	DefaultConversationMaxTurns = 25
)

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)

var toolCatalog = map[string]bool{
	"openai.web_search": true, "openai.code_interpreter": true,
	"repository.list": true, "knowledge.retrieve": true,
	"artifact.list": true, "artifact.get": true, "artifact.create": true,
	"artifact.version": true, "artifact.activate": true, "artifact.supersede": true,
	"memory.list": true, "memory.get": true, "memory.search": true,
	"memory.create": true, "memory.version": true, "memory.supersede": true, "memory.archive": true,
	"entity.get": true, "entity.resolve": true, "entity.create": true, "entity.merge": true,
	"epic.list": true, "epic.view": true, "epic.create": true,
	"coding_run.start": true, "coding_run.status": true, "coding_run.events": true,
	"agent.invoke": true,
}

type Definition struct {
	Kind                        string               `json:"kind"`
	Name                        string               `json:"name"`
	Purpose                     string               `json:"purpose"`
	SystemPrompt                string               `json:"system_prompt"`
	Model                       Model                `json:"model"`
	Tools                       []Tool               `json:"tools,omitempty"`
	Knowledge                   []KnowledgeReference `json:"knowledge,omitempty"`
	Heartbeat                   *Heartbeat           `json:"heartbeat,omitempty"`
	Budget                      *Budget              `json:"budget,omitempty"`
	EvalCriticAgentID           string               `json:"eval_critic_agent_id,omitempty"`
	Runtime                     *RuntimeSelection    `json:"runtime,omitempty"`
	ActionPolicy                *ActionPolicy        `json:"action_policy,omitempty"`
	WritableKnowledgeNamespaces []string             `json:"writable_knowledge_namespaces,omitempty"`
	Sources                     *SourcePermissions   `json:"sources,omitempty"`
	Concurrency                 int                  `json:"concurrency,omitempty"`
	TimeoutSeconds              int                  `json:"timeout_seconds,omitempty"`
	Conversations               *ConversationPolicy  `json:"conversations,omitempty"`
	Checkpoints                 *CheckpointPolicy    `json:"checkpoints,omitempty"`
}

type RuntimeSelection struct {
	Kind string `json:"kind"`
}

type ActionPolicy struct {
	Default          string   `json:"default"`
	AllowedActions   []string `json:"allowed_actions,omitempty"`
	ApprovalRequired []string `json:"approval_required,omitempty"`
}

type SourcePermissions struct {
	Network            string   `json:"network"`
	AllowedDomains     []string `json:"allowed_domains,omitempty"`
	AllowedSourceTypes []string `json:"allowed_source_types,omitempty"`
}

type ConversationPolicy struct {
	Enabled  bool `json:"enabled"`
	MaxTurns int  `json:"max_turns"`
}

type CheckpointPolicy struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"interval_seconds"`
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
	if d.Kind == DefinitionKindV2 {
		if d.Runtime == nil {
			d.Runtime = &RuntimeSelection{Kind: RuntimeTypeScriptAgentsSDK}
		}
		if d.ActionPolicy == nil {
			allowed := make([]string, 0, len(d.Tools))
			for _, configured := range d.Tools {
				allowed = append(allowed, configured.ID)
			}
			d.ActionPolicy = &ActionPolicy{Default: "deny", AllowedActions: allowed}
		}
		if d.Sources == nil {
			d.Sources = &SourcePermissions{Network: "none"}
		}
		if d.Concurrency == 0 {
			d.Concurrency = DefaultConcurrency
		}
		if d.TimeoutSeconds == 0 {
			d.TimeoutSeconds = DefaultTimeoutSeconds
		}
		if d.Conversations == nil {
			d.Conversations = &ConversationPolicy{Enabled: false, MaxTurns: DefaultConversationMaxTurns}
		}
		if d.Conversations.MaxTurns == 0 {
			d.Conversations.MaxTurns = DefaultConversationMaxTurns
		}
		if d.Checkpoints == nil {
			d.Checkpoints = &CheckpointPolicy{Enabled: true, IntervalSeconds: 30}
		}
		if d.Checkpoints.Enabled && d.Checkpoints.IntervalSeconds == 0 {
			d.Checkpoints.IntervalSeconds = 30
		}
	}
}

func (d Definition) EffectiveRuntime() RuntimeSelection {
	if d.Runtime == nil || strings.TrimSpace(d.Runtime.Kind) == "" {
		return RuntimeSelection{Kind: RuntimeTypeScriptAgentsSDK}
	}
	return *d.Runtime
}

func (d Definition) AllowsAction(action string) bool {
	if d.Kind == DefinitionKind {
		for _, configured := range d.Tools {
			if configured.ID == action {
				return true
			}
		}
		return false
	}
	if d.ActionPolicy == nil {
		return false
	}
	for _, allowed := range d.ActionPolicy.AllowedActions {
		if allowed == action {
			return true
		}
	}
	return d.ActionPolicy.Default == "allow"
}

func (d Definition) AllowsKnowledgeWrite(namespace string) bool {
	if d.Kind == DefinitionKind {
		return true
	}
	for _, allowed := range d.WritableKnowledgeNamespaces {
		if allowed == namespace {
			return true
		}
	}
	return false
}

func (d Definition) RequiresApproval(action string) bool {
	if d.ActionPolicy == nil {
		return false
	}
	for _, guarded := range d.ActionPolicy.ApprovalRequired {
		if guarded == action {
			return true
		}
	}
	return false
}

func (d Definition) Validate(modelAvailable func(string) bool) error {
	if d.Kind != DefinitionKind && d.Kind != DefinitionKindV2 {
		return fmt.Errorf("kind must be %q or %q", DefinitionKind, DefinitionKindV2)
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
	if d.Kind == DefinitionKindV2 {
		if d.EffectiveRuntime().Kind != RuntimeTypeScriptAgentsSDK {
			return fmt.Errorf("runtime kind %q is not available", d.EffectiveRuntime().Kind)
		}
		if d.ActionPolicy == nil || (d.ActionPolicy.Default != "allow" && d.ActionPolicy.Default != "deny") {
			return fmt.Errorf("action_policy default must be allow or deny")
		}
		for _, configured := range d.Tools {
			if !d.AllowsAction(configured.ID) {
				return fmt.Errorf("tool %q is denied by action_policy", configured.ID)
			}
			if namespace := ToolWriteNamespace(configured.ID); namespace != "" && !d.AllowsKnowledgeWrite(namespace) {
				return fmt.Errorf("tool %q requires writable knowledge namespace %q", configured.ID, namespace)
			}
			if configured.ID == "openai.web_search" && d.Sources != nil && d.Sources.Network == "none" {
				return fmt.Errorf("openai.web_search requires network source permission")
			}
		}
		if err := validateUniqueKnownActions(d.ActionPolicy.AllowedActions); err != nil {
			return err
		}
		if err := validateUniqueKnownActions(d.ActionPolicy.ApprovalRequired); err != nil {
			return err
		}
		for _, guarded := range d.ActionPolicy.ApprovalRequired {
			if !d.AllowsAction(guarded) {
				return fmt.Errorf("approval-required action %q must also be allowed", guarded)
			}
		}
		for _, namespace := range d.WritableKnowledgeNamespaces {
			if !namespacePattern.MatchString(namespace) {
				return fmt.Errorf("invalid writable knowledge namespace %q", namespace)
			}
		}
		if err := validateSourcePermissions(d.Sources); err != nil {
			return err
		}
		if d.Concurrency < 1 || d.Concurrency > 20 {
			return fmt.Errorf("concurrency must be 1-20")
		}
		if d.TimeoutSeconds < 1 || d.TimeoutSeconds > 86400 {
			return fmt.Errorf("timeout_seconds must be 1-86400")
		}
		if d.Conversations == nil || d.Conversations.MaxTurns < 1 || d.Conversations.MaxTurns > 200 {
			return fmt.Errorf("conversations.max_turns must be 1-200")
		}
		if d.Checkpoints == nil || (d.Checkpoints.Enabled && (d.Checkpoints.IntervalSeconds < 5 || d.Checkpoints.IntervalSeconds > 3600)) {
			return fmt.Errorf("enabled checkpoints interval_seconds must be 5-3600")
		}
	}
	return nil
}

func ToolWriteNamespace(toolID string) string {
	switch toolID {
	case "memory.create", "memory.version", "memory.supersede", "memory.archive":
		return "memories"
	case "artifact.create", "artifact.version", "artifact.activate", "artifact.supersede":
		return "artifacts"
	case "entity.create", "entity.merge":
		return "entities"
	default:
		return ""
	}
}

func validateUniqueKnownActions(groups ...[]string) error {
	seen := map[string]bool{}
	for _, group := range groups {
		for _, action := range group {
			if !toolCatalog[action] {
				return fmt.Errorf("unknown action %q", action)
			}
			if seen[action] {
				return fmt.Errorf("duplicate action policy entry %q", action)
			}
			seen[action] = true
		}
	}
	return nil
}

func validateSourcePermissions(p *SourcePermissions) error {
	if p == nil {
		return fmt.Errorf("sources policy is required")
	}
	switch p.Network {
	case "none", "allowlist", "public":
	default:
		return fmt.Errorf("sources.network must be none, allowlist, or public")
	}
	if p.Network == "allowlist" && len(p.AllowedDomains) == 0 {
		return fmt.Errorf("allowlist network policy requires allowed_domains")
	}
	for _, domain := range p.AllowedDomains {
		parsed, err := url.Parse("https://" + domain)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Host != domain || strings.ContainsAny(domain, "/?#@") {
			return fmt.Errorf("invalid allowed domain %q", domain)
		}
	}
	allowedSource := map[string]bool{"rss": true, "atom": true, "web": true, "reddit": true, "x": true}
	seen := map[string]bool{}
	for _, sourceType := range p.AllowedSourceTypes {
		if !allowedSource[sourceType] || seen[sourceType] {
			return fmt.Errorf("invalid or duplicate source type %q", sourceType)
		}
		seen[sourceType] = true
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
