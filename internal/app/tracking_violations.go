package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxTrackingViolations bounds the recorder. A blocked request repeats on every
// page view, so the useful information is which origins are blocked, not how
// often — a small buffer of distinct origins is enough to fix a snippet.
const maxTrackingViolations = 100

// maxCSPReportBytes keeps an unauthenticated endpoint from being used to push
// large bodies at the server.
const maxCSPReportBytes = 8 * 1024

// TrackingViolation is one origin the content security policy refused, kept
// with the directive that refused it so the settings screen can say what to
// allow.
type TrackingViolation struct {
	Origin    string    `json:"origin"`
	Directive string    `json:"directive"`
	Page      string    `json:"page"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Allowed   bool      `json:"allowed"`
}

// violationRecorder collects policy violations reported by browsers. It is
// deliberately in memory: the reports are a live troubleshooting aid for the
// person pasting a snippet, not an audit record, and keeping them out of the
// database means the browser can report freely without growing storage.
type violationRecorder struct {
	mu         sync.Mutex
	violations map[string]*TrackingViolation
	now        func() time.Time
}

func newViolationRecorder() *violationRecorder {
	return &violationRecorder{violations: map[string]*TrackingViolation{}, now: time.Now}
}

// record notes one blocked request. Anything that is not an http origin, such
// as a browser extension or a data: URL, is ignored because allowing it is
// neither possible nor useful.
func (r *violationRecorder) record(blockedURI, directive, page string) {
	origin := originOf(blockedURI)
	if origin == "" {
		return
	}
	directive = strings.TrimSpace(strings.ToLower(directive))
	if index := strings.IndexByte(directive, ' '); index > 0 {
		directive = directive[:index]
	}
	if directive == "" {
		directive = "connect-src"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := directive + " " + origin
	if existing, found := r.violations[key]; found {
		existing.Count++
		existing.LastSeen = r.now()
		existing.Page = page
		return
	}
	if len(r.violations) >= maxTrackingViolations {
		r.evictOldest()
	}
	moment := r.now()
	r.violations[key] = &TrackingViolation{Origin: origin, Directive: directive, Page: page, Count: 1, FirstSeen: moment, LastSeen: moment}
}

func (r *violationRecorder) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, violation := range r.violations {
		if oldestKey == "" || violation.LastSeen.Before(oldest) {
			oldestKey, oldest = key, violation.LastSeen
		}
	}
	delete(r.violations, oldestKey)
}

// list returns the blocked origins, most recent first, marking the ones the
// configuration already allows so a fixed snippet stops nagging.
func (r *violationRecorder) list(config trackingConfig) []TrackingViolation {
	allowed := map[string]struct{}{}
	scripts, connects, images := config.policySources()
	for _, group := range [][]string{scripts, connects, images} {
		for _, origin := range group {
			allowed[strings.ToLower(strings.TrimSuffix(origin, "/"))] = struct{}{}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]TrackingViolation, 0, len(r.violations))
	for _, violation := range r.violations {
		copied := *violation
		_, known := allowed[copied.Origin]
		copied.Allowed = known || matchesWildcardOrigin(copied.Origin, allowed)
		items = append(items, copied)
	}
	sort.Slice(items, func(first, second int) bool {
		if items[first].LastSeen.Equal(items[second].LastSeen) {
			return items[first].Origin < items[second].Origin
		}
		return items[first].LastSeen.After(items[second].LastSeen)
	})
	return items
}

// forget drops the recorded violations, which is what an administrator does
// after fixing a snippet to check whether anything is still blocked.
func (r *violationRecorder) forget() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.violations = map[string]*TrackingViolation{}
}

// matchesWildcardOrigin covers policy entries such as https://*.google-analytics.com.
func matchesWildcardOrigin(origin string, allowed map[string]struct{}) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	for pattern := range allowed {
		star := strings.Index(pattern, "*.")
		if star < 0 {
			continue
		}
		if strings.HasPrefix(origin, pattern[:star]) && strings.HasSuffix(parsed.Host, pattern[star+1:]) {
			return true
		}
	}
	return false
}

// addAllowedHost appends an origin to the allow list, leaving the existing
// entries and their order alone.
func addAllowedHost(existing, origin string) string {
	origin = strings.TrimSpace(strings.TrimSuffix(origin, "/"))
	if origin == "" {
		return existing
	}
	for _, host := range allowedHostList(existing) {
		if strings.EqualFold(host, origin) {
			return existing
		}
	}
	if strings.TrimSpace(existing) == "" {
		return origin
	}
	return strings.TrimSpace(existing) + ", " + origin
}

type cspReport struct {
	Report struct {
		BlockedURI         string `json:"blocked-uri"`
		ViolatedDirective  string `json:"violated-directive"`
		EffectiveDirective string `json:"effective-directive"`
		DocumentURI        string `json:"document-uri"`
	} `json:"csp-report"`
}

// receiveCSPReport records what a browser refused to load. Reports are always
// answered with 204: the browser sends them without credentials and nothing a
// page could do with an error is useful.
func (s *Server) receiveCSPReport(w http.ResponseWriter, r *http.Request) {
	defer w.WriteHeader(http.StatusNoContent)
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCSPReportBytes))
	if err != nil || len(body) == 0 {
		return
	}
	var report cspReport
	if json.Unmarshal(body, &report) != nil {
		return
	}
	directive := report.Report.EffectiveDirective
	if directive == "" {
		directive = report.Report.ViolatedDirective
	}
	s.violations.record(report.Report.BlockedURI, directive, report.Report.DocumentURI)
}

// listTrackingViolations shows the administrator which addresses the policy is
// blocking, so a snippet can be fixed without reading the browser console.
func (s *Server) listTrackingViolations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.violations.list(s.trackingConfig(r.Context()))})
}

// clearTrackingViolations forgets the recorded reports, which is how an
// administrator checks whether a change actually fixed the snippet.
func (s *Server) clearTrackingViolations(w http.ResponseWriter, _ *http.Request) {
	s.violations.forget()
	w.WriteHeader(http.StatusNoContent)
}

// allowTrackingHost adds one blocked origin to the allow list: the one-click
// fix for the reports listed above.
func (s *Server) allowTrackingHost(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Origin string `json:"origin"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	origin := originOf(in.Origin)
	if origin == "" {
		writeError(w, http.StatusBadRequest, "invalid_origin", "허용할 주소는 http(s) 출처여야 합니다")
		return
	}
	current, _ := s.getSetting(r.Context(), "tracking.allowed_hosts")
	updated := addAllowedHost(current, origin)
	u, _ := userFrom(r)
	if _, err := s.db.Exec(r.Context(), `UPDATE settings SET value=$2,updated_at=now(),updated_by=$3 WHERE key=$1`, "tracking.allowed_hosts", updated, u.ID); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.invalidateSettings()
	s.audit(r.Context(), u.ID, "settings.update", "settings", "", clientIP(r), map[string]any{"changes": map[string]any{"tracking.allowed_hosts": map[string]string{"before": current, "after": updated}}})
	writeJSON(w, http.StatusOK, map[string]any{"allowedHosts": updated})
}

// momentoProxy forwards /momento/* to the configured Momento collector so the
// tracker and its beacons stay on this origin and the policy never names an
// external host. It answers 404 unless Momento with the proxy is switched on,
// so a fresh installation exposes nothing here.
func (s *Server) momentoProxy(w http.ResponseWriter, r *http.Request) {
	config := s.trackingConfig(r.Context())
	if !config.Enabled || config.Provider != trackingProviderMomento || !config.MomentoProxy {
		http.NotFound(w, r)
		return
	}
	target, err := url.Parse(strings.TrimRight(config.MomentoURL, "/"))
	if err != nil || target.Host == "" {
		http.NotFound(w, r)
		return
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.SetXForwarded()
			// The visitor's session with this app is none of the collector's
			// business.
			request.Out.Header.Del("Cookie")
			request.Out.Header.Del("Authorization")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.logger.Warn("momento proxy failed", "error", err, "path", r.URL.Path)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	forwarded := r.Clone(r.Context())
	forwarded.URL.Path = strings.TrimPrefix(r.URL.Path, momentoProxyPrefix)
	forwarded.URL.RawPath = ""
	if forwarded.URL.Path == "" {
		forwarded.URL.Path = "/"
	}
	proxy.ServeHTTP(w, forwarded)
}
