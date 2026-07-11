package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/sandbox"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

const (
	PreviewTTL = 24 * time.Hour
	FailureTTL = 4 * time.Hour
	MaxTTL     = 7 * 24 * time.Hour
)

type GCOptions struct {
	DryRun    bool
	OlderThan time.Duration
}

type GCResult struct {
	Scanned      int      `json:"scanned"`
	Destroyed    []string `json:"destroyed"`
	WouldDestroy []string `json:"would_destroy,omitempty"`
	Reconciled   []string `json:"reconciled,omitempty"`
}

func Initialize(ctx context.Context, db *state.DB, s *state.Sandbox) error {
	now := time.Now().UTC()
	s.LastAccessedAt = now.Format(time.RFC3339Nano)
	s.ExpiresAt = now.Add(PreviewTTL).Format(time.RFC3339Nano)
	if err := db.UpdateSandbox(ctx, s); err != nil {
		return err
	}
	return refreshContainerLease(ctx, s)
}

func Touch(ctx context.Context, db *state.DB, s *state.Sandbox) error {
	return setLease(ctx, db, s, PreviewTTL, false)
}

func MarkFailed(ctx context.Context, db *state.DB, s *state.Sandbox) error {
	return setLease(ctx, db, s, FailureTTL, false)
}

func Retain(ctx context.Context, db *state.DB, s *state.Sandbox, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("retention duration must be positive")
	}
	if duration > MaxTTL {
		return fmt.Errorf("retention duration exceeds maximum of %s", FormatDuration(MaxTTL))
	}
	return setLease(ctx, db, s, duration, true)
}

func setLease(ctx context.Context, db *state.DB, s *state.Sandbox, duration time.Duration, explicit bool) error {
	now := time.Now().UTC()
	s.LastAccessedAt = now.Format(time.RFC3339Nano)
	s.ExpiresAt = now.Add(duration).Format(time.RFC3339Nano)
	if explicit {
		s.RetainedUntil = s.ExpiresAt
	}
	if err := db.UpdateSandbox(ctx, s); err != nil {
		return err
	}
	return refreshContainerLease(ctx, s)
}

func EffectiveExpiry(s *state.Sandbox) time.Time {
	expires := parseTime(s.ExpiresAt)
	retained := parseTime(s.RetainedUntil)
	if retained.After(expires) {
		return retained
	}
	return expires
}

func refreshContainerLease(ctx context.Context, s *state.Sandbox) error {
	if s.Backend != "docker" || s.ContainerID == "" || s.ContainerID == "local" || s.Status == "destroyed" || s.Status == "expired" {
		return nil
	}
	expires := EffectiveExpiry(s)
	if expires.IsZero() {
		return nil
	}
	ds := sandbox.NewDocker(s.ID)
	ds.SetContainerID(s.ContainerID, "", s.PreviewPort)
	return ds.RefreshLease(ctx, expires)
}

func DestroySuperseded(ctx context.Context, db *state.DB, root, runID, keepID string) ([]string, error) {
	list, err := db.ListSandboxesForRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	var destroyed []string
	for i := range list {
		s := &list[i]
		if s.ID == keepID || s.Status == "destroyed" || s.Status == "expired" {
			continue
		}
		if err := Destroy(ctx, db, root, s, "superseded"); err != nil {
			return destroyed, err
		}
		destroyed = append(destroyed, s.ID)
	}
	return destroyed, nil
}

func Destroy(ctx context.Context, db *state.DB, root string, s *state.Sandbox, reason string) error {
	if s.Backend == "docker" && s.ContainerID != "" && s.ContainerID != "local" {
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", s.ContainerID).Run()
	}
	removeWorkdir(root, s)
	s.Status = "destroyed"
	if reason == "expired" {
		s.Status = "expired"
	}
	s.PreviewURL = ""
	s.DestroyedAt = state.Now()
	return db.UpdateSandbox(ctx, s)
}

func GC(ctx context.Context, db *state.DB, root string, opts GCOptions) (GCResult, error) {
	list, err := db.ListSandboxes(ctx)
	if err != nil {
		return GCResult{}, err
	}
	now := time.Now().UTC()
	result := GCResult{Scanned: len(list), Destroyed: []string{}, WouldDestroy: []string{}, Reconciled: []string{}}
	for i := range list {
		s := &list[i]
		if s.Status == "destroyed" || s.Status == "expired" {
			continue
		}
		expires := EffectiveExpiry(s)
		created := parseTime(s.CreatedAt)
		if expires.IsZero() {
			if opts.DryRun {
				continue
			}
			s.LastAccessedAt = now.Format(time.RFC3339Nano)
			s.ExpiresAt = now.Add(PreviewTTL).Format(time.RFC3339Nano)
			if err := db.UpdateSandbox(ctx, s); err != nil {
				return result, err
			}
			_ = refreshContainerLease(ctx, s)
			expires = EffectiveExpiry(s)
		}
		expired := !expires.IsZero() && !expires.After(now)
		if opts.OlderThan > 0 && !created.IsZero() && !created.Add(opts.OlderThan).After(now) {
			expired = true
		}
		if expired {
			if opts.DryRun {
				result.WouldDestroy = append(result.WouldDestroy, s.ID)
				continue
			}
			if err := Destroy(ctx, db, root, s, "expired"); err != nil {
				return result, err
			}
			result.Destroyed = append(result.Destroyed, s.ID)
			continue
		}
		if s.Backend == "docker" && s.ContainerID != "" && s.ContainerID != "local" && !dockerExists(ctx, s.ContainerID) {
			if opts.DryRun {
				continue
			}
			s.Status = "missing"
			s.PreviewURL = ""
			s.DestroyedAt = state.Now()
			if err := db.UpdateSandbox(ctx, s); err != nil {
				return result, err
			}
			result.Reconciled = append(result.Reconciled, s.ID)
		}
	}
	return result, nil
}

func ParseDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasSuffix(raw, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(raw, "d"), 64)
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid duration %q", raw)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	return time.ParseDuration(raw)
}

func FormatDuration(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
	return d.String()
}

func parseTime(raw string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, raw)
	return t
}

func dockerExists(ctx context.Context, containerID string) bool {
	return exec.CommandContext(ctx, "docker", "inspect", containerID).Run() == nil
}

func removeWorkdir(root string, s *state.Sandbox) {
	var meta struct {
		HostWorkdir string `json:"host_workdir"`
	}
	_ = json.Unmarshal([]byte(s.MetaJSON), &meta)
	base := filepath.Join(root, ".vessica", "sandboxes", s.ID)
	if meta.HostWorkdir != "" {
		base = filepath.Dir(meta.HostWorkdir)
	}
	_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", meta.HostWorkdir).Run()
	_ = os.RemoveAll(base)
}
