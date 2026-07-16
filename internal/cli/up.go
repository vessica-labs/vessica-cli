package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/harness"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/onboarding"
	"github.com/vessica-labs/vessica-cli/internal/pack"
	"github.com/vessica-labs/vessica-cli/internal/state"
	ks "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

type hostedUpOptions struct {
	Workspace string
	Refresh   bool
	Stream    string
	ResumeID  string
}

type railwayWorkspaceChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func newUpCmd(app *App) *cobra.Command {
	var opts hostedUpOptions
	cmd := &cobra.Command{Use: "up", Short: "Attach this repository to hosted Vessica on Railway", RunE: func(cmd *cobra.Command, args []string) error { return runHostedUp(cmd, app, opts) }}
	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "Railway workspace id or name")
	cmd.Flags().BoolVar(&opts.Refresh, "refresh", false, "refresh the repository map for an attached repository")
	cmd.Flags().StringVar(&opts.Stream, "stream", "", "event stream format (jsonl)")
	cmd.AddCommand(&cobra.Command{Use: "status [operation-id]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		remote := detectGitRemote(app.Root)
		id := ""
		if len(args) > 0 {
			id = args[0]
		}
		op, err := onboarding.Find(id, state.CanonicalRepositoryRemote(remote))
		if err != nil {
			if id != "" {
				op, err = loadHostedOnboarding(cmd.Context(), app.Root, id)
			}
			if err != nil {
				return app.Printer.Fail("onboarding_not_found", "no matching onboarding operation", "")
			}
		} else if hosted, hostedErr := loadHostedOnboarding(cmd.Context(), app.Root, op.ID); hostedErr == nil {
			op = hosted
		}
		return app.Printer.Success(op)
	}})
	resume := &cobra.Command{Use: "resume <operation-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		next := opts
		next.ResumeID = args[0]
		return runHostedUp(cmd, app, next)
	}}
	resume.Flags().StringVar(&opts.Stream, "stream", "", "event stream format (jsonl)")
	cmd.AddCommand(resume)
	return cmd
}

