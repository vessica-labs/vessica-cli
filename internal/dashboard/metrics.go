package dashboard

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type metrics struct{ requests, errors, durationNanos, sseActive, sseReconnects, promotionStarts, previewFailures atomic.Int64 }

func (m *metrics) serve(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# TYPE vessica_dashboard_requests_total counter\nvessica_dashboard_requests_total %d\n# TYPE vessica_dashboard_errors_total counter\nvessica_dashboard_errors_total %d\n# TYPE vessica_dashboard_request_duration_seconds_sum counter\nvessica_dashboard_request_duration_seconds_sum %.6f\n# TYPE vessica_dashboard_sse_active gauge\nvessica_dashboard_sse_active %d\n# TYPE vessica_dashboard_sse_reconnects_total counter\nvessica_dashboard_sse_reconnects_total %d\n# TYPE vessica_dashboard_promotion_starts_total counter\nvessica_dashboard_promotion_starts_total %d\n# TYPE vessica_dashboard_preview_failures_total counter\nvessica_dashboard_preview_failures_total %d\n", m.requests.Load(), m.errors.Load(), float64(m.durationNanos.Load())/float64(time.Second), m.sseActive.Load(), m.sseReconnects.Load(), m.promotionStarts.Load(), m.previewFailures.Load())
}
