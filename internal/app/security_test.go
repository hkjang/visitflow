package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestRateLimiterAllowsUpToLimitPerWindow(t *testing.T) {
	limiter := newRateLimiter(time.Minute)
	now := time.Now()
	for attempt := 1; attempt <= 3; attempt++ {
		if allowed, _ := limiter.allow("pass|10.0.0.1", 3, now); !allowed {
			t.Fatalf("request %d was rejected inside the limit", attempt)
		}
	}
	allowed, retryAfter := limiter.allow("pass|10.0.0.1", 3, now)
	if allowed {
		t.Fatal("the fourth request exceeded the limit but was allowed")
	}
	if retryAfter <= 0 {
		t.Fatal("a rejected request must report when the window reopens")
	}
	if allowed, _ := limiter.allow("pass|10.0.0.2", 3, now); !allowed {
		t.Fatal("a different address was throttled by another address's counter")
	}
	if allowed, _ := limiter.allow("pass|10.0.0.1", 3, now.Add(2*time.Minute)); !allowed {
		t.Fatal("the counter did not reset in the next window")
	}
}

func TestRateLimiterCleanupDropsExpiredWindows(t *testing.T) {
	limiter := newRateLimiter(time.Minute)
	now := time.Now()
	limiter.allow("pass|10.0.0.1", 1, now)
	limiter.cleanup(now.Add(2 * time.Minute))
	if len(limiter.entries) != 0 {
		t.Fatalf("cleanup left %d expired entries", len(limiter.entries))
	}
}

func TestLoginThrottleKeysCoverAddressAndAccount(t *testing.T) {
	keys := loginThrottleKeys("10.0.0.1", "Admin")
	if len(keys) != 2 || keys[0] != "ip:10.0.0.1" || keys[1] != "user:admin" {
		t.Fatalf("unexpected throttle keys %v", keys)
	}
	if anonymous := loginThrottleKeys("10.0.0.1", ""); len(anonymous) != 1 {
		t.Fatalf("a missing username must still throttle the address: %v", anonymous)
	}
}

func TestClientIPStripsPort(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.168.10.4:53112"
	if got := clientIP(request); got != "192.168.10.4" {
		t.Fatalf("clientIP() = %q", got)
	}
	request.RemoteAddr = "192.168.10.4"
	if got := clientIP(request); got != "192.168.10.4" {
		t.Fatalf("clientIP() without a port = %q", got)
	}
}

