package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestExpiryWatchdogCommand(t *testing.T) {
	cmd := expiryWatchdogCommand()
	for _, want := range []string{"VES_SANDBOX_EXPIRES_EPOCH", "/tmp/ves-expires-at", "while :", "exit 0"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("watchdog command missing %q", want)
		}
	}
}

func TestDockerSelfExpiry(t *testing.T) {
	if os.Getenv("VES_TEST_DOCKER_TTL") != "1" {
		t.Skip("set VES_TEST_DOCKER_TTL=1 to run Docker TTL integration test")
	}
	if exec.Command("docker", "image", "inspect", "node:24-bookworm").Run() != nil {
		t.Skip("node:24-bookworm image is not available")
	}
	ctx := context.Background()
	id := fmt.Sprintf("sbx_ttl_%d", time.Now().UnixNano())
	d := NewDocker(id)
	if err := d.Create(ctx, CreateOpts{
		SandboxID:   id,
		WorkspaceID: "ws_test",
		RunID:       "run_test",
		Image:       "node:24-bookworm",
		ExpiresAt:   time.Now().UTC().Add(3 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	defer d.Destroy(context.Background())
	if err := d.Start(ctx); err != nil {
		t.Fatal(err)
	}
	inspect, err := exec.Command("docker", "inspect", "--format", "{{.HostConfig.AutoRemove}} {{index .Config.Labels \"vessica.managed\"}}", d.ContainerID()).Output()
	if err != nil || strings.TrimSpace(string(inspect)) != "true true" {
		t.Fatalf("inspect=%q err=%v", inspect, err)
	}
	if err := d.RefreshLease(ctx, time.Now().UTC().Add(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * time.Second)
	if exec.Command("docker", "inspect", d.ContainerID()).Run() != nil {
		t.Fatal("container expired before renewed lease")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command("docker", "inspect", d.ContainerID()).Run() != nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("container was not automatically removed after lease expiry")
}
