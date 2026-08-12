package newsletter

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (c *RedditCollector) Collect(ctx context.Context, source Source, checkpoint Checkpoint) (Collection, error) {
	token, err := resolveCredential(ctx, c.Credentials, source.CredentialEnv)
	if err != nil {
		return Collection{}, err
	}
	now := time.Now().UTC()
	items, after, seenPages := []Item{}, "", map[string]bool{}
	newest := checkpoint
	for page := 0; page < 20; page++ {
		request, requestErr := newRedditRequest(source, token, after, checkpoint.Fullname)
		if requestErr != nil {
			return Collection{}, requestErr
		}
		response, requestErr := httpClient(c.Client).Do(request.WithContext(ctx))
		if requestErr != nil {
			return Collection{}, fmt.Errorf("collect reddit %s: %w", source.Key, requestErr)
		}
		var payload struct {
			Data struct {
				After    string `json:"after"`
				Children []struct {
					Data struct {
						ID, Name, Title, Permalink, Selftext string
						CreatedUTC                           float64 `json:"created_utc"`
					} `json:"data"`
				} `json:"children"`
			} `json:"data"`
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return Collection{}, fmt.Errorf("collect reddit %s: HTTP %d", source.Key, response.StatusCode)
		}
		requestErr = decodeBounded(response.Body, &payload)
		response.Body.Close()
		if requestErr != nil {
			return Collection{}, requestErr
		}
		reachedWatermark := false
		for _, child := range payload.Data.Children {
			fullname := child.Data.Name
			if fullname == "" {
				fullname = "t3_" + child.Data.ID
			}
			if checkpoint.Fullname != "" && fullname == checkpoint.Fullname {
				reachedWatermark = true
				break
			}
			published := time.Unix(int64(child.Data.CreatedUTC), 0).UTC()
			if checkpoint.NewestAt != "" {
				if previous, parseErr := time.Parse(time.RFC3339, checkpoint.NewestAt); parseErr == nil && !published.After(previous) {
					reachedWatermark = true
					break
				}
			}
			if newest.NewestAt == "" || published.Format(time.RFC3339) > newest.NewestAt {
				newest.Fullname, newest.NewestAt = fullname, published.Format(time.RFC3339)
			}
			itemURL := "https://www.reddit.com" + child.Data.Permalink
			items = append(items, Item{SourceItemID: child.Data.ID, Title: child.Data.Title, URL: itemURL, Content: child.Data.Selftext, PublishedAt: published.Format(time.RFC3339), Trust: UntrustedSourceData, Provenance: provenance(source, itemURL, now)})
		}
		if reachedWatermark || payload.Data.After == "" {
			newest.Cursor = ""
			return Collection{Items: deduplicate(items), Checkpoint: newest}, nil
		}
		if seenPages[payload.Data.After] {
			return Collection{}, fmt.Errorf("collect reddit %s: repeated pagination cursor", source.Key)
		}
		seenPages[payload.Data.After] = true
		after = payload.Data.After
	}
	return Collection{}, fmt.Errorf("collect reddit %s: pagination exceeds 20 pages", source.Key)
}

func (c *XCollector) Collect(ctx context.Context, source Source, checkpoint Checkpoint) (Collection, error) {
	token, err := resolveCredential(ctx, c.Credentials, source.CredentialEnv)
	if err != nil {
		return Collection{}, err
	}
	now := time.Now().UTC()
	items, next, seenPages := []Item{}, "", map[string]bool{}
	newestID := checkpoint.NewestID
	for page := 0; page < 20; page++ {
		request, requestErr := NewXRequest(source, token, checkpoint.NewestID, next)
		if requestErr != nil {
			return Collection{}, requestErr
		}
		response, requestErr := httpClient(c.Client).Do(request.WithContext(ctx))
		if requestErr != nil {
			return Collection{}, fmt.Errorf("collect x %s: %w", source.Key, requestErr)
		}
		var payload struct {
			Data []struct {
				ID        string `json:"id"`
				Text      string `json:"text"`
				CreatedAt string `json:"created_at"`
			} `json:"data"`
			Meta struct {
				NextToken string `json:"next_token"`
				NewestID  string `json:"newest_id"`
			} `json:"meta"`
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return Collection{}, fmt.Errorf("collect x %s: HTTP %d", source.Key, response.StatusCode)
		}
		requestErr = decodeBounded(response.Body, &payload)
		response.Body.Close()
		if requestErr != nil {
			return Collection{}, requestErr
		}
		if page == 0 && payload.Meta.NewestID != "" {
			newestID = payload.Meta.NewestID
		}
		for _, tweet := range payload.Data {
			itemURL := "https://x.com/i/status/" + tweet.ID
			items = append(items, Item{SourceItemID: tweet.ID, URL: itemURL, Content: tweet.Text, PublishedAt: tweet.CreatedAt, Trust: UntrustedSourceData, Provenance: provenance(source, itemURL, now)})
		}
		if payload.Meta.NextToken == "" {
			return Collection{Items: deduplicate(items), Checkpoint: Checkpoint{NewestID: newestID}}, nil
		}
		if seenPages[payload.Meta.NextToken] {
			return Collection{}, fmt.Errorf("collect x %s: repeated pagination cursor", source.Key)
		}
		seenPages[payload.Meta.NextToken] = true
		next = payload.Meta.NextToken
	}
	return Collection{}, fmt.Errorf("collect x %s: pagination exceeds 20 pages", source.Key)
}

func NewRedditRequest(source Source, token, cursor string) (*http.Request, error) {
	return newRedditRequest(source, token, cursor, "")
}

func newRedditRequest(source Source, token, cursor, newestFullname string) (*http.Request, error) {
	if err := validateCredentialReference(source.CredentialEnv); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(source.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "oauth.reddit.com" {
		return nil, fmt.Errorf("reddit source must use https://oauth.reddit.com")
	}
	query := parsed.Query()
	query.Set("raw_json", "1")
	query.Set("limit", "100")
	if cursor != "" {
		query.Set("after", cursor)
	} else if newestFullname != "" {
		query.Set("before", newestFullname)
	}
	parsed.RawQuery = query.Encode()
	request, _ := http.NewRequest(http.MethodGet, parsed.String(), nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", "vessica-newsletter/1.0")
	return request, nil
}

func NewXRequest(source Source, token, sinceID, nextToken string) (*http.Request, error) {
	if err := validateCredentialReference(source.CredentialEnv); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(source.URL)
	if err != nil || parsed.Scheme != "https" || (parsed.Hostname() != "api.x.com" && parsed.Hostname() != "api.twitter.com") {
		return nil, fmt.Errorf("x source must use the HTTPS X API host")
	}
	query := parsed.Query()
	if strings.TrimSpace(source.Query) == "" {
		return nil, fmt.Errorf("x source query is required")
	}
	query.Set("query", source.Query)
	query.Set("max_results", "100")
	query.Set("tweet.fields", "created_at")
	if sinceID != "" {
		query.Set("since_id", sinceID)
	}
	if nextToken != "" {
		query.Set("next_token", nextToken)
	}
	parsed.RawQuery = query.Encode()
	request, _ := http.NewRequest(http.MethodGet, parsed.String(), nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request, nil
}
