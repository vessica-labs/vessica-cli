package tracker

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLinearClientIssueAndMutations(t *testing.T) {
	var operations []string
	var subIssueProject string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "lin_test" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		switch {
		case strings.Contains(request.Query, "VessicaIssueState"):
			operations = append(operations, "state")
			_, _ = w.Write([]byte(`{"data":{"issueUpdate":{"success":true}}}`))
		case strings.Contains(request.Query, "VessicaCommentUpdate"):
			operations = append(operations, "comment-update")
			_, _ = w.Write([]byte(`{"data":{"commentUpdate":{"success":true}}}`))
		case strings.Contains(request.Query, "VessicaComment"):
			operations = append(operations, "comment")
			_, _ = w.Write([]byte(`{"data":{"commentCreate":{"success":true,"comment":{"id":"comment_1","body":"body"}}}}`))
		case strings.Contains(request.Query, "VessicaSubIssue"):
			operations = append(operations, "subissue")
			input, _ := request.Variables["input"].(map[string]any)
			subIssueProject, _ = input["projectId"].(string)
			_, _ = w.Write([]byte(`{"data":{"issueCreate":{"success":true,"issue":{"id":"child_1","identifier":"ENG-2","title":"Child","team":{"id":"team_1"},"state":{"id":"todo"}}}}}`))
		default:
			operations = append(operations, "get")
			_, _ = w.Write([]byte(`{"data":{"issue":{"id":"issue_1","identifier":"ENG-1","title":"Build it","description":"Details","url":"https://linear.app/ENG-1","parent":null,"team":{"id":"team_1","name":"Engineering","key":"ENG"},"state":{"id":"todo","name":"Todo","type":"unstarted"},"labels":{"nodes":[{"id":"label_1","name":"ves:auto"}]}}}}`))
		}
	}))
	defer server.Close()
	client := NewLinearClient("lin_test")
	client.Endpoint = server.URL
	client.HTTPClient = server.Client()
	ctx := context.Background()
	issue, err := client.GetIssue(ctx, "issue_1")
	if err != nil || issue.Identifier != "ENG-1" || !LinearIssueHasLabel(issue, "VES:AUTO") {
		t.Fatalf("issue=%#v err=%v", issue, err)
	}
	if err := client.UpdateIssueState(ctx, issue.ID, "wip"); err != nil {
		t.Fatal(err)
	}
	comment, err := client.CreateComment(ctx, issue.ID, "body")
	if err != nil || comment.ID != "comment_1" {
		t.Fatalf("comment=%#v err=%v", comment, err)
	}
	if err := client.UpdateComment(ctx, comment.ID, "updated"); err != nil {
		t.Fatal(err)
	}
	child, err := client.CreateSubIssue(ctx, issue, "project_1", "Child", "Ticket", "todo")
	if err != nil || child.ID != "child_1" {
		t.Fatalf("child=%#v err=%v", child, err)
	}
	if strings.Join(operations, ",") != "get,state,comment,comment-update,subissue" {
		t.Fatalf("operations=%v", operations)
	}
	if subIssueProject != "project_1" {
		t.Fatalf("sub-issue project=%q", subIssueProject)
	}
}

func TestLinearClientCreatesIssueInProject(t *testing.T) {
	var projectID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		input, _ := request.Variables["input"].(map[string]any)
		projectID, _ = input["projectId"].(string)
		_, _ = w.Write([]byte(`{"data":{"issueCreate":{"success":true,"issue":{"id":"issue_1","identifier":"ENG-1"}}}}`))
	}))
	defer server.Close()
	client := NewLinearClient("lin_test")
	client.Endpoint = server.URL
	client.HTTPClient = server.Client()
	if _, err := client.CreateIssue(context.Background(), "team_1", "project_1", "Title", "Body", "todo", nil); err != nil {
		t.Fatal(err)
	}
	if projectID != "project_1" {
		t.Fatalf("project=%q", projectID)
	}
}

func TestLinearDiscoveryIncludesProjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"teams":{"nodes":[{"id":"team_1","name":"Engineering","key":"ENG","states":{"nodes":[]}}]},"projects":{"nodes":[{"id":"project_1","name":"Launch","slugId":"launch","teams":{"nodes":[{"id":"team_1","name":"Engineering","key":"ENG"}]}}]}}}`))
	}))
	defer server.Close()
	client := NewLinearClient("lin_test")
	client.Endpoint = server.URL
	client.HTTPClient = server.Client()
	discovery, err := client.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Projects) != 1 || discovery.Projects[0].SlugID != "launch" || discovery.Projects[0].Teams.Nodes[0].ID != "team_1" {
		t.Fatalf("projects=%#v", discovery.Projects)
	}
}

func TestVerifyLinearWebhook(t *testing.T) {
	body := []byte(`{"type":"Issue"}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))
	if !VerifyLinearWebhook("secret", body, signature) {
		t.Fatal("valid signature rejected")
	}
	if VerifyLinearWebhook("secret", body, "bad") {
		t.Fatal("invalid signature accepted")
	}
}

func TestLinearClientUsesTokenSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh-access" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":{"teams":{"nodes":[]}}}`))
	}))
	defer server.Close()
	called := 0
	client := NewLinearClientWithTokenSource(func(context.Context) (string, error) {
		called++
		return "fresh-access", nil
	})
	client.Endpoint = server.URL
	client.HTTPClient = server.Client()
	if _, err := client.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("token source called %d times", called)
	}
}

func TestEnsureIssueLabelFindsOrCreatesLabel(t *testing.T) {
	queries := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		queries++
		if strings.Contains(request.Query, "VessicaIssueLabelCreate") {
			_, _ = w.Write([]byte(`{"data":{"issueLabelCreate":{"success":true,"issueLabel":{"id":"label-new","name":"Vessica"}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"issueLabels":{"nodes":[]}}}`))
	}))
	defer server.Close()
	client := NewLinearClient("lin_test")
	client.Endpoint = server.URL
	client.HTTPClient = server.Client()
	label, err := client.EnsureIssueLabel(context.Background(), "team-1", "Vessica")
	if err != nil || label.ID != "label-new" || queries != 2 {
		t.Fatalf("label=%#v queries=%d err=%v", label, queries, err)
	}
}
