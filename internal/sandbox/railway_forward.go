package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

const railwayForwardOutputLimit = 64 << 10

// ExposePort runs Railway's native loopback forward. The Railway CLI handles
// ordinary relay reconnects internally; if that process eventually exits,
// Vessica restarts it on the same local port with bounded backoff.
func (r *RailwaySandbox) ExposePort(ctx context.Context, remotePort int) (string, error) {
	if r.sandboxID == "" {
		return "", ErrNotRunning
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("reserve Railway forward port: %w", err)
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	_ = r.StopForward()
	forwardCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	output := &boundedTailBuffer{limit: railwayForwardOutputLimit}
	r.forwardMu.Lock()
	r.forwardCancel = cancel
	r.forwardDone = done
	r.forwardMu.Unlock()

	args := append(r.baseArgs(), "forward", "--id", r.sandboxID, "--strict", fmt.Sprintf("%d:%d", localPort, remotePort))
	go r.superviseForward(forwardCtx, done, output, args)

	localURL := fmt.Sprintf("http://127.0.0.1:%d", localPort)
	if err := waitForHTTP(ctx, localURL, 30*time.Second); err != nil {
		_ = r.StopForward()
		return "", fmt.Errorf("railway sandbox forward: %w: %s", err, output.String())
	}
	if err := r.persistSession(); err != nil {
		_ = r.StopForward()
		return "", fmt.Errorf("persist Railway CLI session after sandbox forward: %w", err)
	}
	r.previewURL = localURL
	return localURL, nil
}

func (r *RailwaySandbox) superviseForward(ctx context.Context, done chan<- struct{}, output *boundedTailBuffer, args []string) {
	defer close(done)
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		cmd := r.command(ctx, args...)
		cmd.Stdout, cmd.Stderr = output, output
		if err := cmd.Start(); err == nil {
			_ = cmd.Wait()
			_ = r.persistSession()
		}
		if ctx.Err() != nil {
			return
		}
		fmt.Fprintf(os.Stderr, "Railway sandbox forward exited for sandbox %s; restarting in %s\n", r.sandboxID, backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (r *RailwaySandbox) StopForward() error {
	r.forwardMu.Lock()
	cancel, done := r.forwardCancel, r.forwardDone
	r.forwardCancel, r.forwardDone = nil, nil
	r.forwardMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			return fmt.Errorf("Railway sandbox forward did not stop within 5s")
		}
	}
	return nil
}

type boundedTailBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
}

func (b *boundedTailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	if len(p) >= b.limit {
		b.buf.Reset()
		_, _ = b.buf.Write(p[len(p)-b.limit:])
		return written, nil
	}
	if b.buf.Len()+len(p) > b.limit {
		existing := append([]byte(nil), b.buf.Bytes()...)
		keep := b.limit - len(p)
		b.buf.Reset()
		_, _ = b.buf.Write(existing[len(existing)-keep:])
	}
	_, _ = b.buf.Write(p)
	return written, nil
}

func (b *boundedTailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
