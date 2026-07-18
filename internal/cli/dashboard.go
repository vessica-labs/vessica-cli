package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/dashboard"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/version"
)

type dashboardRuntime struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	Version   string `json:"version"`
	StartedAt string `json:"started_at"`
}

func newDashboardCmd(app *App) *cobra.Command {
	var port int
	var open bool
	cmd := &cobra.Command{Use: "dashboard", Short: "Open the Vessica dashboard", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		if app.Config.Hosted.ControlPlaneURL != "" {
			if open {
				target, err := hostedDashboardOpenURL(cmd.Context(), app)
				if err != nil {
					return err
				}
				if err := auth.OpenBrowser(target); err != nil {
					return err
				}
			}
			return app.Printer.Success(map[string]any{"mode": "hosted", "dashboard_url": app.Config.Hosted.ControlPlaneURL, "opened": open})
		}
		if open {
			return openLocalDashboard(cmd.Context(), app, port)
		}
		return serveLocalDashboard(cmd.Context(), app, port, true)
	}}
	cmd.Flags().BoolVar(&open, "open", false, "open the dashboard in a browser and return")
	cmd.Flags().IntVar(&port, "port", 0, "loopback port (0 selects an available port)")
	servePort := 0
	serve := &cobra.Command{Use: "serve", Hidden: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		return serveLocalDashboard(cmd.Context(), app, servePort, false)
	}}
	serve.Flags().IntVar(&servePort, "port", 0, "loopback port")
	status := &cobra.Command{Use: "status", Short: "Show local dashboard process status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		runtime, _ := readDashboardRuntime(app.Root)
		running := runtime != nil && dashboardHealthy(cmd.Context(), runtime.Port, runtime.Version)
		return app.Printer.Success(map[string]any{"running": running, "runtime": runtime})
	}}
	cmd.AddCommand(serve, status)
	return cmd
}

func hostedDashboardOpenURL(ctx context.Context, app *App) (string, error) {
	base := strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/")
	secrets, err := loadRailwaySecrets(app.Root)
	if err != nil {
		return "", fmt.Errorf("load hosted dashboard credentials: %w", err)
	}
	if strings.TrimSpace(secrets.ServiceToken) == "" {
		return "", fmt.Errorf("hosted dashboard owner recovery credential is unavailable; run ves up to repair the workspace attachment")
	}
	claimURL, required, err := ensureHostedOwnerClaim(ctx, base, secrets.ServiceToken, id.New("dashboard"))
	if err != nil {
		return "", fmt.Errorf("prepare hosted dashboard access: %w", err)
	}
	if required {
		return claimURL, nil
	}
	return base, nil
}

func dashboardRuntimePath(root string) string {
	return filepath.Join(root, ".vessica", "state", "dashboard.json")
}
func dashboardLockPath(root string) string {
	return filepath.Join(root, ".vessica", "state", "dashboard.lock")
}
func readDashboardRuntime(root string) (*dashboardRuntime, error) {
	raw, err := os.ReadFile(dashboardRuntimePath(root))
	if err != nil {
		return nil, err
	}
	var v dashboardRuntime
	if err = json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
func writeDashboardRuntime(root string, v dashboardRuntime) error {
	raw, _ := json.Marshal(v)
	tmp := dashboardRuntimePath(root) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dashboardRuntimePath(root))
}
func dashboardHealthy(ctx context.Context, port int, wantVersion string) bool {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/healthz", port), nil)
	client := &http.Client{Timeout: 700 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK && wantVersion == version.Version
}
func issueLocalLaunch(ctx context.Context, port int) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/auth/local/launch", port), nil)
	req.Header.Set("X-Vessica-CLI", "1")
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var env struct {
		Data struct {
			LaunchToken string `json:"launch_token"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return "", err
	}
	if env.Data.LaunchToken == "" {
		return "", fmt.Errorf("dashboard did not issue a launch token")
	}
	return env.Data.LaunchToken, nil
}
func openLocalDashboard(ctx context.Context, app *App, port int) error {
	runtime, _ := readDashboardRuntime(app.Root)
	if runtime == nil || !dashboardHealthy(ctx, runtime.Port, runtime.Version) {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		args := []string{"--cwd", app.Root, "dashboard", "serve"}
		if port > 0 {
			args = append(args, "--port", strconv.Itoa(port))
		}
		logPath := filepath.Join(app.Root, ".vessica", "state", "dashboard.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		process := exec.Command(exe, args...)
		process.Stdout, process.Stderr = logFile, logFile
		detachDashboardProcess(process)
		if err = process.Start(); err != nil {
			_ = logFile.Close()
			return err
		}
		_ = process.Process.Release()
		_ = logFile.Close()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(80 * time.Millisecond)
			runtime, _ = readDashboardRuntime(app.Root)
			if runtime != nil && dashboardHealthy(ctx, runtime.Port, runtime.Version) {
				break
			}
		}
		if runtime == nil || !dashboardHealthy(ctx, runtime.Port, runtime.Version) {
			return fmt.Errorf("dashboard did not become ready; inspect %s", logPath)
		}
	}
	launch, err := issueLocalLaunch(ctx, runtime.Port)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/#launch_token=%s", runtime.Port, launch)
	if err = auth.OpenBrowser(url); err != nil {
		return fmt.Errorf("open browser: %w (open %s manually)", err, url)
	}
	return app.Printer.Success(map[string]any{"running": true, "url": fmt.Sprintf("http://127.0.0.1:%d/", runtime.Port), "pid": runtime.PID, "version": runtime.Version})
}
func serveLocalDashboard(ctx context.Context, app *App, port int, printURL bool) error {
	if err := os.MkdirAll(filepath.Dir(dashboardRuntimePath(app.Root)), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(dashboardLockPath(app.Root), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		runtime, _ := readDashboardRuntime(app.Root)
		if runtime != nil && dashboardHealthy(ctx, runtime.Port, runtime.Version) {
			return fmt.Errorf("dashboard already running at http://127.0.0.1:%d", runtime.Port)
		}
		_ = os.Remove(dashboardLockPath(app.Root))
		lock, err = os.OpenFile(dashboardLockPath(app.Root), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(dashboardLockPath(app.Root))
		_ = os.Remove(dashboardRuntimePath(app.Root))
	}()
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	defer listener.Close()
	actual := listener.Addr().(*net.TCPAddr).Port
	service := appservice.New(app.DB, app.Root, app.Config)
	server := dashboard.New(service, "local")
	server.Origin = fmt.Sprintf("http://127.0.0.1:%d", actual)
	server.PreviewAccess = func(ctx context.Context, runID string) (string, error) {
		runRecord, err := app.DB.GetRun(ctx, runID)
		if err != nil {
			return "", err
		}
		if runRecord.PreviewURL == "" {
			return "", fmt.Errorf("preview is unavailable")
		}
		return runRecord.PreviewURL, nil
	}
	runtime := dashboardRuntime{PID: os.Getpid(), Port: actual, Version: version.Version, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err = writeDashboardRuntime(app.Root, runtime); err != nil {
		return err
	}
	if printURL {
		launch := server.IssueLaunchToken()
		fmt.Fprintf(cmdWriter(app), "Vessica Dashboard: http://127.0.0.1:%d/#launch_token=%s\n", actual, launch)
	}
	httpServer := &http.Server{Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err = <-done:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
func cmdWriter(app *App) *os.File {
	if app.Printer != nil && app.Flags.Quiet {
		return os.Stderr
	}
	return os.Stdout
}
