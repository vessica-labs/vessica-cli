package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleMembers(w http.ResponseWriter, r *http.Request) {
	v, e := s.DB.ListMemberships(r.Context())
	s.respond(w, r, v, e)
}
func (s *Server) handleInvitation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GitHubLogin string `json:"github_login"`
	}
	if !s.decode(w, r, &body, 16<<10) {
		return
	}
	login := strings.TrimSpace(body.GitHubLogin)
	if login == "" {
		s.fail(w, r, 400, "invalid_login", "GitHub login required", nil)
		return
	}
	raw := token(32)
	a := currentActor(r)
	v, e := s.DB.CreateInvitation(r.Context(), login, "member", digest(raw), time.Now().Add(7*24*time.Hour).UTC().Format(time.RFC3339Nano), a.UserID)
	if e == nil {
		_ = s.DB.AppendDashboardAudit(r.Context(), a.UserID, "access.invite", "invitation", v.ID, r.Header.Get("X-Request-ID"), map[string]any{"github_login": login})
		result := map[string]any{"invitation": v, "claim_token": raw, "invite_url": strings.TrimRight(s.Origin, "/") + "/?invitation=" + url.QueryEscape(raw)}
		_ = s.DB.PutDashboardIdempotency(r.Context(), a.UserID, r.Header.Get("Idempotency-Key"), "access.invite", result)
		s.ok(w, result, nil)
		return
	}
	s.internal(w, r, e)
}
func (s *Server) handleOwnerClaim(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	if !a.Service {
		s.fail(w, r, 403, "forbidden", "owner claims may only be provisioned by the service", nil)
		return
	}
	raw := token(32)
	claimID, e := s.DB.CreateOwnerClaim(r.Context(), digest(raw), time.Now().Add(30*time.Minute).UTC().Format(time.RFC3339Nano))
	if e != nil {
		s.internal(w, r, e)
		return
	}
	result := map[string]any{"id": claimID, "token": raw, "expires_in": 1800}
	_ = s.DB.PutDashboardIdempotency(r.Context(), a.UserID, r.Header.Get("Idempotency-Key"), "access.owner_claim", result)
	s.ok(w, result, nil)
}
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	v, e := s.DB.ListDashboardAudit(r.Context(), queryLimit(r))
	s.respond(w, r, v, e)
}

func (s *Server) handleStartPromotion(w http.ResponseWriter, r *http.Request) {
	s.metrics.promotionStarts.Add(1)
	var body map[string]any
	if !s.decode(w, r, &body, 1<<20) {
		return
	}
	a := currentActor(r)
	operation, e := s.DB.CreateHostingOperation(r.Context(), "railway_promotion", r.Header.Get("Idempotency-Key"), a.UserID, body)
	if e != nil {
		s.internal(w, r, e)
		return
	}
	_, _ = s.DB.AppendHostingOperationEvent(r.Context(), operation.ID, "queued", "pending", "Promotion queued", nil)
	_ = s.DB.AppendDashboardAudit(r.Context(), a.UserID, "hosting.promote", "hosting_operation", operation.ID, r.Header.Get("X-Request-ID"), nil)
	_ = s.DB.PutDashboardIdempotency(r.Context(), a.UserID, r.Header.Get("Idempotency-Key"), "hosting.promote", operation)
	if s.Promotion != nil {
		go func() { _ = s.Promotion(context.Background(), operation) }()
	}
	s.okStatus(w, http.StatusAccepted, operation, nil)
}
func (s *Server) handlePromotion(w http.ResponseWriter, r *http.Request) {
	v, e := s.DB.GetHostingOperation(r.Context(), r.PathValue("id"))
	s.respond(w, r, v, e)
}
func (s *Server) handleResumePromotion(w http.ResponseWriter, r *http.Request) {
	if !s.confirmed(w, r) {
		return
	}
	operation, err := s.DB.GetHostingOperation(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if operation.Status != "failed" {
		s.fail(w, r, http.StatusConflict, "operation_not_resumable", "only failed promotions may be resumed", nil)
		return
	}
	if err = s.DB.BeginHostingOperation(r.Context(), operation.ID, "prerequisites"); err != nil {
		s.fail(w, r, http.StatusConflict, "operation_not_resumable", err.Error(), nil)
		return
	}
	operation.Status = "running"
	operation.Stage = "prerequisites"
	operation.Attempts++
	a := currentActor(r)
	_, _ = s.DB.AppendHostingOperationEvent(r.Context(), operation.ID, operation.Stage, "pending", "Promotion resume requested", nil)
	_ = s.DB.AppendDashboardAudit(r.Context(), a.UserID, "hosting.resume", "hosting_operation", operation.ID, r.Header.Get("X-Request-ID"), nil)
	_ = s.DB.PutDashboardIdempotency(r.Context(), a.UserID, r.Header.Get("Idempotency-Key"), "hosting.resume", operation)
	if s.Promotion != nil {
		go func() { _ = s.Promotion(context.Background(), operation) }()
	}
	s.okStatus(w, http.StatusAccepted, operation, nil)
}
func (s *Server) handlePromotionStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.fail(w, r, 500, "stream_unavailable", "streaming unavailable", nil)
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if n, e := strconv.ParseInt(raw, 10, 64); e == nil && n > after {
			after = n
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	poll := time.NewTicker(500 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
			events, e := s.DB.ListHostingOperationEvents(r.Context(), r.PathValue("id"), after)
			if e != nil {
				return
			}
			for _, event := range events {
				after = event.Seq
				raw, _ := json.Marshal(event)
				fmt.Fprintf(w, "id: %d\ndata: %s\n\n", event.Seq, raw)
			}
			if len(events) > 0 {
				flusher.Flush()
			}
			op, e := s.DB.GetHostingOperation(r.Context(), r.PathValue("id"))
			if e == nil && (op.Status == "completed" || op.Status == "failed") {
				raw, _ := json.Marshal(op)
				fmt.Fprintf(w, "event: result\ndata: %s\n\n", raw)
				flusher.Flush()
				return
			}
		}
	}
}