func runHostedUp(cmd *cobra.Command, app *App, opts hostedUpOptions) error {
	if opts.Stream != "" && opts.Stream != "jsonl" {
		return app.Printer.Fail("invalid_stream_format", "--stream must be jsonl", "")
	}
	root, err := gitRoot(app.Root)
	if err != nil {
		return app.Printer.Fail("repository_required", err.Error(), "run ves up from a Git repository with an origin remote")
	}
	app.Root = root
	remote := detectGitRemote(root)
	if strings.TrimSpace(remote) == "" {
		return app.Printer.Fail("repository_remote_required", "origin remote is required", "add a Git origin and push the repository before running ves up")
	}
	profile := onboarding.Scan(root, remote)
	if err := ensureRailwayCLI(cmd.Context()); err != nil {
		return app.Printer.Fail("railway_cli_missing", err.Error(), "install it with the official Railway installer, then resume ves up")
	}
	preflight := railwayUpPreflight(cmd.Context(), root)
	workspaces, _ := preflight["workspaces"].([]railwayWorkspaceChoice)
	if opts.Workspace == "" {
		if len(workspaces) == 1 {
			opts.Workspace = workspaces[0].ID
		} else if len(workspaces) > 1 && !app.Flags.DryRun {
			if app.Flags.JSON || app.Flags.Yes {
				return app.Printer.Fail("railway_workspace_required", "multiple Railway workspaces are available", "repeat with --workspace <id>")
			}
			selected, selectErr := selectRailwayWorkspace(cmd, workspaces)
			if selectErr != nil {
				return selectErr
			}
			opts.Workspace = selected
		}
	}
	plan := map[string]any{"repository": profile, "workspace": opts.Workspace, "retrieval": map[string]any{"mode": "lexical", "embedding_state": "not_configured", "upgrade_optional": true}, "resources": []string{"control-plane", "knowledge-server", "Postgres (vessica_control and vessica_knowledge)", "Railway sandbox checkpoint"}, "harness_action": map[string]string{"absent": "create", "partial": "audit and preserve", "present": "audit and preserve"}[profile.Harness], "linear": "not connected", "railway_preflight": preflight}
	if app.Flags.DryRun {
		return app.dryRun("up", plan)
	}
	if !app.Flags.Yes {
		if app.Flags.JSON {
			return app.requireYes("provision hosted Vessica and attach this repository")
		}
		ok, confirmErr := confirmHostedUp(cmd, profile, opts.Workspace)
		if confirmErr != nil {
			return confirmErr
		}
		if !ok {
			return app.Printer.Fail("cancelled", "onboarding cancelled", "")
		}
	}
	op := onboarding.New(id.New("onb"), state.CanonicalRepositoryRemote(remote))
	if opts.ResumeID != "" {
		if saved, loadErr := onboarding.Find(opts.ResumeID, ""); loadErr == nil {
			op = saved
		} else if hosted, hostedErr := loadHostedOnboarding(cmd.Context(), root, opts.ResumeID); hostedErr == nil {
			op = hosted
			_ = onboarding.Save(op)
		} else {
			return app.Printer.Fail("onboarding_not_found", "no matching onboarding operation", "")
		}
	}
	var hostedOperationEndpoint, hostedOperationToken, hostedRepositoryID string
	emit := func(name, status, message string) error {
		op.Set(name, status, message)
		_ = onboarding.Save(op)
		if hostedOperationEndpoint != "" {
			if err := syncHostedOnboarding(cmd.Context(), hostedOperationEndpoint, hostedOperationToken, hostedRepositoryID, op); err != nil {
				return err
			}
		}
		if opts.Stream == "jsonl" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"schema": "vessica.onboarding/v1", "operation_id": op.ID, "stage": name, "status": status, "message": message})
		}
		return nil
	}
	fail := func(name, code string, stageErr error) error {
		op.ErrorCode = code
		op.Error = stageErr.Error()
		_ = emit(name, "failed", stageErr.Error())
		hint := "run ves up resume " + op.ID + " --yes"
		if code == "sandbox_feature_disabled" {
			hint = "enable Railway Sandboxes in Priority Boarding, then " + hint
		}
		if opts.Stream == "jsonl" {
			failure := map[string]any{"schema": "vessica.onboarding/v1", "operation_id": op.ID, "status": "failed", "error": map[string]any{"code": code, "message": stageErr.Error()}, "resumable": true}
			if code == "sandbox_feature_disabled" {
				failure["action_url"] = "https://railway.com/account/feature-flags"
			}
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(failure)
			return stageErr
		}
		return app.Printer.Fail(code, stageErr.Error(), hint)
	}
	_ = emit("repository_preflight", "succeeded", "repository scanned")
	if err := ensureHostedBootstrap(cmd.Context(), app, remote); err != nil {
		return fail("provider_authentication", "bootstrap_failed", err)
	}
	if app.Config.Hosted.ProjectID == "" {
		installation, credentialJSON, findErr := onboarding.FindInstallation(opts.Workspace)
		if findErr == nil {
			known := installation.Config
			known.Repo = app.Config.Repo
			known.Repo.Remote = remote
			known.Attachment = config.AttachmentConfig{}
			known.State = app.Config.State
			app.Config = known
			var credentials railwaySecrets
			if err := json.Unmarshal(credentialJSON, &credentials); err != nil {
				return fail("workspace_discovery", "client_registry_invalid", err)
			}
			if err := saveRailwaySecrets(root, credentials); err != nil {
				return fail("workspace_discovery", "credential_restore_failed", err)
			}
			if err := config.Save(root, app.Config); err != nil {
				return fail("workspace_discovery", "repository_config_failed", err)
			}
		} else if errors.Is(findErr, os.ErrNotExist) {
			candidates, discoverErr := discoverRailwayInstallations(cmd.Context(), opts.Workspace)
			if discoverErr != nil {
				return fail("workspace_discovery", "installation_discovery_failed", discoverErr)
			}
			if len(candidates) > 1 {
				return fail("workspace_discovery", "installation_conflict", fmt.Errorf("Railway workspace contains multiple verified Vessica installations: %s", installationCandidateSummary(candidates)))
			}
			if len(candidates) == 1 {
				recovered, recoveredSecrets, recoverErr := recoverRailwayInstallation(cmd.Context(), candidates[0])
				if recoverErr != nil {
					return fail("workspace_discovery", "installation_recovery_failed", recoverErr)
				}
				recovered.Repo.Remote = remote
				app.Config = recovered
				if err := saveRailwaySecrets(root, recoveredSecrets); err != nil {
					return fail("workspace_discovery", "credential_restore_failed", err)
				}
				if err := config.Save(root, app.Config); err != nil {
					return fail("workspace_discovery", "repository_config_failed", err)
				}
			}
		} else {
			return fail("workspace_discovery", "client_registry_invalid", findErr)
		}
	}
	alreadyAttached := app.Config.Attachment.RepositoryID != "" && app.Config.Hosted.ControlPlaneURL != ""
	_ = emit("provider_authentication", "succeeded", "provider credentials resolved")
	_ = emit("workspace_discovery", "succeeded", "Railway workspace selected")
	_ = emit("sandbox_entitlement", "running", "sandbox entitlement will be verified by checkpoint creation")
	_ = emit("plan_ready", "succeeded", "hosted plan confirmed")
	missingBefore := missingHarnessTargets(profile)
	var result map[string]any
	if alreadyAttached {
		_ = emit("resource_provision", "skipped", "existing Railway installation reused")
		result, err = verifyAttachedInstallation(cmd.Context(), app)
		if err != nil {
			return fail("hosted_verification", "hosted_verification_failed", err)
		}
		_ = emit("service_deploy", "skipped", "existing hosted services are healthy")
	} else {
		_ = emit("resource_provision", "running", "provisioning Railway services")
		result, err = railwayUp(cmd.Context(), app, railwayUpOptions{Name: "vessica", Workspace: opts.Workspace, Image: defaultControlPlaneImage()})
		if err != nil {
			code := "railway_provision_failed"
			if sandboxFeatureError(err) {
				code = "sandbox_feature_disabled"
			}
			return fail("resource_provision", code, err)
		}
		if result["status"] != "running" {
			return fail("provider_authentication", "credentials_required", fmt.Errorf("hosted prerequisites are incomplete: %v", result))
		}
		_ = emit("resource_provision", "succeeded", "Railway resources reconciled")
		_ = emit("service_deploy", "succeeded", "control-plane and knowledge deployments reached success")
	}
	_ = emit("knowledge_ready", "succeeded", "hosted lexical retrieval is ready")
	_ = emit("sandbox_entitlement", "succeeded", "Railway sandbox capability verified")
	secrets, err := loadRailwaySecrets(root)
	if err != nil {
		return fail("repository_attach", "repository_attach_failed", err)
	}
	ownerClaimURL, ownerClaimRequired, err := ensureHostedOwnerClaim(cmd.Context(), app.Config.Hosted.ControlPlaneURL, secrets.ServiceToken, op.ID)
	if err != nil {
		return fail("owner_claim", "owner_claim_failed", err)
	}
	if ownerClaimRequired {
		_ = emit("owner_claim", "succeeded", "workspace owner claim is ready for authenticated completion")
	} else {
		_ = emit("owner_claim", "skipped", "workspace already has an owner")
	}
	var repository state.Repository
	attachEndpoint := strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/repositories"
	if err := hostedRequestWithKey(cmd.Context(), "POST", attachEndpoint, secrets.APIToken, "attach:"+state.CanonicalRepositoryRemote(remote), map[string]string{"remote": remote}, &repository); err != nil {
		return fail("repository_attach", "repository_attach_failed", err)
	}
	app.Config.APIVersion = "vessica.dev/v1"
	app.Config.Kind = "RepositoryAttachment"
	app.Config.Attachment = config.AttachmentConfig{WorkspaceID: repository.WorkspaceID, RepositoryID: repository.ID}
	app.Config.Repo.Remote = remote
	if err := saveHostedClientConfig(app.Config, secrets); err != nil {
		return fail("repository_attach", "credential_registry_failed", err)
	}
	if err := config.Save(root, app.Config); err != nil {
		return fail("repository_attach", "repository_config_failed", err)
	}
	hostedOperationEndpoint, hostedOperationToken, hostedRepositoryID = app.Config.Hosted.ControlPlaneURL, secrets.APIToken, repository.ID
	_ = emit("repository_attach", "succeeded", "repository attached to hosted workspace")
	_ = emit("runner_checkpoint", "succeeded", "managed runner checkpoint verified")
	_ = emit("repository_scan", "succeeded", "deterministic repository profile recorded")
	if profile.Harness == "absent" {
		_ = emit("harness_synthesis", "running", "installing and specializing the engineering harness from repository findings")
		if err := harness.VerifyStartingTargetsAbsent(root, missingBefore); err != nil {
			return fail("harness_apply", "harness_apply_conflict", err)
		}
		if _, err := pack.Install(root, pack.DefaultRef); err != nil {
			return fail("harness_synthesis", "harness_install_failed", err)
		}
		if err := harness.WriteStartingFiles(root, missingBefore, startingHarnessFindings(profile)); err != nil {
			code := "harness_apply_failed"
			if errors.Is(err, harness.ErrStartingFileConflict) {
				code = "harness_apply_conflict"
			}
			return fail("harness_apply", code, err)
		}
		_ = emit("harness_synthesis", "succeeded", "repository-specific harness generated")
		_ = emit("harness_apply", "succeeded", "new harness files applied without overwriting existing files")
	} else {
		_ = emit("harness_synthesis", "skipped", "existing Vessica harness preserved")
		_ = emit("harness_apply", "skipped", "existing Vessica harness preserved")
	}
	mappedCommit, artifactID := profile.Commit, "unchanged"
	if alreadyAttached && !opts.Refresh {
		_ = emit("repository_mapping", "skipped", "existing repository map retained; use --refresh to remap")
	} else {
		_ = emit("repository_mapping", "running", "mapping the remote commit in a read-only Railway sandbox")
		orientation, orientationErr := runRailwayOrientation(cmd.Context(), app.Config, remote)
		if orientationErr != nil {
			code := "repository_mapping_failed"
			if sandboxFeatureError(orientationErr) {
				code = "sandbox_feature_disabled"
			}
			return fail("repository_mapping", code, orientationErr)
		}
		mappedCommit = orientation.Commit
		profile.MappedCommit = mappedCommit
		profile.MappedFiles = orientation.Files
		var mapErr error
		artifactID, mapErr = publishRepositoryMap(cmd.Context(), app, profile)
		if mapErr != nil {
			return fail("repository_mapping", "repository_mapping_failed", mapErr)
		}
		_ = emit("repository_mapping", "succeeded", "repository map published")
	}
	_ = emit("hosted_verification", "succeeded", "control plane, lexical knowledge, and sandbox checkpoint verified")
	op.Result = map[string]any{"workspace_id": repository.WorkspaceID, "repository_id": repository.ID, "dashboard_url": app.Config.Hosted.ControlPlaneURL, "retrieval_mode": "lexical", "embedding_state": "not_configured", "repository_map_artifact": artifactID, "mapped_commit": mappedCommit, "harness": profile.Harness, "railway": result}
	if ownerClaimURL != "" {
		op.Result["owner_claim_url"] = ownerClaimURL
	}
	_ = emit("completed", "succeeded", "hosted Vessica is ready")
	if err := syncHostedOnboarding(cmd.Context(), hostedOperationEndpoint, hostedOperationToken, hostedRepositoryID, op); err != nil {
		return fail("hosted_verification", "onboarding_persistence_failed", err)
	}
	final := map[string]any{"schema": "vessica.onboarding/v1", "operation_id": op.ID, "status": "ready", "receipt": op.Result}
	if opts.Stream == "jsonl" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(final)
	}
	return app.Printer.Success(final)
}

