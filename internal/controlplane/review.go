package controlplane

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	runengine "github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

const reviewTokenLifetime = 30 * 24 * time.Hour

type hostedRunPrompter interface {
	Prompt(context.Context, *state.Run, string) (*runengine.PromptResult, error)
}

type reviewPageData struct {
	RunID      string
	Token      string
	PRURL      string
	PreviewURL string
	Message    string
	Error      string
	Output     string
	Panel      bool
	Standalone bool
	Open       bool
	CanReview  bool
	CanPrompt  bool
	Refresh    bool
	Terminal   bool
	WindowURL  string
}

func (s *Server) reviewToken(runID string, expires time.Time) string {
	payload := runID + "|" + strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(s.reviewSecret()))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) verifyReviewToken(runID, token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || s.reviewSecret() == "" {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != 2 || fields[0] != runID {
		return false
	}
	expires, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || time.Now().After(time.Unix(expires, 0)) {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.reviewSecret()))
	_, _ = mac.Write(payload)
	return hmac.Equal(signature, mac.Sum(nil))
}

func (s *Server) reviewSecret() string {
	if strings.TrimSpace(s.APIToken) != "" {
		return s.APIToken
	}
	return s.WorkerDownloadToken
}

func (s *Server) reviewURL(runID, action string) string {
	base := strings.TrimRight(s.Config.Hosted.ControlPlaneURL, "/")
	if base == "" || s.reviewSecret() == "" {
		return ""
	}
	token := s.reviewToken(runID, time.Now().Add(reviewTokenLifetime))
	return fmt.Sprintf("%s/review/runs/%s?action=%s&token=%s", base, url.PathEscape(runID), url.QueryEscape(action), url.QueryEscape(token))
}

func (s *Server) previewOverlay(runID string) string {
	return fmt.Sprintf(`<iframe id="ves-review-panel" title="Vessica review" src="/review/runs/%s/panel" sandbox="allow-forms allow-scripts allow-popups allow-popups-to-escape-sandbox" style="position:fixed;right:16px;bottom:16px;width:min(420px,calc(100vw - 32px));height:68px;border:0;background:transparent;z-index:2147483647;color-scheme:light"></iframe>
<script>(function(){var f=document.getElementById('ves-review-panel');window.addEventListener('message',function(e){if(e.source!==f.contentWindow||!e.data||e.data.scope!=='vessica.review')return;if(e.data.type==='resize'){f.style.height=Math.min(Number(e.data.height)||68,Math.max(68,window.innerHeight-32))+'px';}if(e.data.type==='detach'){f.style.display='none';}if(e.data.type==='attach'){f.style.display='block';}if(e.data.type==='reload'){f.remove();window.location.reload();}});})();</script>`, template.HTMLEscapeString(runID))
}

func (s *Server) handleReviewPage(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	panel := strings.HasSuffix(r.URL.Path, "/panel")
	standalone := strings.HasSuffix(r.URL.Path, "/window")
	var token string
	if panel {
		if r.Header.Get("Sec-Fetch-Dest") != "iframe" {
			http.NotFound(w, r)
			return
		}
		cookie, err := r.Cookie(previewCookie)
		resolved, valid := "", false
		if err == nil && s.PreviewBroker != nil {
			resolved, valid = s.PreviewBroker.ResolveCapability(cookie.Value)
		}
		if err != nil || !valid || resolved != runID {
			http.Error(w, "preview review session unavailable", http.StatusUnauthorized)
			return
		}
		token = s.reviewToken(runID, time.Now().Add(reviewTokenLifetime))
	} else {
		token = r.URL.Query().Get("token")
		if !s.verifyReviewToken(runID, token) {
			http.Error(w, "review link is invalid or expired", http.StatusUnauthorized)
			return
		}
	}
	s.renderReviewPage(w, r, reviewPageData{RunID: runID, Token: token, Panel: panel, Standalone: standalone, Open: !panel})
}

