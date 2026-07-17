package tracker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const linearGraphQLEndpoint = "https://api.linear.app/graphql"

type LinearClient struct {
	Token       string
	TokenSource func(context.Context) (string, error)
	Endpoint    string
	HTTPClient  *http.Client
}

type LinearTeam struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}

type LinearWorkflowState struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type LinearProject struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	SlugID string `json:"slugId"`
	Teams  struct {
		Nodes []LinearTeam `json:"nodes"`
	} `json:"teams"`
}

type LinearLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type LinearIssue struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Parent      *struct {
		ID string `json:"id"`
	} `json:"parent"`
	Team   LinearTeam          `json:"team"`
	State  LinearWorkflowState `json:"state"`
	Labels struct {
		Nodes []LinearLabel `json:"nodes"`
	} `json:"labels"`
}

type LinearComment struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}

type LinearWebhook struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

type LinearDiscovery struct {
	Teams    []LinearTeam
	Projects []LinearProject
	States   map[string][]LinearWorkflowState
}

func (c *LinearClient) EnsureIssueLabel(ctx context.Context, teamID, name string) (*LinearLabel, error) {
	name = strings.TrimSpace(name)
	if teamID == "" || name == "" {
		return nil, fmt.Errorf("Linear team and label name are required")
	}
	var existing struct {
		IssueLabels struct {
			Nodes []LinearLabel `json:"nodes"`
		} `json:"issueLabels"`
	}
	query := `query VessicaIssueLabels($team: ID!) { issueLabels(first: 100, filter: { team: { id: { eq: $team } } }) { nodes { id name } } }`
	if err := c.graphql(ctx, query, map[string]any{"team": teamID}, &existing); err != nil {
		return nil, err
	}
	for i := range existing.IssueLabels.Nodes {
		if strings.EqualFold(existing.IssueLabels.Nodes[i].Name, name) {
			return &existing.IssueLabels.Nodes[i], nil
		}
	}
	var created struct {
		IssueLabelCreate struct {
			Success    bool        `json:"success"`
			IssueLabel LinearLabel `json:"issueLabel"`
		} `json:"issueLabelCreate"`
	}
	mutation := `mutation VessicaIssueLabelCreate($input: IssueLabelCreateInput!) { issueLabelCreate(input: $input) { success issueLabel { id name } } }`
	input := map[string]any{"teamId": teamID, "name": name, "description": "Issues queued for Vessica", "color": "#5E6AD2"}
	if err := c.graphql(ctx, mutation, map[string]any{"input": input}, &created); err != nil {
		return nil, err
	}
	if !created.IssueLabelCreate.Success || created.IssueLabelCreate.IssueLabel.ID == "" {
		return nil, fmt.Errorf("Linear issue label creation was not successful")
	}
	return &created.IssueLabelCreate.IssueLabel, nil
}

func NewLinearClient(token string) *LinearClient {
	return &LinearClient{Token: strings.TrimSpace(token), Endpoint: linearGraphQLEndpoint, HTTPClient: &http.Client{Timeout: 20 * time.Second}}
}

func NewLinearClientWithTokenSource(source func(context.Context) (string, error)) *LinearClient {
	return &LinearClient{TokenSource: source, Endpoint: linearGraphQLEndpoint, HTTPClient: &http.Client{Timeout: 20 * time.Second}}
}

func (c *LinearClient) Discover(ctx context.Context) (*LinearDiscovery, error) {
	var data struct {
		Teams struct {
			Nodes []struct {
				LinearTeam
				States struct {
					Nodes []LinearWorkflowState `json:"nodes"`
				} `json:"states"`
			} `json:"nodes"`
		} `json:"teams"`
		Projects struct {
			Nodes []LinearProject `json:"nodes"`
		} `json:"projects"`
	}
	if err := c.graphql(ctx, `query VessicaTrackerDiscovery { teams { nodes { id name key states { nodes { id name type } } } } projects(first: 100) { nodes { id name slugId teams { nodes { id name key } } } } }`, nil, &data); err != nil {
		return nil, err
	}
	discovery := &LinearDiscovery{States: map[string][]LinearWorkflowState{}}
	for _, team := range data.Teams.Nodes {
		discovery.Teams = append(discovery.Teams, team.LinearTeam)
		discovery.States[team.ID] = team.States.Nodes
	}
	discovery.Projects = data.Projects.Nodes
	return discovery, nil
}

