package app

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// serverMetrics holds the process-local counters. Gauges that describe stored
// state are read from PostgreSQL when the endpoint is scraped, so they stay
// correct with more than one node in front of the same database.
type serverMetrics struct {
	requests            atomic.Int64
	responses2xx        atomic.Int64
	responses4xx        atomic.Int64
	responses5xx        atomic.Int64
	loginFailures       atomic.Int64
	loginLockouts       atomic.Int64
	rateLimited         atomic.Int64
	qrVerified          atomic.Int64
	qrRejected          atomic.Int64
	checkIns            atomic.Int64
	checkOuts           atomic.Int64
	notificationsSent   atomic.Int64
	notificationsFailed atomic.Int64
	startedAt           time.Time
}

func newServerMetrics() *serverMetrics {
	return &serverMetrics{startedAt: time.Now()}
}

func (m *serverMetrics) observeStatus(status int) {
	m.requests.Add(1)
	switch {
	case status >= 500:
		m.responses5xx.Add(1)
	case status >= 400:
		m.responses4xx.Add(1)
	case status >= 200 && status < 300:
		m.responses2xx.Add(1)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

type metricsSnapshot struct {
	VisitsToday        int              `json:"visitsToday"`
	VisitorsCurrent    int              `json:"visitorsCurrent"`
	PendingApproval    int              `json:"pendingApproval"`
	ActiveSessions     int              `json:"activeSessions"`
	ActiveAPIKeys      int              `json:"activeApiKeys"`
	LockedAccounts     int              `json:"lockedAccounts"`
	Notifications      map[string]int   `json:"notifications"`
	QueueBacklog       int              `json:"queueBacklog"`
	QueueOldestSeconds int              `json:"queueOldestSeconds"`
	SchemaVersion      int              `json:"schemaVersion"`
	UptimeSeconds      int              `json:"uptimeSeconds"`
	Counters           map[string]int64 `json:"counters"`
}

func (s *Server) metricsSnapshot(ctx context.Context) (metricsSnapshot, error) {
	snapshot := metricsSnapshot{Notifications: map[string]int{}}
	err := s.db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM visitor_visits vv JOIN visits v ON v.id=vv.visit_id JOIN sites si ON si.id=v.site_id
			WHERE (v.start_at AT TIME ZONE si.timezone)::date=(now() AT TIME ZONE si.timezone)::date AND vv.status NOT IN ('CANCELLED','REJECTED')),
		(SELECT count(*) FROM visitor_visits WHERE status='CHECKED_IN'),
		(SELECT count(*) FROM visits WHERE status='PENDING_APPROVAL'),
		(SELECT count(*) FROM sessions WHERE expires_at>now()),
		(SELECT count(*) FROM api_keys WHERE (expires_at IS NULL OR expires_at>now()) AND revoked_at IS NULL),
		(SELECT count(*) FROM auth_throttle WHERE locked_until>now()),
		(SELECT count(*) FROM notifications WHERE status IN ('queued','failed') AND next_attempt_at<=now()),
		(SELECT COALESCE(EXTRACT(EPOCH FROM now()-min(next_attempt_at))::int,0) FROM notifications WHERE status IN ('queued','failed') AND next_attempt_at<=now()),
		(SELECT COALESCE(max(version),0) FROM schema_migrations)`).
		Scan(&snapshot.VisitsToday, &snapshot.VisitorsCurrent, &snapshot.PendingApproval, &snapshot.ActiveSessions,
			&snapshot.ActiveAPIKeys, &snapshot.LockedAccounts, &snapshot.QueueBacklog, &snapshot.QueueOldestSeconds, &snapshot.SchemaVersion)
	if err != nil {
		return snapshot, err
	}
	rows, err := s.db.Query(ctx, `SELECT status,count(*) FROM notifications GROUP BY status`)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if rows.Scan(&status, &count) == nil {
			snapshot.Notifications[status] = count
		}
	}
	snapshot.UptimeSeconds = int(time.Since(s.metrics.startedAt).Seconds())
	snapshot.Counters = map[string]int64{
		"requests":            s.metrics.requests.Load(),
		"responses2xx":        s.metrics.responses2xx.Load(),
		"responses4xx":        s.metrics.responses4xx.Load(),
		"responses5xx":        s.metrics.responses5xx.Load(),
		"loginFailures":       s.metrics.loginFailures.Load(),
		"loginLockouts":       s.metrics.loginLockouts.Load(),
		"rateLimited":         s.metrics.rateLimited.Load(),
		"qrVerified":          s.metrics.qrVerified.Load(),
		"qrRejected":          s.metrics.qrRejected.Load(),
		"checkIns":            s.metrics.checkIns.Load(),
		"checkOuts":           s.metrics.checkOuts.Load(),
		"notificationsSent":   s.metrics.notificationsSent.Load(),
		"notificationsFailed": s.metrics.notificationsFailed.Load(),
	}
	return snapshot, rows.Err()
}

func (s *Server) adminMetrics(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.metricsSnapshot(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

// prometheusMetrics is the scrape endpoint. It stays disabled until an operator
// configures security.metrics_token, so an offline deployment never exposes
// operational counts to the whole intranet by default.
func (s *Server) prometheusMetrics(w http.ResponseWriter, r *http.Request) {
	token, _ := s.getSetting(r.Context(), "security.metrics_token")
	if strings.TrimSpace(token) == "" {
		http.NotFound(w, r)
		return
	}
	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if subtle.ConstantTimeCompare([]byte(provided), []byte(strings.TrimSpace(token))) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="visitflow"`)
		writeError(w, http.StatusUnauthorized, "metrics_token_required", "metrics 토큰이 필요합니다")
		return
	}
	snapshot, err := s.metricsSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "metrics_unavailable", "지표를 수집하지 못했습니다")
		return
	}
	var b strings.Builder
	gauge := func(name, help string, value int) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, value)
	}
	counter := func(name, help string, value int64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, value)
	}
	gauge("visitflow_visits_today", "Visitor participations scheduled for today", snapshot.VisitsToday)
	gauge("visitflow_visitors_current", "Visitors currently checked in", snapshot.VisitorsCurrent)
	gauge("visitflow_visits_pending_approval", "Visits waiting for approval", snapshot.PendingApproval)
	gauge("visitflow_sessions_active", "Active browser sessions", snapshot.ActiveSessions)
	gauge("visitflow_api_keys_active", "Active personal API keys", snapshot.ActiveAPIKeys)
	gauge("visitflow_accounts_locked", "Accounts or addresses currently locked out", snapshot.LockedAccounts)
	gauge("visitflow_notification_queue_backlog", "Notifications due for delivery", snapshot.QueueBacklog)
	gauge("visitflow_notification_queue_oldest_seconds", "Age of the oldest due notification", snapshot.QueueOldestSeconds)
	gauge("visitflow_schema_version", "Applied database schema version", snapshot.SchemaVersion)
	gauge("visitflow_uptime_seconds", "Process uptime", snapshot.UptimeSeconds)
	b.WriteString("# HELP visitflow_notifications Notifications by status\n# TYPE visitflow_notifications gauge\n")
	statuses := make([]string, 0, len(snapshot.Notifications))
	for status := range snapshot.Notifications {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	for _, status := range statuses {
		fmt.Fprintf(&b, "visitflow_notifications{status=%q} %d\n", status, snapshot.Notifications[status])
	}
	names := make([]string, 0, len(snapshot.Counters))
	for name := range snapshot.Counters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		counter("visitflow_"+toSnakeCase(name)+"_total", "VisitFlow "+name+" counter", snapshot.Counters[name])
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}

func toSnakeCase(value string) string {
	var b strings.Builder
	for index, char := range value {
		if char >= 'A' && char <= 'Z' {
			if index > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(char + 32)
			continue
		}
		b.WriteRune(char)
	}
	return b.String()
}