func (s *Server) handleReviewEvents(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	if !s.verifyReviewToken(runID, r.URL.Query().Get("token")) {
		http.Error(w, "review capability is invalid or expired", http.StatusUnauthorized)
		return
	}
	if _, err := s.DB.GetRun(r.Context(), runID); err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "null")
	w.Header().Add("Vary", "Origin")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("latest") == "1" {
		seq, err := s.DB.LatestEventSeq(r.Context(), runID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"seq": seq})
		return
	}
	if r.URL.Query().Get("session") == "1" {
		events, err := s.DB.ListEvents(r.Context(), runID, 0)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		latest, _ := s.DB.LatestEventSeq(r.Context(), runID)
		after, prompt, found, terminal := latest, "", false, false
		for i := range events {
			event := events[i]
			if event.Type == "sandbox.prompt.started" {
				after, found, terminal = event.Seq-1, true, false
				var payload map[string]any
				if json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil {
					prompt, _ = payload["prompt"].(string)
				}
			} else if found && (event.Type == "sandbox.prompt.completed" || event.Type == "sandbox.prompt.failed") {
				terminal = true
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"after": after, "prompt": prompt, "found": found, "terminal": terminal})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is unavailable", http.StatusInternalServerError)
		return
	}
	var after int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		_, _ = fmt.Sscan(raw, &after)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	poll := time.NewTicker(300 * time.Millisecond)
	heartbeat := time.NewTicker(12 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-poll.C:
			events, err := s.DB.ListEvents(r.Context(), runID, after)
			if err != nil {
				return
			}
			terminal := false
			for i := range events {
				event := events[i]
				after = event.Seq
				if !reviewStreamEvent(event.Type) {
					continue
				}
				body, _ := json.Marshal(event)
				fmt.Fprintf(w, "id: %d\ndata: %s\n\n", event.Seq, body)
				terminal = terminal || event.Type == "sandbox.prompt.completed" || event.Type == "sandbox.prompt.failed"
			}
			if len(events) > 0 {
				flusher.Flush()
			}
			if terminal {
				return
			}
		}
	}
}

func reviewStreamEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "agent.") || eventType == "sandbox.prompt.started" || eventType == "sandbox.prompt.completed" || eventType == "sandbox.prompt.failed" || eventType == "repo.branch.updated"
}

func (s *Server) handleReviewPrompt(w http.ResponseWriter, r *http.Request) {
	data, runRecord, ok := s.authorizeReviewPost(w, r)
	if !ok {
		return
	}
	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if prompt == "" || len(prompt) > 4000 {
		data.Error = "Enter a refinement request of 4,000 characters or fewer."
		s.respondReview(w, r, data)
		return
	}
	if runRecord.Status != "completed" || runRecord.PRMode == "merged" || runRecord.PRMode == "rolled_back" {
		data.Error = "This run is no longer available for preview refinements."
		s.respondReview(w, r, data)
		return
	}
	prompter, ok := s.Launcher.(hostedRunPrompter)
	if !ok {
		data.Error = "This control plane cannot prompt retained sandboxes."
		s.respondReview(w, r, data)
		return
	}
	reviewRequestID := id.New("review")
	commentBody := fmt.Sprintf("<!-- vessica:review-request:%s -->\n**Revision requested from the Vessica preview**\n\n%s\n\nRun: `%s`", reviewRequestID, prompt, runRecord.ID)
	if runRecord.PRURL != "" {
		commentBody += fmt.Sprintf("\nDraft PR: %s", runRecord.PRURL)
	}
	if err := s.enqueueLinearReviewComment(r.Context(), runRecord, "review_request", reviewRequestID, "linear:review:request:"+reviewRequestID, commentBody); err != nil {
		data.Error = "Could not record the revision request in Linear: " + err.Error()
		s.respondReview(w, r, data)
		return
	}
	result, err := prompter.Prompt(r.Context(), runRecord, prompt)
	if err != nil {
		data.Error = err.Error()
		s.respondReview(w, r, data)
		return
	}
	data.Refresh = result.Pushed
	if result.Pushed {
		data.Message = "Changes were committed, pushed, and applied to the live preview."
	} else {
		data.Message = "Codex completed the request; no file changes were needed."
	}
	data.Output = result.Output
	if runRecord.PRURL != "" {
		if number, err := repo.ParsePRNumber(runRecord.PRURL); err == nil {
			body := fmt.Sprintf("Vessica applied a preview refinement.\n\n**Request**\n%s\n\n**Result**\n%s", prompt, result.Output)
			_ = repo.CommentPullRequest(r.Context(), s.Config.Repo.Remote, number, body)
		}
	}
	s.respondReview(w, r, data)
}