func TestNormalizeLocale(t *testing.T) {
	for input, want := range map[string]string{
		"ko": "ko", "ko-KR": "ko", "EN_us": "en", "ja": "ja", "zh-Hans": "zh",
		"": "", "fr": "", "klingon": "",
	} {
		if got := normalizeLocale(input); got != want {
			t.Fatalf("normalizeLocale(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBestAcceptLanguagePrefersHighestQuality(t *testing.T) {
	allowed := map[string]bool{"ko": true, "en": true}
	if got := bestAcceptLanguage("fr;q=1.0, en;q=0.4, ko;q=0.8", allowed); got != "ko" {
		t.Fatalf("bestAcceptLanguage() = %q, want ko", got)
	}
	if got := bestAcceptLanguage("en-GB,en;q=0.9", allowed); got != "en" {
		t.Fatalf("bestAcceptLanguage() = %q, want en", got)
	}
	if got := bestAcceptLanguage("fr,de", allowed); got != "" {
		t.Fatalf("unsupported languages returned %q", got)
	}
}

func TestVisitCursorRoundTrip(t *testing.T) {
	item := VisitSummary{ID: "1f3a", StartAt: time.Date(2026, 9, 2, 10, 30, 0, 0, time.UTC)}
	timestamp, id, ok := decodeVisitCursor(encodeVisitCursor(item))
	if !ok {
		t.Fatal("a freshly encoded cursor did not decode")
	}
	if id != item.ID || !timestamp.Equal(item.StartAt) {
		t.Fatalf("cursor round trip changed the position: %s %s", timestamp, id)
	}
	for _, invalid := range []string{"", "not-base64!", "YWJj", "MjAyNi0wOS0wMg"} {
		if _, _, ok := decodeVisitCursor(invalid); ok {
			t.Fatalf("invalid cursor %q was accepted", invalid)
		}
	}
}

func TestContentSecurityPolicyNoncesStyleElements(t *testing.T) {
	policy := contentSecurityPolicy("abc123")
	if !strings.Contains(policy, "style-src-elem 'self' 'nonce-abc123'") {
		t.Fatalf("style elements are not nonced: %s", policy)
	}
	if !strings.Contains(policy, "style-src-attr 'unsafe-inline'") {
		t.Fatalf("style attributes must stay allowed for MUI: %s", policy)
	}
	if strings.Contains(policy, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("scripts must not allow inline sources: %s", policy)
	}
}

func TestInjectCSPNoncePublishesMetaTag(t *testing.T) {
	document, ok := injectCSPNonce("<html><head><title>VisitFlow</title></head><body></body></html>", "abc123")
	if !ok {
		t.Fatal("nonce injection reported failure on a normal document")
	}
	if !strings.Contains(document, `<meta property="csp-nonce" content="abc123">`) {
		t.Fatalf("meta tag missing: %s", document)
	}
	if _, ok := injectCSPNonce("<html><body></body></html>", "abc123"); ok {
		t.Fatal("a document without <head> must report that it could not be nonced")
	}
	if _, ok := injectCSPNonce("<html><head></head></html>", ""); ok {
		t.Fatal("an empty nonce must not be injected")
	}
}

func TestNotificationRecipientPerAudience(t *testing.T) {
	data := notificationEventData{VisitorPhone: "010-1111-2222", HostPhone: "010-3333-4444", VisitorVisitID: "vv-1"}
	if got := notificationRecipient("visitor", "sms", data); got != "01011112222" {
		t.Fatalf("visitor recipient = %q", got)
	}
	if got := notificationRecipient("host", "sms", data); got != "01033334444" {
		t.Fatalf("host recipient = %q", got)
	}
	if got := notificationRecipient("system", "webhook", data); got != "vv-1" {
		t.Fatalf("system recipient = %q, want the participant id", got)
	}
	data.VisitorEmail, data.HostEmail = "guest@partner.example", "host@company.intra"
	if got := notificationRecipient("visitor", "email", data); got != "guest@partner.example" {
		t.Fatalf("visitor e-mail recipient = %q", got)
	}
	if got := notificationRecipient("host", "email", data); got != "host@company.intra" {
		t.Fatalf("host e-mail recipient = %q", got)
	}
}

func TestConsentSourceMapsVisitOrigin(t *testing.T) {
	for input, want := range map[string]string{
		"employee": "host", "": "host", "lobby": "lobby",
		"import": "import", "api": "api", "mcp": "mcp", "self": "self",
	} {
		if got := consentSource(input); got != want {
			t.Fatalf("consentSource(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHasActiveDelegate(t *testing.T) {
	now := time.Now()
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	delegate := "user-2"
	if (User{}).HasActiveDelegate(now) {
		t.Fatal("a user without a delegate reported one")
	}
	if (User{DelegateUserID: &delegate, DelegateUntil: &past}).HasActiveDelegate(now) {
		t.Fatal("an expired delegation is still active")
	}
	if !(User{DelegateUserID: &delegate, DelegateUntil: &future}).HasActiveDelegate(now) {
		t.Fatal("a current delegation was not recognised")
	}
}

func TestToSnakeCase(t *testing.T) {
	for input, want := range map[string]string{
		"requests": "requests", "responses2xx": "responses2xx",
		"loginFailures": "login_failures", "notificationsSent": "notifications_sent",
	} {
		if got := toSnakeCase(input); got != want {
			t.Fatalf("toSnakeCase(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHashedAssetsAreImmutableAndCompressed(t *testing.T) {
	server := NewServer(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{
		"index.html":           {Data: []byte("<html><head></head><body>spa</body></html>")},
		"assets/app-abc123.js": {Data: []byte(strings.Repeat("console.log('visitflow');", 200))},
		"sw.js":                {Data: []byte("self.addEventListener('fetch', () => {});")},
	}, "test", "test", "test")
	handler := server.Routes()

	request := httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("asset returned %d", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("hashed asset cache header = %q", got)
	}
	if got := response.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("asset was not compressed: %q", got)
	}

	worker := httptest.NewRecorder()
	handler.ServeHTTP(worker, httptest.NewRequest(http.MethodGet, "/sw.js", nil))
	if got := worker.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("service worker must revalidate: %q", got)
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/visits", nil))
	if got := page.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("the SPA document carries a per-request nonce and must not be cached: %q", got)
	}
}

// A call to a path that does not exist must come back in the error envelope the
// client parses. A bare chi 404 reaches the operator as "요청 실패 (404)" with no
// hint of which path was wrong, which is how a misspelled admin endpoint stayed
// unexplained.
func TestUnknownAPIPathsAnswerInTheErrorEnvelope(t *testing.T) {
	server := NewServer(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{
		"index.html": {Data: []byte("<html><head></head><body>spa</body></html>")},
	}, "test", "test", "test")
	handler := server.Routes()

	for _, target := range []struct {
		method, path string
	}{
		{http.MethodPost, "/api/v1/admin/lobbys"},
		{http.MethodGet, "/api/v1/does-not-exist"},
		{http.MethodPost, "/mcp/extra"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(target.method, target.path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s %s returned %d, want 404", target.method, target.path, response.Code)
		}
		if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Fatalf("%s %s answered %q, want JSON", target.method, target.path, got)
		}
		if body := response.Body.String(); !strings.Contains(body, "endpoint_not_found") || !strings.Contains(body, target.path) {
			t.Fatalf("%s %s body does not name the path: %s", target.method, target.path, body)
		}
	}

	// A known path called with the wrong verb is a 405, also in the envelope.
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/api/v1/version", nil))
	if response.Code != http.StatusMethodNotAllowed || !strings.Contains(response.Body.String(), "method_not_allowed") {
		t.Fatalf("wrong verb returned %d: %s", response.Code, response.Body.String())
	}

	// Application routes still fall through to the single-page app.
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/admin/resources", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "spa") {
		t.Fatalf("SPA route returned %d: %s", page.Code, page.Body.String())
	}
}