func (c *LinearClient) GetIssue(ctx context.Context, issueID string) (*LinearIssue, error) {
	var data struct {
		Issue LinearIssue `json:"issue"`
	}
	query := `query VessicaIssue($id: String!) { issue(id: $id) { id identifier title description url parent { id } team { id name key } state { id name type } labels { nodes { id name } } } }`
	if err := c.graphql(ctx, query, map[string]any{"id": issueID}, &data); err != nil {
		return nil, err
	}
	if data.Issue.ID == "" {
		return nil, fmt.Errorf("Linear issue not found: %s", issueID)
	}
	return &data.Issue, nil
}

func (c *LinearClient) ListIssuesInState(ctx context.Context, teamID, stateID string) ([]LinearIssue, error) {
	var data struct {
		Issues struct {
			Nodes []LinearIssue `json:"nodes"`
		} `json:"issues"`
	}
	query := `query VessicaTriggerIssues($team: ID!, $state: ID!) { issues(filter: { team: { id: { eq: $team } }, state: { id: { eq: $state } } }, first: 100) { nodes { id identifier title description url parent { id } team { id name key } state { id name type } labels { nodes { id name } } } } }`
	if err := c.graphql(ctx, query, map[string]any{"team": teamID, "state": stateID}, &data); err != nil {
		return nil, err
	}
	return data.Issues.Nodes, nil
}

func (c *LinearClient) UpdateIssueState(ctx context.Context, issueID, stateID string) error {
	var data struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	query := `mutation VessicaIssueState($id: String!, $stateId: String!) { issueUpdate(id: $id, input: { stateId: $stateId }) { success } }`
	if err := c.graphql(ctx, query, map[string]any{"id": issueID, "stateId": stateID}, &data); err != nil {
		return err
	}
	if !data.IssueUpdate.Success {
		return fmt.Errorf("Linear issue state update was not successful")
	}
	return nil
}

func (c *LinearClient) CreateComment(ctx context.Context, issueID, body string) (*LinearComment, error) {
	var data struct {
		CommentCreate struct {
			Success bool          `json:"success"`
			Comment LinearComment `json:"comment"`
		} `json:"commentCreate"`
	}
	query := `mutation VessicaComment($issueId: String!, $body: String!) { commentCreate(input: { issueId: $issueId, body: $body }) { success comment { id body } } }`
	if err := c.graphql(ctx, query, map[string]any{"issueId": issueID, "body": body}, &data); err != nil {
		return nil, err
	}
	if !data.CommentCreate.Success {
		return nil, fmt.Errorf("Linear comment creation was not successful")
	}
	return &data.CommentCreate.Comment, nil
}

func (c *LinearClient) UpdateComment(ctx context.Context, commentID, body string) error {
	var data struct {
		CommentUpdate struct {
			Success bool `json:"success"`
		} `json:"commentUpdate"`
	}
	query := `mutation VessicaCommentUpdate($id: String!, $body: String!) { commentUpdate(id: $id, input: { body: $body }) { success } }`
	if err := c.graphql(ctx, query, map[string]any{"id": commentID, "body": body}, &data); err != nil {
		return err
	}
	if !data.CommentUpdate.Success {
		return fmt.Errorf("Linear comment update was not successful")
	}
	return nil
}