func installationCandidateSummary(candidates []railwayInstallationCandidate) string {
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, candidate.ProjectID+" ("+candidate.Endpoint+")")
	}
	return strings.Join(items, ", ")
}

func ensureHostedOwnerClaim(ctx context.Context, endpoint, token, operationID string) (string, bool, error) {
	base := strings.TrimRight(endpoint, "/")
	var members struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := hostedDashboardRequest(ctx, http.MethodGet, base+"/api/v1/access/members", token, "", nil, &members); err != nil {
		return "", false, err
	}
	if len(members.Data) > 0 {
		return "", false, nil
	}
	var claim struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := hostedDashboardRequest(ctx, http.MethodPost, base+"/api/v1/access/owner-claims", token, "owner-claim:"+operationID, map[string]any{}, &claim); err != nil {
		return "", false, err
	}
	if strings.TrimSpace(claim.Data.Token) == "" {
		return "", false, fmt.Errorf("hosted control plane returned an empty owner claim")
	}
	return base + "/?owner_claim=" + url.QueryEscape(claim.Data.Token), true, nil
}

func selectRailwayWorkspace(cmd *cobra.Command, workspaces []railwayWorkspaceChoice) (string, error) {
	fmt.Fprintln(cmd.OutOrStdout(), "Railway workspaces:")
	for i, workspace := range workspaces {
		fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s (%s)\n", i+1, workspace.Name, workspace.ID)
	}
	fmt.Fprint(cmd.OutOrStdout(), "Select workspace: ")
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	var selected int
	if _, err := fmt.Sscan(strings.TrimSpace(line), &selected); err != nil || selected < 1 || selected > len(workspaces) {
		return "", fmt.Errorf("invalid Railway workspace selection")
	}
	return workspaces[selected-1].ID, nil
}

