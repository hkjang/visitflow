package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if got := notificationRecipient("visitor", data); got != "01011112222" {
		t.Fatalf("visitor recipient = %q", got)
	}
	if got := notificationRecipient("host", data); got != "01033334444" {
		t.Fatalf("host recipient = %q", got)
	}
	if got := notificationRecipient("system", data); got != "vv-1" {
		t.Fatalf("system recipient = %q, want the participant id", got)
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
