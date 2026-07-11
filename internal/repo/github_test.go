package repo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/auth"
)

func TestParsePRNumber(t *testing.T) {
	got, err := ParsePRNumber("https://github.com/vessica-labs/brightwire-mobile-marketing-site-v1/pull/3")
	if err != nil || got != 3 {
		t.Fatalf("got=%d err=%v", got, err)
	}
	if _, err := ParsePRNumber("https://github.com/org/repo/pull/not-a-number"); err == nil {
		t.Fatal("expected invalid PR URL error")
	}
}

func TestAuthenticatedRemoteConvertsGitHubSSHRemote(t *testing.T) {
	got := authenticatedRemote("git@github.com:vessica-labs/brightwire-mobile-marketing-site-v1.git", "test-token")
	want := "https://x-access-token:test-token@github.com/vessica-labs/brightwire-mobile-marketing-site-v1.git"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAuthenticatedRemoteLeavesRemoteWithoutToken(t *testing.T) {
	remote := "git@github.com:vessica-labs/brightwire-mobile-marketing-site-v1.git"
	if got := authenticatedRemote(remote, ""); got != remote {
		t.Fatalf("got %q, want %q", got, remote)
	}
}

func TestGitHubApprovalAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := auth.Login("github", "test-token", "tester"); err != nil {
		t.Fatal(err)
	}
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Errorf("missing GitHub headers: %#v", r.Header)
		}
		switch {
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/acme/demo/pull/7","node_id":"PR_node","number":7,"state":"open","draft":true,"merged":false,"head":{"ref":"vessica/run","sha":"head_sha"}}`))
		case r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.Contains(body["query"].(string), "markPullRequestReadyForReview") {
				t.Errorf("query=%v", body["query"])
			}
			_, _ = w.Write([]byte(`{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false}}}}`))
		case r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["merge_method"] != "squash" || body["sha"] != "head_sha" {
				t.Errorf("merge body=%v", body)
			}
			_, _ = w.Write([]byte(`{"sha":"merge_sha","merged":true,"message":"merged"}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	oldAPI, oldGraphQL, oldClient := githubAPIBaseURL, githubGraphQLURL, githubHTTPClient
	githubAPIBaseURL = server.URL
	githubGraphQLURL = server.URL + "/graphql"
	githubHTTPClient = server.Client()
	defer func() {
		githubAPIBaseURL, githubGraphQLURL, githubHTTPClient = oldAPI, oldGraphQL, oldClient
	}()

	ctx := context.Background()
	details, err := GetPullRequest(ctx, "git@github.com:acme/demo.git", 7)
	if err != nil || details.NodeID != "PR_node" || !details.Draft {
		t.Fatalf("details=%#v err=%v", details, err)
	}
	if err := MarkPullRequestReady(ctx, details.NodeID); err != nil {
		t.Fatal(err)
	}
	merged, err := MergePullRequest(ctx, "git@github.com:acme/demo.git", 7, "squash", "head_sha")
	if err != nil || merged.SHA != "merge_sha" {
		t.Fatalf("merge=%#v err=%v", merged, err)
	}
	if err := DeleteBranch(ctx, "git@github.com:acme/demo.git", "vessica/run"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("calls=%v", calls)
	}
}

func TestCommentAndClosePullRequest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := auth.Login("github", "test-token", "tester"); err != nil {
		t.Fatal(err)
	}
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case http.MethodPatch:
			_, _ = w.Write([]byte(`{"state":"closed"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	oldAPI, oldClient := githubAPIBaseURL, githubHTTPClient
	githubAPIBaseURL, githubHTTPClient = server.URL, server.Client()
	defer func() { githubAPIBaseURL, githubHTTPClient = oldAPI, oldClient }()

	if err := CommentPullRequest(context.Background(), "git@github.com:acme/demo.git", 7, "Rolled back"); err != nil {
		t.Fatal(err)
	}
	if err := ClosePullRequest(context.Background(), "git@github.com:acme/demo.git", 7); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /repos/acme/demo/issues/7/comments", "PATCH /repos/acme/demo/pulls/7"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v want=%v", calls, want)
	}
}