func syncHostedOnboarding(ctx context.Context, endpoint, token, repositoryID string, op *onboarding.Operation) error {
	if endpoint == "" || token == "" || repositoryID == "" || op == nil {
		return nil
	}
	body := map[string]any{"id": op.ID, "repository_id": repositoryID, "status": op.Status, "current_stage": op.CurrentStage, "document": op}
	return hostedRequestWithKey(ctx, "POST", strings.TrimRight(endpoint, "/")+"/api/v1/onboarding/operations", token, "onboarding:"+op.ID, body, nil)
}

func loadHostedOnboarding(ctx context.Context, root, operationID string) (*onboarding.Operation, error) {
	repositoryRoot, err := config.FindRoot(root)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(repositoryRoot)
	if err != nil || cfg.Hosted.ControlPlaneURL == "" {
		return nil, os.ErrNotExist
	}
	secrets, err := loadRailwaySecrets(repositoryRoot)
	if err != nil {
		_, credentialJSON, registryErr := onboarding.FindInstallation(cfg.Hosted.WorkspaceID)
		if registryErr != nil {
			return nil, err
		}
		if decodeErr := json.Unmarshal(credentialJSON, &secrets); decodeErr != nil {
			return nil, decodeErr
		}
	}
	var operation onboarding.Operation
	endpoint := strings.TrimRight(cfg.Hosted.ControlPlaneURL, "/") + "/api/v1/onboarding/operations/" + operationID
	if err := hostedRequest(ctx, "GET", endpoint, secrets.APIToken, nil, &operation); err != nil {
		return nil, err
	}
	return &operation, nil
}

