package controlplane

import (
	"net/http"
	"strings"
)

func dashboardRoute(r *http.Request) bool {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/v1/sandboxes") && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return false
	}
	if strings.HasPrefix(p, "/auth/") || strings.HasPrefix(p, "/assets/") || p == "/internal/dashboard/metrics" {
		return true
	}
	for _, prefix := range []string{"/api/v1/system", "/api/v1/integrations", "/api/v1/sandboxes", "/api/v1/knowledge", "/api/v1/access", "/api/v1/audit", "/api/v1/hosting", "/api/v1/docs", "/api/v1/conversations", "/api/v1/briefings", "/api/v1/operator"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	if strings.HasPrefix(p, "/api/v1/runs") {
		return !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("Accept") == "application/vnd.vessica.dashboard+json"
	}
	for _, prefix := range []string{"/api/v1/agents", "/api/v1/agent-runs", "/api/v1/agent-builds", "/api/v1/agent-tools"} {
		if strings.HasPrefix(p, prefix) {
			return !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
	}
	if strings.HasPrefix(p, "/api/v1/repositories") || strings.HasPrefix(p, "/api/v1/onboarding") {
		return !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	for _, prefix := range []string{"/healthz", "/readyz", "/webhooks/", "/internal/", "/review/", "/previews/", "/api/v1/status", "/api/v1/jobs", "/api/v1/receipts", "/api/v1/epics"} {
		if strings.HasPrefix(p, prefix) {
			return false
		}
	}
	return r.Method == http.MethodGet
}