func (s *Server) handleReviewApprove(w http.ResponseWriter, r *http.Request) {
	data, _, ok := s.authorizeReviewPost(w, r)
	if !ok {
		return
	}
	result, err := s.approveRun(r.Context(), data.RunID)
	if err != nil {
		data.Error = err.Error()
	} else {
		data.Message = fmt.Sprintf("Pull request merged successfully at %s.", result["merge_commit_sha"])
		data.Terminal = true
	}
	s.respondReview(w, r, data)
}

func (s *Server) handleReviewRollback(w http.ResponseWriter, r *http.Request) {
	data, _, ok := s.authorizeReviewPost(w, r)
	if !ok {
		return
	}
	if err := s.rollbackRun(r.Context(), data.RunID); err != nil {
		data.Error = err.Error()
	} else {
		data.Message = "The draft PR was marked rolled back and closed. The preview sandbox has been stopped."
		data.Terminal = true
	}
	s.respondReview(w, r, data)
}

func (s *Server) authorizeReviewPost(w http.ResponseWriter, r *http.Request) (reviewPageData, *state.Run, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid review request", http.StatusBadRequest)
		return reviewPageData{}, nil, false
	}
	runID := r.PathValue("run_id")
	token := r.FormValue("token")
	if !s.verifyReviewToken(runID, token) {
		http.Error(w, "review capability is invalid or expired", http.StatusUnauthorized)
		return reviewPageData{}, nil, false
	}
	runRecord, err := s.DB.GetRun(r.Context(), runID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return reviewPageData{}, nil, false
	}
	panel := r.FormValue("panel") == "1"
	standalone := r.FormValue("standalone") == "1"
	return reviewPageData{RunID: runID, Token: token, Panel: panel, Standalone: standalone, Open: true}, runRecord, true
}

func (s *Server) renderReviewPage(w http.ResponseWriter, r *http.Request, data reviewPageData) {
	runRecord, err := s.DB.GetRun(r.Context(), data.RunID)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	data.PRURL, data.PreviewURL = runRecord.PRURL, runRecord.PreviewURL
	data.WindowURL = fmt.Sprintf("/review/runs/%s/window?token=%s", url.PathEscape(data.RunID), url.QueryEscape(data.Token))
	data.CanReview = runRecord.Status == "completed" && runRecord.PRURL != "" && runRecord.PRMode != "merged" && runRecord.PRMode != "rolled_back"
	data.CanPrompt = data.CanReview
	if sandboxRecord, err := s.DB.GetSandboxForRun(r.Context(), data.RunID); err != nil || sandboxRecord.Status == "destroyed" || sandboxRecord.Status == "expired" {
		data.CanPrompt = false
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	connectSource := strings.TrimRight(s.Config.Hosted.ControlPlaneURL, "/")
	if connectSource == "" {
		connectSource = "'none'"
	}
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox; default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src "+connectSource+"; frame-ancestors 'self'; base-uri 'none'")
	if err := reviewPageTemplate.Execute(w, data); err != nil && s.Logger != nil {
		s.Logger.Printf("render review page: %v", err)
	}
}

func (s *Server) respondReview(w http.ResponseWriter, r *http.Request, data reviewPageData) {
	if r.FormValue("async") == "1" {
		w.Header().Set("Access-Control-Allow-Origin", "null")
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": data.Error == "", "message": data.Message, "error": data.Error,
			"output": data.Output, "refresh": data.Refresh, "terminal": data.Terminal,
		})
		return
	}
	s.renderReviewPage(w, r, data)
}
