package runner

import (
	"context"
	"io"
)

// Runner abstracts coding CLI tools.
type Runner interface {
	Name() string
	Prepare(ctx context.Context, in Input) error
	Start(ctx context.Context, task Task) error
	StreamEvents(ctx context.Context) (<-chan Event, error)
	Cancel(ctx context.Context) error
	CollectResult(ctx context.Context) (Result, error)
}

type Input struct {
	RepoPath        string
	Phase           string
	Instructions    string
	ArtifactContext string
	TicketContext   string
	HarnessContext  string
	Budget          Budget
	Env             map[string]string
	Workdir         string
	AllowStub       bool
	Model           string
	ReasoningEffort string
}

type Task struct {
	Name         string
	Prompt       string
	SystemPrompt string
}

type Budget struct {
	MaxRetries int
	MaxMinutes int
}

type Event struct {
	Type    string         `json:"type"`
	Message string         `json:"message,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
	Raw     string         `json:"-"`
}

type Result struct {
	Status       string         `json:"status"`
	Output       string         `json:"output"`
	Evidence     string         `json:"evidence,omitempty"`
	CostUSD      float64        `json:"cost_usd,omitempty"`
	InputTokens  int64          `json:"input_tokens,omitempty"`
	OutputTokens int64          `json:"output_tokens,omitempty"`
	Model        string         `json:"model,omitempty"`
	FilesChanged []string       `json:"files_changed,omitempty"`
	Meta         map[string]any `json:"meta,omitempty"`
}

// WriterEventSink adapts a writer for simple logging.
type WriterEventSink struct {
	W io.Writer
}