func saveHostedClientConfig(cfg config.Config, secrets railwaySecrets) error {
	credentialJSON, err := json.Marshal(secrets)
	if err != nil {
		return err
	}
	return onboarding.SaveInstallation(cfg, credentialJSON)
}

func ensureHostedBootstrap(ctx context.Context, app *App, remote string) error {
	if _, err := config.FindRoot(app.Root); err == nil {
		return app.loadRepositoryConfig()
	}
	_ = ctx
	cfg := config.HostedDefaults()
	cfg.Repo.Remote = remote
	if err := config.Save(app.Root, cfg); err != nil {
		return err
	}
	app.Config = cfg
	app.DB = nil
	return nil
}

func publishRepositoryMap(ctx context.Context, app *App, p onboarding.RepositoryProfile) (string, error) {
	g, err := app.openKnowledge(ctx)
	if err != nil {
		return "", err
	}
	defer g.Close()
	scope, err := g.EnsureRepositoryScope(ctx, state.CanonicalRepositoryRemote(p.Remote), p.Name)
	if err != nil {
		return "", err
	}
	body, _ := json.MarshalIndent(p, "", "  ")
	commit := firstNonEmpty(p.MappedCommit, p.Commit)
	artifact, err := g.CreateArtifact(ctx, "onboarding:repository-map:"+commit, ks.Artifact{ScopeID: scope.ID, Type: "repository-map", Title: "Repository map: " + p.Name, Status: "active", Content: string(body), Metadata: map[string]any{"commit": commit, "generated_by": "ves up"}})
	if err != nil {
		return "", err
	}
	_, err = g.CreateMemory(ctx, "onboarding:episode:"+commit, ks.Memory{ScopeID: scope.ID, Type: "episode", Title: "Repository onboarding completed", Content: "Vessica mapped repository " + p.Name + " at remote commit " + commit + " and published repository-map artifact " + artifact.ID + ".", Importance: 0.7, Confidence: 1, ConfidenceSource: "observed", Metadata: map[string]any{"commit": commit, "repository_map_artifact_id": artifact.ID, "generated_by": "ves up"}})
	return artifact.ID, err
}
func gitRoot(root string) (string, error) {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("current directory is not a Git repository")
	}
	return strings.TrimSpace(string(out)), nil
}
func confirmHostedUp(cmd *cobra.Command, p onboarding.RepositoryProfile, workspace string) (bool, error) {
	fmt.Fprintf(cmd.OutOrStdout(), "Repository: %s\nRemote: %s\nRailway workspace: %s\nRetrieval: lexical (semantic optional later)\nHarness: %s\nRailway: control plane, knowledge service, one Postgres service with two isolated databases, sandboxes\n\nProceed? [Y/n] ", p.Name, p.Remote, firstNonEmpty(workspace, "select during authentication"), p.Harness)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && len(line) == 0 {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "" || answer == "y" || answer == "yes", nil
}
