package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/auth"
)

type PRRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

type PRResponse struct {
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Draft   bool   `json:"draft"`
}

type PRDetails struct {
	HTMLURL        string `json:"html_url"`
	NodeID         string `json:"node_id"`
	Number         int    `json:"number"`
	State          string `json:"state"`
	Draft          bool   `json:"draft"`
	Merged         bool   `json:"merged"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	Head           struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

type MergeResult struct {
	SHA     string `json:"sha"`
	Merged  bool   `json:"merged"`
	Message string `json:"message"`
}

var (
	githubAPIBaseURL = "https://api.github.com"
	githubGraphQLURL = "https://api.github.com/graphql"
	githubHTTPClient = http.DefaultClient
)

func ParsePRNumber(prURL string) (int, error) {
	n, err := strconv.Atoi(path.Base(strings.TrimRight(strings.TrimSpace(prURL), "/")))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid GitHub PR URL: %s", prURL)
	}
	return n, nil
}

func ParseGitHubRemote(remote string) (owner, name string, err error) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(remote, "git@github.com:"):
		path := strings.TrimPrefix(remote, "git@github.com:")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid github remote: %s", remote)
		}
		return parts[0], parts[1], nil
	case strings.Contains(remote, "github.com/"):
		i := strings.Index(remote, "github.com/")
		path := remote[i+len("github.com/"):]
		parts := strings.Split(path, "/")
		if len(parts) < 2 {
			return "", "", fmt.Errorf("invalid github remote: %s", remote)
		}
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("not a github remote: %s", remote)
	}
}

func CreateDraftPR(ctx context.Context, remote, head, base, title, body string) (*PRResponse, error) {
	if base == "" {
		base = "main"
	}
	owner, repo, err := ParseGitHubRemote(remote)
	if err != nil {
		return nil, err
	}
	token, err := auth.Token("github")
	if err != nil {
		return nil, err
	}
	reqBody := PRRequest{Title: title, Body: body, Head: head, Base: base, Draft: true}
	b, _ := json.Marshal(reqBody)
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls", githubAPIBaseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	setGitHubHeaders(req, token)
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github PR create failed (%d): %s", resp.StatusCode, string(data))
	}
	var pr PRResponse
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func UpdatePRBody(ctx context.Context, remote string, number int, body string) error {
	owner, repo, err := ParseGitHubRemote(remote)
	if err != nil {
		return err
	}
	token, err := auth.Token("github")
	if err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]string{"body": body})
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPIBaseURL, owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	setGitHubHeaders(req, token)
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("github PR update failed (%d): %s", resp.StatusCode, string(data))
	}
	return nil
}

func CommentPullRequest(ctx context.Context, remote string, number int, body string) error {
	owner, name, err := ParseGitHubRemote(remote)
	if err != nil {
		return err
	}
	token, _ := auth.Token("github")
	if token == "" {
		return fmt.Errorf("github token unavailable")
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	endpoint := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", githubAPIBaseURL, owner, name, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	setGitHubHeaders(req, token)
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("github PR comment failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func ClosePullRequest(ctx context.Context, remote string, number int) error {
	owner, name, err := ParseGitHubRemote(remote)
	if err != nil {
		return err
	}
	token, _ := auth.Token("github")
	if token == "" {
		return fmt.Errorf("github token unavailable")
	}
	payload, _ := json.Marshal(map[string]string{"state": "closed"})
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPIBaseURL, owner, name, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	setGitHubHeaders(req, token)
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github PR close failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func GetPullRequest(ctx context.Context, remote string, number int) (*PRDetails, error) {
	owner, repoName, err := ParseGitHubRemote(remote)
	if err != nil {
		return nil, err
	}
	token, err := auth.Token("github")
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPIBaseURL, owner, repoName, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setGitHubHeaders(req, token)
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github PR lookup failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var details PRDetails
	if err := json.Unmarshal(data, &details); err != nil {
		return nil, err
	}
	return &details, nil
}

func MarkPullRequestReady(ctx context.Context, nodeID string) error {
	token, err := auth.Token("github")
	if err != nil {
		return err
	}
	body := map[string]any{
		"query":     `mutation MarkReady($id: ID!) { markPullRequestReadyForReview(input: {pullRequestId: $id}) { pullRequest { isDraft } } }`,
		"variables": map[string]string{"id": nodeID},
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubGraphQLURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	setGitHubHeaders(req, token)
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("github mark-ready failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var result struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("github mark-ready failed: %s", result.Errors[0].Message)
	}
	return nil
}

func MergePullRequest(ctx context.Context, remote string, number int, method, expectedSHA string) (*MergeResult, error) {
	owner, repoName, err := ParseGitHubRemote(remote)
	if err != nil {
		return nil, err
	}
	token, err := auth.Token("github")
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(map[string]string{"merge_method": method, "sha": expectedSHA})
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/merge", githubAPIBaseURL, owner, repoName, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	setGitHubHeaders(req, token)
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result MergeResult
	_ = json.Unmarshal(data, &result)
	if resp.StatusCode >= 300 || !result.Merged {
		message := strings.TrimSpace(result.Message)
		if message == "" {
			message = strings.TrimSpace(string(data))
		}
		return nil, fmt.Errorf("github PR merge failed (%d): %s", resp.StatusCode, message)
	}
	return &result, nil
}

func DeleteBranch(ctx context.Context, remote, branch string) error {
	owner, repoName, err := ParseGitHubRemote(remote)
	if err != nil {
		return err
	}
	token, err := auth.Token("github")
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/git/refs/heads/%s", githubAPIBaseURL, owner, repoName, url.PathEscape(branch))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	setGitHubHeaders(req, token)
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("github branch delete failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func setGitHubHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
}

func PushBranch(ctx context.Context, workdir, remote, branch string) error {
	pushURL := AuthenticatedRemote(remote)
	cmds := [][]string{
		{"git", "push", "-u", pushURL, branch},
	}
	for _, c := range cmds {
		cmd := GitCommandContext(ctx, c[1:]...)
		cmd.Dir = workdir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git push: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func AuthenticatedRemote(remote string) string {
	token, _ := auth.Token("github")
	return authenticatedRemote(remote, token)
}

func authenticatedRemote(remote, token string) string {
	if strings.TrimSpace(token) == "" {
		return remote
	}
	if owner, name, err := ParseGitHubRemote(remote); err == nil {
		return fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, owner, name)
	}
	return remote
}

func DefaultBranch(ctx context.Context, workdir string) string {
	out, err := GitCommandContext(ctx, "-C", workdir, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		s := strings.TrimSpace(string(out))
		if i := strings.LastIndex(s, "/"); i >= 0 {
			return s[i+1:]
		}
	}
	for _, b := range []string{"main", "master"} {
		if GitCommandContext(ctx, "-C", workdir, "rev-parse", "--verify", b).Run() == nil {
			return b
		}
	}
	return "main"
}

func Status(ctx context.Context, workdir, remote string) map[string]any {
	owner, name, err := ParseGitHubRemote(remote)
	m := map[string]any{"remote": remote, "provider": "github"}
	if err == nil {
		m["owner"] = owner
		m["repo"] = name
	}
	if workdir != "" {
		out, _ := GitCommandContext(ctx, "-C", workdir, "status", "-sb").Output()
		m["git_status"] = strings.TrimSpace(string(out))
	}
	if _, err := auth.Token("github"); err == nil {
		m["authenticated"] = true
	} else {
		m["authenticated"] = false
	}
	return m
}