func (c *LinearClient) CreateSubIssue(ctx context.Context, parent *LinearIssue, projectID, title, description, stateID string) (*LinearIssue, error) {
	var data struct {
		IssueCreate struct {
			Success bool        `json:"success"`
			Issue   LinearIssue `json:"issue"`
		} `json:"issueCreate"`
	}
	input := map[string]any{"teamId": parent.Team.ID, "parentId": parent.ID, "title": title, "description": description}
	if projectID != "" {
		input["projectId"] = projectID
	}
	if stateID != "" {
		input["stateId"] = stateID
	}
	query := `mutation VessicaSubIssue($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { id identifier title description url state { id name type } team { id name key } } } }`
	if err := c.graphql(ctx, query, map[string]any{"input": input}, &data); err != nil {
		return nil, err
	}
	if !data.IssueCreate.Success {
		return nil, fmt.Errorf("Linear sub-issue creation was not successful")
	}
	return &data.IssueCreate.Issue, nil
}

func (c *LinearClient) CreateIssue(ctx context.Context, teamID, projectID, title, description, stateID string, labelIDs []string) (*LinearIssue, error) {
	var data struct {
		IssueCreate struct {
			Success bool        `json:"success"`
			Issue   LinearIssue `json:"issue"`
		} `json:"issueCreate"`
	}
	input := map[string]any{"teamId": teamID, "title": title, "description": description}
	if projectID != "" {
		input["projectId"] = projectID
	}
	if stateID != "" {
		input["stateId"] = stateID
	}
	if len(labelIDs) > 0 {
		input["labelIds"] = labelIDs
	}
	query := `mutation VessicaIssue($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { id identifier title description url state { id name type } team { id name key } } } }`
	if err := c.graphql(ctx, query, map[string]any{"input": input}, &data); err != nil {
		return nil, err
	}
	if !data.IssueCreate.Success || data.IssueCreate.Issue.ID == "" {
		return nil, fmt.Errorf("Linear issue creation was not successful")
	}
	return &data.IssueCreate.Issue, nil
}

func (c *LinearClient) CreateWebhook(ctx context.Context, teamID, callbackURL, secret string) (*LinearWebhook, error) {
	var data struct {
		WebhookCreate struct {
			Success bool          `json:"success"`
			Webhook LinearWebhook `json:"webhook"`
		} `json:"webhookCreate"`
	}
	input := map[string]any{"url": callbackURL, "teamId": teamID, "resourceTypes": []string{"Issue"}, "enabled": true, "label": "Vessica control plane"}
	if strings.TrimSpace(secret) != "" {
		input["secret"] = secret
	}
	query := `mutation VessicaWebhook($input: WebhookCreateInput!) { webhookCreate(input: $input) { success webhook { id enabled } } }`
	if err := c.graphql(ctx, query, map[string]any{"input": input}, &data); err != nil {
		return nil, err
	}
	if !data.WebhookCreate.Success {
		return nil, fmt.Errorf("Linear webhook creation was not successful")
	}
	return &data.WebhookCreate.Webhook, nil
}

func (c *LinearClient) graphql(ctx context.Context, query string, variables map[string]any, target any) error {
	token := strings.TrimSpace(c.Token)
	oauth := false
	if c.TokenSource != nil {
		var err error
		token, err = c.TokenSource(ctx)
		if err != nil {
			return fmt.Errorf("get Linear access token: %w", err)
		}
		oauth = true
	}
	if token == "" {
		return fmt.Errorf("Linear API token is required")
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = linearGraphQLEndpoint
	}
	body, _ := json.Marshal(map[string]any{"query": query, "variables": variables})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if oauth {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set("Authorization", token)
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Linear API failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("Linear API: %s", envelope.Errors[0].Message)
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(envelope.Data, target)
}

func VerifyLinearWebhook(secret string, body []byte, signature string) bool {
	secret = strings.TrimSpace(secret)
	signature = strings.TrimSpace(signature)
	if secret == "" || signature == "" {
		return false
	}
	expected := hmac.New(sha256.New, []byte(secret))
	_, _ = expected.Write(body)
	provided, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return hmac.Equal(expected.Sum(nil), provided)
}

func LinearIssueHasLabel(issue *LinearIssue, label string) bool {
	label = strings.TrimSpace(label)
	if label == "" {
		return true
	}
	for _, candidate := range issue.Labels.Nodes {
		if strings.EqualFold(strings.TrimSpace(candidate.Name), label) {
			return true
		}
	}
	return false
}
