package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/auth"
)

func newAuthCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage provider authentication"}
	var token, account, clientID string
	login := &cobra.Command{
		Use:   "login [provider]",
		Short: "Open browser login for railway|linear|github|codex",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			providers := []string{"railway", "linear", "github", "codex"}
			if len(args) == 1 {
				providers = []string{strings.ToLower(args[0])}
			}
			var results []map[string]any
			for _, provider := range providers {
				result, err := loginProvider(cmd.Context(), cmd, app, provider, token, account, clientID)
				if err != nil {
					return err
				}
				results = append(results, result)
			}
			if len(results) == 1 {
				return app.Printer.Success(results[0])
			}
			return app.Printer.Success(map[string]any{"providers": results})
		},
	}
	login.Flags().StringVar(&token, "token", "", "access token for headless or CI login")
	login.Flags().StringVar(&account, "account", "", "account label")
	login.Flags().StringVar(&clientID, "client-id", "", "OAuth client ID (Railway or Linear)")
	cmd.AddCommand(login)

	cmd.AddCommand(&cobra.Command{
		Use:   "logout <provider>",
		Args:  cobra.ExactArgs(1),
		Short: "Logout provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := strings.ToLower(args[0])
			if provider == "codex" || provider == "openai" {
				if path, err := exec.LookPath("codex"); err == nil {
					logout := exec.CommandContext(cmd.Context(), path, "logout")
					logout.Stdin, logout.Stdout, logout.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
					if err := logout.Run(); err != nil {
						return err
					}
				}
				provider = "openai"
			}
			_ = auth.Logout(provider)
			return app.Printer.Success(map[string]any{"provider": provider, "logged_in": false})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show auth status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.Printer.Success(auth.StatusAll([]string{"github", "gitlab", "linear", "jira", "railway", "codex"}))
		},
	})
	return cmd
}

func loginProvider(ctx context.Context, cmd *cobra.Command, app *App, provider, token, account, clientID string) (map[string]any, error) {
	switch provider {
	case "railway", "linear":
		if token != "" {
			if err := auth.Login(provider, token, account); err != nil {
				return nil, err
			}
			return authResult(provider, account, "token"), nil
		}
		if clientID == "" {
			clientID = auth.OAuthClientID(provider)
		}
		configuration, err := auth.Provider(provider, clientID)
		if err != nil {
			return nil, err
		}
		if !app.Flags.JSON {
			fmt.Fprintf(cmd.ErrOrStderr(), "Opening %s in your browser...\n", strings.Title(provider))
		}
		credential, err := auth.BrowserLogin(ctx, configuration, func(target string) error {
			if err := auth.OpenBrowser(target); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Could not open a browser automatically. Open this URL:\n%s\n", target)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"provider": provider, "logged_in": true, "method": "oauth_pkce", "expires_at": credential.ExpiresAt}, nil
	case "github":
		tok := firstNonEmpty(token, os.Getenv("VES_GITHUB_TOKEN"), os.Getenv("GITHUB_TOKEN"))
		if tok == "" {
			var err error
			tok, account, err = githubBrowserLogin(ctx, cmd)
			if err != nil {
				return nil, err
			}
		}
		if err := auth.Login("github", tok, account); err != nil {
			return nil, err
		}
		return authResult("github", account, "github_cli"), nil
	case "codex", "openai":
		if token != "" {
			if err := auth.Login("openai", token, account); err != nil {
				return nil, err
			}
			return authResult("codex", account, "api_key"), nil
		}
		path, err := exec.LookPath("codex")
		if err != nil {
			return nil, fmt.Errorf("codex CLI is not installed")
		}
		if loggedIn, detail := auth.CodexStatus(ctx); !loggedIn {
			if !app.Flags.JSON {
				fmt.Fprintln(cmd.ErrOrStderr(), "Opening Codex sign-in in your browser...")
			}
			login := exec.CommandContext(ctx, path, "login")
			login.Stdin, login.Stdout, login.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			if err := login.Run(); err != nil {
				return nil, fmt.Errorf("Codex login failed: %w (%s)", err, detail)
			}
		}
		return authResult("codex", "ChatGPT", "codex_cli"), nil
	case "gitlab", "jira":
		tok := firstNonEmpty(token, os.Getenv("VES_"+strings.ToUpper(provider)+"_TOKEN"))
		if tok == "" {
			if app.Flags.JSON {
				return nil, app.Printer.Fail("missing_token", "token required", "pass --token or set VES_"+strings.ToUpper(provider)+"_TOKEN")
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Paste %s token: ", provider)
			line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			tok = strings.TrimSpace(line)
		}
		if tok == "" {
			return nil, fmt.Errorf("empty %s token", provider)
		}
		if err := auth.Login(provider, tok, account); err != nil {
			return nil, err
		}
		return authResult(provider, account, "token"), nil
	default:
		return nil, app.Printer.Fail("invalid_provider", "unsupported provider", "railway|linear|github|codex|gitlab|jira")
	}
}

func githubBrowserLogin(ctx context.Context, cmd *cobra.Command) (string, string, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", "", fmt.Errorf("GitHub CLI is required for browser login; install gh or pass --token")
	}
	read := func(args ...string) string {
		output, err := exec.CommandContext(ctx, path, args...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(output))
	}
	token := read("auth", "token")
	if token == "" {
		login := exec.CommandContext(ctx, path, "auth", "login", "--web", "--git-protocol", "https")
		login.Stdin, login.Stdout, login.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
		if err := login.Run(); err != nil {
			return "", "", err
		}
		token = read("auth", "token")
	}
	if token == "" {
		return "", "", fmt.Errorf("GitHub CLI login completed without an available token")
	}
	account := read("api", "user", "--jq", ".login")
	return token, account, nil
}

func authResult(provider, account, method string) map[string]any {
	return map[string]any{"provider": provider, "logged_in": true, "account": account, "method": method}
}
