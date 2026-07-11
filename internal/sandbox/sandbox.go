package sandbox

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Sandbox is the v1 execution environment abstraction.
type Sandbox interface {
	Create(ctx context.Context, opts CreateOpts) error
	Start(ctx context.Context) error
	Exec(ctx context.Context, cmd []string, stdout, stderr io.Writer) (int, error)
	Stream(ctx context.Context, stdout, stderr io.Writer) error
	ExposePort(ctx context.Context, port int) (string, error)
	StartPreview(ctx context.Context, command string, port int, healthcheck string) (string, error)
	StopPreview(ctx context.Context) error
	RefreshLease(ctx context.Context, expiresAt time.Time) error
	PreviewURL() string
	Destroy(ctx context.Context) error
	Status(ctx context.Context) (string, error)
	ID() string
	ContainerID() string
	Workdir() string
}

type CreateOpts struct {
	SandboxID   string
	Image       string
	Workdir     string
	Branch      string
	RemoteURL   string
	Token       string
	Env         map[string]string
	HostWorkdir string // optional bind for local-dev mode
	PreviewPort int
	WorkspaceID string
	RunID       string
	ExpiresAt   time.Time
}

func DefaultImage() string {
	return "ghcr.io/vessica-cli/sandbox:v1"
}

// FallbackImage is used when the custom image is unavailable.
func FallbackImage() string {
	return "golang:1.24-bookworm"
}

var ErrNotRunning = fmt.Errorf("sandbox not running")
