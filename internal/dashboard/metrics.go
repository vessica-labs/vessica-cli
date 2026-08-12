package dashboard

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

type metrics struct{ requests, errors, durationNanos, sseActive, sseReconnects, promotionStarts, previewFailures atomic.Int64 }

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.App.OperatorSnapshot(r.Context(), time.Now())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.metrics.serve(w, snapshot)
}

func (m *metrics) serve(w http.ResponseWriter, snapshot state.OperatorSnapshot) {
	var budgetLimit, budgetSpent int64
	for _, budget := range snapshot.Budgets {
		budgetLimit += budget.LimitMicroUSD
		budgetSpent += budget.SpentMicroUSD
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# TYPE vessica_dashboard_requests_total counter\nvessica_dashboard_requests_total %d\n# TYPE vessica_dashboard_errors_total counter\nvessica_dashboard_errors_total %d\n# TYPE vessica_dashboard_request_duration_seconds_sum counter\nvessica_dashboard_request_duration_seconds_sum %.6f\n# TYPE vessica_dashboard_sse_active gauge\nvessica_dashboard_sse_active %d\n# TYPE vessica_dashboard_sse_reconnects_total counter\nvessica_dashboard_sse_reconnects_total %d\n# TYPE vessica_dashboard_promotion_starts_total counter\nvessica_dashboard_promotion_starts_total %d\n# TYPE vessica_dashboard_preview_failures_total counter\nvessica_dashboard_preview_failures_total %d\n# TYPE vessica_oauth_failures_total counter\nvessica_oauth_failures_total %d\n# TYPE vessica_mcp_errors_total counter\nvessica_mcp_errors_total %d\n# TYPE vessica_mcp_latency_milliseconds_sum counter\nvessica_mcp_latency_milliseconds_sum %d\n# TYPE vessica_stale_source_checkpoints gauge\nvessica_stale_source_checkpoints %d\n# TYPE vessica_stale_ingestion_batches gauge\nvessica_stale_ingestion_batches %d\n# TYPE vessica_ingestion_rejected_records_total counter\nvessica_ingestion_rejected_records_total %d\n# TYPE vessica_failed_agents gauge\nvessica_failed_agents %d\n# TYPE vessica_missing_briefings gauge\nvessica_missing_briefings %d\n# TYPE vessica_agent_budget_limit_microusd gauge\nvessica_agent_budget_limit_microusd %d\n# TYPE vessica_agent_budget_spent_microusd gauge\nvessica_agent_budget_spent_microusd %d\n# TYPE vessica_denied_actions_total counter\nvessica_denied_actions_total %d\n", m.requests.Load(), m.errors.Load(), float64(m.durationNanos.Load())/float64(time.Second), m.sseActive.Load(), m.sseReconnects.Load(), m.promotionStarts.Load(), m.previewFailures.Load(), snapshot.OAuthFailures, snapshot.MCPErrors, snapshot.MCPLatencyMS, len(snapshot.StaleCheckpoints), snapshot.StaleIngestionBatches, snapshot.RejectedRecords, len(snapshot.FailedAgents), len(snapshot.MissingBriefings), budgetLimit, budgetSpent, snapshot.DeniedActions)
}
