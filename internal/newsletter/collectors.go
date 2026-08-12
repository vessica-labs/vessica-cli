package newsletter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	UntrustedSourceData = "untrusted_source_data"
	maxResponseBytes    = 4 << 20
)

var (
	envReferencePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,127}$`)
	titlePattern        = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	descriptionPattern  = regexp.MustCompile(`(?is)<meta[^>]+name=["']description["'][^>]+content=["']([^"']*)["']`)
	scriptPattern       = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	tagPattern          = regexp.MustCompile(`(?s)<[^>]+>`)
	spacePattern        = regexp.MustCompile(`\s+`)
)

type Source struct {
	Key           string `json:"key"`
	Type          string `json:"type"`
	URL           string `json:"url"`
	Query         string `json:"query,omitempty"`
	CredentialEnv string `json:"credential_env,omitempty"`
}

type Checkpoint struct {
	Cursor       string `json:"cursor,omitempty"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	Fullname     string `json:"fullname,omitempty"`
	NewestAt     string `json:"newest_at,omitempty"`
	NewestID     string `json:"newest_id,omitempty"`
}

type Provenance struct {
	SourceKey   string `json:"source_key"`
	SourceType  string `json:"source_type"`
	URL         string `json:"url"`
	CollectedAt string `json:"collected_at"`
}

type Item struct {
	SourceItemID string     `json:"source_item_id"`
	Title        string     `json:"title"`
	URL          string     `json:"url"`
	Content      string     `json:"content"`
	PublishedAt  string     `json:"published_at,omitempty"`
	Trust        string     `json:"trust"`
	Provenance   Provenance `json:"provenance"`
}

type Collection struct {
	Items      []Item     `json:"items"`
	Checkpoint Checkpoint `json:"checkpoint"`
}

type Collector interface {
	Collect(context.Context, Source, Checkpoint) (Collection, error)
}

type CredentialResolver interface {
	ResolveCredential(context.Context, string) (string, error)
}

type EnvironmentCredentials struct{}

func (EnvironmentCredentials) ResolveCredential(_ context.Context, reference string) (string, error) {
	if err := validateCredentialReference(reference); err != nil {
		return "", err
	}
	value, ok := os.LookupEnv(reference)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("credential reference %s is unavailable", reference)
	}
	return value, nil
}

type FeedCollector struct{ Client *http.Client }
type WebCollector struct{ Client *http.Client }
type RedditCollector struct {
	Client      *http.Client
	Credentials CredentialResolver
}
type XCollector struct {
	Client      *http.Client
	Credentials CredentialResolver
}

func (c *FeedCollector) Collect(ctx context.Context, source Source, checkpoint Checkpoint) (Collection, error) {
	request, err := sourceRequest(ctx, source.URL, checkpoint)
	if err != nil {
		return Collection{}, err
	}
	response, err := httpClient(c.Client).Do(request)
	if err != nil {
		return Collection{}, fmt.Errorf("collect feed %s: %w", source.Key, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return Collection{Checkpoint: checkpoint}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Collection{}, fmt.Errorf("collect feed %s: HTTP %d", source.Key, response.StatusCode)
	}
	body, err := readBounded(response.Body)
	if err != nil {
		return Collection{}, err
	}
	items, err := parseFeed(body, source, time.Now().UTC())
	if err != nil {
		return Collection{}, fmt.Errorf("parse feed %s: %w", source.Key, err)
	}
	return Collection{Items: deduplicate(items), Checkpoint: responseCheckpoint(response, checkpoint)}, nil
}

func (c *WebCollector) Collect(ctx context.Context, source Source, checkpoint Checkpoint) (Collection, error) {
	request, err := sourceRequest(ctx, source.URL, checkpoint)
	if err != nil {
		return Collection{}, err
	}
	response, err := httpClient(c.Client).Do(request)
	if err != nil {
		return Collection{}, fmt.Errorf("collect web %s: %w", source.Key, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return Collection{Checkpoint: checkpoint}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Collection{}, fmt.Errorf("collect web %s: HTTP %d", source.Key, response.StatusCode)
	}
	body, err := readBounded(response.Body)
	if err != nil {
		return Collection{}, err
	}
	text := string(body)
	title := firstMatch(titlePattern, text)
	description := firstMatch(descriptionPattern, text)
	visible := cleanHTML(text)
	if description != "" && !strings.Contains(visible, description) {
		visible = strings.TrimSpace(description + "\n\n" + visible)
	}
	if len(visible) > 64*1024 {
		visible = visible[:64*1024]
	}
	collected := time.Now().UTC()
	id := stableID(source.URL, response.Header.Get("ETag"), response.Header.Get("Last-Modified"), visible)
	item := Item{SourceItemID: id, Title: title, URL: source.URL, Content: visible, Trust: UntrustedSourceData, Provenance: provenance(source, source.URL, collected)}
	return Collection{Items: []Item{item}, Checkpoint: responseCheckpoint(response, checkpoint)}, nil
}

type feedEnvelope struct {
	Channel struct {
		Items []struct {
			GUID        string `xml:"guid"`
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
	Entries []struct {
		ID        string `xml:"id"`
		Title     string `xml:"title"`
		Summary   string `xml:"summary"`
		Content   string `xml:"content"`
		Updated   string `xml:"updated"`
		Published string `xml:"published"`
		Link      struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func parseFeed(body []byte, source Source, collected time.Time) ([]Item, error) {
	var feed feedEnvelope
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(feed.Channel.Items)+len(feed.Entries))
	for _, value := range feed.Channel.Items {
		itemID := strings.TrimSpace(value.GUID)
		if itemID == "" {
			itemID = stableID(value.Link, value.Title)
		}
		items = append(items, Item{SourceItemID: itemID, Title: cleanText(value.Title), URL: strings.TrimSpace(value.Link), Content: cleanHTML(value.Description), PublishedAt: normalizeDate(value.PubDate), Trust: UntrustedSourceData, Provenance: provenance(source, strings.TrimSpace(value.Link), collected)})
	}
	for _, value := range feed.Entries {
		itemID := strings.TrimSpace(value.ID)
		if itemID == "" {
			itemID = stableID(value.Link.Href, value.Title)
		}
		content := value.Summary
		if content == "" {
			content = value.Content
		}
		published := value.Published
		if published == "" {
			published = value.Updated
		}
		items = append(items, Item{SourceItemID: itemID, Title: cleanText(value.Title), URL: strings.TrimSpace(value.Link.Href), Content: cleanHTML(content), PublishedAt: normalizeDate(published), Trust: UntrustedSourceData, Provenance: provenance(source, strings.TrimSpace(value.Link.Href), collected)})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("feed contains no items")
	}
	return items, nil
}

func sourceRequest(ctx context.Context, rawURL string, checkpoint Checkpoint) (*http.Request, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("source URL is invalid")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil, fmt.Errorf("public sources require HTTPS")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, text/html")
	if checkpoint.ETag != "" {
		request.Header.Set("If-None-Match", checkpoint.ETag)
	}
	if checkpoint.LastModified != "" {
		request.Header.Set("If-Modified-Since", checkpoint.LastModified)
	}
	return request, nil
}

func isLoopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func readBounded(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("source response exceeds %d bytes", maxResponseBytes)
	}
	return body, nil
}

func decodeBounded(reader io.Reader, value any) error {
	body, err := readBounded(reader)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(body, value); err != nil {
		return fmt.Errorf("decode source response: %w", err)
	}
	return nil
}

func responseCheckpoint(response *http.Response, previous Checkpoint) Checkpoint {
	checkpoint := previous
	if value := response.Header.Get("ETag"); value != "" {
		checkpoint.ETag = value
	}
	if value := response.Header.Get("Last-Modified"); value != "" {
		checkpoint.LastModified = value
	}
	return checkpoint
}

func httpClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return NewPublicHTTPClient()
}

func resolveCredential(ctx context.Context, resolver CredentialResolver, ref string) (string, error) {
	if err := validateCredentialReference(ref); err != nil {
		return "", err
	}
	if resolver == nil {
		return "", fmt.Errorf("credential resolver is not configured")
	}
	value, err := resolver.ResolveCredential(ctx, ref)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("credential reference %s is unavailable", ref)
	}
	return value, nil
}

func validateCredentialReference(value string) error {
	if !envReferencePattern.MatchString(value) {
		return fmt.Errorf("credential_env must be an environment variable reference")
	}
	return nil
}

func provenance(source Source, itemURL string, collected time.Time) Provenance {
	return Provenance{SourceKey: source.Key, SourceType: source.Type, URL: itemURL, CollectedAt: collected.Format(time.RFC3339Nano)}
}

func stableID(values ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(hash[:])
}

func cleanText(value string) string {
	return strings.TrimSpace(spacePattern.ReplaceAllString(html.UnescapeString(value), " "))
}

func cleanHTML(value string) string {
	value = scriptPattern.ReplaceAllString(value, " ")
	value = tagPattern.ReplaceAllString(value, " ")
	return cleanText(value)
}

func firstMatch(pattern *regexp.Regexp, value string) string {
	match := pattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return cleanText(match[1])
}

func normalizeDate(value string) string {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func deduplicate(items []Item) []Item {
	byID := map[string]Item{}
	for _, item := range items {
		if strings.TrimSpace(item.SourceItemID) != "" {
			byID[item.SourceItemID] = item
		}
	}
	ids := make([]string, 0, len(byID))
	for itemID := range byID {
		ids = append(ids, itemID)
	}
	sort.Strings(ids)
	out := make([]Item, 0, len(ids))
	for _, itemID := range ids {
		out = append(out, byID[itemID])
	}
	return out
}
