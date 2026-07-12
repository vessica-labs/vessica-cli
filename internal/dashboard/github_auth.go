package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultGitHubClientID is public OAuth application metadata, not a credential.
// Forks may override it with VES_GITHUB_OAUTH_CLIENT_ID.
const DefaultGitHubClientID = "Ov23liGzoiGwD9tvXQGC"

type githubFlow struct {
	DeviceCode, OwnerClaimHash, InvitationHash string
	ExpiresAt                                  time.Time
	Interval                                   int
}
type githubDeviceResponse struct {
	DeviceCode       string `json:"device_code"`
	UserCode         string `json:"user_code"`
	VerificationURI  string `json:"verification_uri"`
	ExpiresIn        int    `json:"expires_in"`
	Interval         int    `json:"interval"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (s *Server) handleAuthConfig(w http.ResponseWriter, _ *http.Request) {
	s.ok(w, map[string]any{"mode": s.Mode, "github_configured": strings.TrimSpace(s.GitHubClientID) != ""}, nil)
}
func (s *Server) handleGitHubDevice(w http.ResponseWriter, r *http.Request) {
	if s.Mode != "hosted" {
		s.fail(w, r, 404, "not_found", "hosted GitHub login unavailable", nil)
		return
	}
	if strings.TrimSpace(s.GitHubClientID) == "" {
		s.fail(w, r, 503, "github_oauth_unconfigured", "GitHub OAuth device flow is not configured", nil)
		return
	}
	var body struct {
		OwnerClaim string `json:"owner_claim"`
		Invitation string `json:"invitation"`
	}
	if !s.decode(w, r, &body, 16<<10) {
		return
	}
	form := url.Values{"client_id": {s.GitHubClientID}, "scope": {"read:user"}}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://github.com/login/device/code", strings.NewReader(form.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, e := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if e != nil {
		s.internal(w, r, e)
		return
	}
	defer resp.Body.Close()
	var device githubDeviceResponse
	if e = json.NewDecoder(resp.Body).Decode(&device); e != nil || device.DeviceCode == "" {
		s.fail(w, r, 502, "github_device_failed", device.ErrorDescription, nil)
		return
	}
	flowID := token(18)
	s.mu.Lock()
	s.githubFlows[flowID] = githubFlow{DeviceCode: device.DeviceCode, OwnerClaimHash: digest(body.OwnerClaim), InvitationHash: digest(body.Invitation), ExpiresAt: time.Now().Add(time.Duration(device.ExpiresIn) * time.Second), Interval: device.Interval}
	s.mu.Unlock()
	s.ok(w, map[string]any{"id": flowID, "user_code": device.UserCode, "verification_uri": device.VerificationURI, "expires_in": device.ExpiresIn, "interval": device.Interval}, nil)
}
func (s *Server) handleGitHubPoll(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	flow, ok := s.githubFlows[r.PathValue("id")]
	s.mu.Unlock()
	if !ok || time.Now().After(flow.ExpiresAt) {
		s.fail(w, r, 410, "github_flow_expired", "GitHub sign-in expired", nil)
		return
	}
	form := url.Values{"client_id": {s.GitHubClientID}, "device_code": {flow.DeviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, e := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if e != nil {
		s.internal(w, r, e)
		return
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&result); e != nil {
		s.internal(w, r, e)
		return
	}
	if result.AccessToken == "" {
		if result.Error == "authorization_pending" || result.Error == "slow_down" {
			retry := flow.Interval
			if retry < 5 {
				retry = 5
			}
			s.okStatus(w, http.StatusAccepted, map[string]any{"status": result.Error, "retry_after": retry}, nil)
			return
		}
		s.fail(w, r, 401, "github_login_failed", result.ErrorDescription, nil)
		return
	}
	userReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com/user", nil)
	userReq.Header.Set("Authorization", "Bearer "+result.AccessToken)
	userReq.Header.Set("Accept", "application/vnd.github+json")
	userResp, e := (&http.Client{Timeout: 20 * time.Second}).Do(userReq)
	if e != nil {
		s.internal(w, r, e)
		return
	}
	defer userResp.Body.Close()
	var identity struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if e = json.NewDecoder(userResp.Body).Decode(&identity); e != nil || identity.ID == 0 {
		s.fail(w, r, 502, "github_identity_failed", "GitHub identity could not be read", nil)
		return
	}
	user, e := s.DB.UpsertDashboardUser(r.Context(), fmt.Sprint(identity.ID), identity.Login, identity.Name, identity.AvatarURL)
	if e != nil {
		s.internal(w, r, e)
		return
	}
	membership, e := s.DB.GetMembership(r.Context(), user.ID)
	if e != nil && flow.OwnerClaimHash != digest("") {
		e = s.DB.ClaimOwner(r.Context(), flow.OwnerClaimHash, user.ID)
		if e == nil {
			membership, _ = s.DB.GetMembership(r.Context(), user.ID)
		}
	}
	if e != nil && flow.InvitationHash != digest("") {
		_, e = s.DB.AcceptInvitation(r.Context(), flow.InvitationHash, identity.Login, user.ID)
		if e == nil {
			membership, _ = s.DB.GetMembership(r.Context(), user.ID)
		}
	}
	if membership == nil {
		s.fail(w, r, 403, "membership_required", "This GitHub identity has not been invited to the workspace", nil)
		return
	}
	sessionRaw, csrfRaw := token(32), token(32)
	session, e := s.DB.CreateDashboardSession(r.Context(), user.ID, membership.Role, digest(sessionRaw), digest(csrfRaw), time.Now().Add(12*time.Hour).UTC().Format(time.RFC3339Nano))
	if e != nil {
		s.internal(w, r, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: sessionRaw, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(12 * time.Hour)})
	s.mu.Lock()
	delete(s.githubFlows, r.PathValue("id"))
	s.mu.Unlock()
	_ = s.DB.AppendDashboardAudit(r.Context(), user.ID, "session.login", "session", session.ID, r.Header.Get("X-Request-ID"), map[string]any{"provider": "github", "login": identity.Login})
	s.ok(w, map[string]any{"status": "completed", "csrf_token": csrfRaw, "user": map[string]any{"id": user.ID, "login": identity.Login, "role": membership.Role}}, nil)
}
