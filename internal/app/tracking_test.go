package app

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func trackingFrom(values map[string]string) trackingConfig {
	return trackingConfigFrom(func(key string) string { return values[key] })
}

func TestTrackingDefaultsAreOff(t *testing.T) {
	config := trackingFrom(map[string]string{})
	if config.Enabled || config.Provider != trackingProviderNone || config.Placement != "head" || !config.MomentoProxy {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if config.active("/") || config.snippet("n") != "" {
		t.Fatal("a fresh installation must not carry a snippet")
	}
	if got := trackingContentSecurityPolicy(config, "/", "n0nce"); got != contentSecurityPolicy("n0nce") {
		t.Fatalf("policy widened while tracking is off:\n%s", got)
	}
}

func TestTrackingMomentoProxyKeepsPolicyOnOrigin(t *testing.T) {
	config := trackingFrom(map[string]string{"tracking.enabled": "true", "tracking.provider": "momento",
		"tracking.momento_url": "https://momento.corp.example/", "tracking.momento_site_id": "vf-01"})
	snippet := config.snippet("n0nce")
	for _, want := range []string{`src="/momento/tracker.js"`, `data-site-id="vf-01"`, `data-environment="prd"`, `data-contract-version="1"`, `data-endpoint="/momento"`, `nonce="n0nce"`} {
		if !strings.Contains(snippet, want) {
			t.Fatalf("momento snippet lacks %s:\n%s", want, snippet)
		}
	}
	scripts, connects, images := config.policySources()
	if len(scripts)+len(connects)+len(images) != 0 {
		t.Fatalf("the proxied collector must not appear in the policy: %v %v %v", scripts, connects, images)
	}
	policy := trackingContentSecurityPolicy(config, "/", "n0nce")
	if !strings.Contains(policy, "script-src 'self' 'nonce-n0nce'") || strings.Contains(policy, "momento.corp.example") || !strings.HasSuffix(policy, "; report-uri "+cspReportPath) {
		t.Fatalf("unexpected policy: %s", policy)
	}
	if strings.Contains(strings.SplitN(policy, "script-src", 2)[1], "'unsafe-inline'") {
		t.Fatalf("scripts were opened with 'unsafe-inline': %s", policy)
	}

	config.MomentoProxy = false
	config.MomentoEnvironment = "stg"
	direct := config.snippet("n0nce")
	if !strings.Contains(direct, `src="https://momento.corp.example/tracker.js"`) || strings.Contains(direct, "data-endpoint") || !strings.Contains(direct, `data-environment="stg"`) {
		t.Fatalf("direct momento snippet: %s", direct)
	}
	scripts, connects, images = config.policySources()
	if len(scripts) != 1 || scripts[0] != "https://momento.corp.example" || len(connects) != 1 || len(images) != 1 {
		t.Fatalf("direct collector must be allowed in every directive: %v %v %v", scripts, connects, images)
	}
}

func TestTrackingActiveSkipsAdminAndAPIPaths(t *testing.T) {
	config := trackingFrom(map[string]string{"tracking.enabled": "true", "tracking.provider": "ga4", "tracking.measurement_id": "G-1"})
	for path, want := range map[string]bool{"/": true, "/visits/new": true, "/lobby": true, "/admin": false, "/admin/settings": false, "/administrator": true,
		"/api/v1/version": false, "/mcp": false, "/healthz": false, "/readyz": false, "/metrics": false, "/momento/tracker.js": false} {
		if got := config.active(path); got != want {
			t.Fatalf("active(%q) = %v, want %v", path, got, want)
		}
	}
	config.IncludeAdmin = true
	if !config.active("/admin/settings") {
		t.Fatal("include_admin must admit the administrative pages")
	}
	scripts, connects, images := config.policySources()
	if scripts[0] != "https://www.googletagmanager.com" || len(connects) != 3 || len(images) != 2 {
		t.Fatalf("ga4 sources: %v %v %v", scripts, connects, images)
	}
}

func TestWithScriptNonceSurvivesCaseFoldingRunes(t *testing.T) {
	// U+0130 grows and U+212A shrinks under strings.ToLower; an index taken
	// from the folded copy would land the nonce inside the tag name.
	for _, snippet := range []string{
		"İİİİ<script>1</script>",
		"\u212a\u212a<SCRIPT src=\"https://t.example/x.js\"></SCRIPT><script nonce=\"keep\">2</script>",
		"<script>a</script>\n<script async src='/momento/tracker.js'></script>",
	} {
		got := withScriptNonce(snippet, "n0nce")
		if strings.Contains(got, "<sc nonce") || strings.Contains(got, "<SC nonce") {
			t.Fatalf("nonce written into the tag name: %s", got)
		}
		tags := regexp.MustCompile(`(?i)<script[^>]*>`).FindAllString(got, -1)
		if len(tags) != strings.Count(strings.ToLower(snippet), "<script") {
			t.Fatalf("lost a script tag: %s", got)
		}
		for _, tag := range tags {
			if !strings.Contains(strings.ToLower(tag), "nonce=") {
				t.Fatalf("tag without nonce: %s in %s", tag, got)
			}
		}
	}
	if got := withScriptNonce(`<script nonce="keep">x</script>`, "n0nce"); strings.Count(got, "nonce=") != 1 || !strings.Contains(got, `nonce="keep"`) {
		t.Fatalf("an existing nonce was replaced or doubled: %s", got)
	}
	if got := withScriptNonce("<script>x</script>", ""); got != "<script>x</script>" {
		t.Fatalf("empty nonce changed the snippet: %s", got)
	}
}

func TestSnippetOriginsReadsEveryAddress(t *testing.T) {
	snippet := `<script src="HTTPS://Momento.corp.example/tracker.js"></script>
<script>window.__t={endpoint:"https://momento.corp.example/collect/v1/events",pixel:'http://pixel.corp.example:8080/p.gif?id=1'};var k="\u212ahttp"+"s://joined.example",p="http://127.0.0.1:"+port;</script>`
	// The Kelvin sign before "http" must not shift the scan, and an address
	// assembled from string pieces — including a host cut off before its port —
	// is not an address the browser will see either, so it stays out.
	got := snippetOrigins(snippet)
	want := []string{"https://momento.corp.example", "http://pixel.corp.example:8080"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("snippetOrigins = %v, want %v", got, want)
	}
	config := trackingFrom(map[string]string{"tracking.enabled": "true", "tracking.provider": "custom", "tracking.custom_snippet": snippet,
		"tracking.allowed_hosts": "https://extra.example, https://*.cdn.example\nhttps://third.example"})
	scripts, _, _ := config.policySources()
	if strings.Join(scripts, " ") != strings.Join(append(want, "https://extra.example", "https://*.cdn.example", "https://third.example"), " ") {
		t.Fatalf("policy sources = %v", scripts)
	}
	policy := trackingContentSecurityPolicy(config, "/", "n0nce")
	if !strings.Contains(policy, "script-src 'self' 'nonce-n0nce' https://momento.corp.example http://pixel.corp.example:8080 https://extra.example https://*.cdn.example https://third.example;") {
		t.Fatalf("policy: %s", policy)
	}
	if !strings.Contains(policy, "connect-src 'self' https://momento.corp.example") || !strings.Contains(policy, "img-src 'self' data: blob: https://momento.corp.example") {
		t.Fatalf("policy: %s", policy)
	}
}

func TestInjectTrackingSnippetHonoursPlacement(t *testing.T) {
	document := "<html><HEAD><title>x</title></HEAD><body><div id=\"root\"></div></body></html>"
	head, ok := injectTrackingSnippet(document, "<script>1</script>", "head")
	if !ok || !strings.Contains(head, "<script>1</script>\n</HEAD>") {
		t.Fatalf("head placement: %v %s", ok, head)
	}
	body, ok := injectTrackingSnippet(document, "<script>1</script>", "body")
	if !ok || !strings.Contains(body, "<script>1</script>\n</body>") {
		t.Fatalf("body placement: %v %s", ok, body)
	}
	if _, ok := injectTrackingSnippet("plain text", "<script>1</script>", "head"); ok {
		t.Fatal("a document without head or body must not report an injection")
	}
	if _, ok := injectTrackingSnippet(document, "  ", "head"); ok {
		t.Fatal("an empty snippet must not report an injection")
	}
}

func TestTrackingValidation(t *testing.T) {
	cases := []struct {
		values map[string]string
		want   string
	}{
		{map[string]string{"tracking.enabled": "false", "tracking.provider": "momento"}, ""},
		{map[string]string{"tracking.enabled": "true", "tracking.provider": "none"}, "Provider"},
		{map[string]string{"tracking.enabled": "true", "tracking.provider": "momento"}, "Momento"},
		{map[string]string{"tracking.enabled": "true", "tracking.provider": "momento", "tracking.momento_url": "momento.corp.example", "tracking.momento_site_id": "1"}, "http(s)"},
		{map[string]string{"tracking.enabled": "true", "tracking.provider": "momento", "tracking.momento_url": "https://momento.corp.example", "tracking.momento_site_id": "1"}, ""},
		{map[string]string{"tracking.enabled": "true", "tracking.provider": "ga4"}, "Measurement"},
		{map[string]string{"tracking.enabled": "true", "tracking.provider": "matomo", "tracking.matomo_url": "https://m.example"}, "Matomo"},
		{map[string]string{"tracking.enabled": "true", "tracking.provider": "custom"}, "추적 코드"},
		{map[string]string{"tracking.enabled": "true", "tracking.provider": "custom", "tracking.custom_snippet": strings.Repeat("x", trackingMaxSnippetBytes+1)}, "8192"},
		{map[string]string{"tracking.enabled": "true", "tracking.provider": "pixel"}, "중 하나"},
	}
	for _, tc := range cases {
		got := trackingFrom(tc.values).validate()
		if (tc.want == "" && got != "") || (tc.want != "" && !strings.Contains(got, tc.want)) {
			t.Fatalf("validate(%v) = %q, want containing %q", tc.values, got, tc.want)
		}
	}
	if validateSettingValue("tracking.custom_snippet", strings.Repeat("x", trackingMaxSnippetBytes)) != "" {
		t.Fatal("a snippet at the limit was rejected")
	}
	if validateSettingValue("tracking.custom_snippet", strings.Repeat("x", trackingMaxSnippetBytes+1)) == "" {
		t.Fatal("a snippet over the limit was accepted")
	}
	if validateSettingValue("tracking.provider", "pixel") == "" || validateSettingValue("tracking.provider", "Momento") != "" {
		t.Fatal("provider list not enforced")
	}
	if validateSettingValue("tracking.placement", "footer") == "" || validateSettingValue("tracking.enabled", "yes") == "" {
		t.Fatal("placement or boolean not enforced")
	}
	if validateSettingValue("tracking.allowed_hosts", "momento.corp.example") == "" || validateSettingValue("tracking.allowed_hosts", "https://a.example, https://*.b.example") != "" {
		t.Fatal("allowed_hosts entries must be http(s) origins")
	}
	if validateSettingValue("tracking.momento_url", "ftp://x") == "" || validateSettingValue("tracking.momento_url", "") != "" {
		t.Fatal("momento_url must be an http(s) URL or empty")
	}
}

func TestViolationRecorderKeepsDistinctOrigins(t *testing.T) {
	recorder := newViolationRecorder()
	moment := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	recorder.now = func() time.Time { return moment }
	for range 5 {
		recorder.record("https://momento.corp.example/collect/v1/events", "connect-src https://x", "https://visit.corp.example/")
	}
	recorder.record("chrome-extension://abc/x.js", "script-src-elem", "/")
	recorder.record("data", "img-src", "/")
	recorder.record("", "", "/")
	moment = moment.Add(time.Minute)
	recorder.record("https://pixel.corp.example/p.gif", "", "/visits")
	items := recorder.list(trackingFrom(map[string]string{"tracking.provider": "custom", "tracking.allowed_hosts": "https://*.corp.example"}))
	if len(items) != 2 {
		t.Fatalf("expected two distinct origins, got %+v", items)
	}
	if items[0].Origin != "https://pixel.corp.example" || items[0].Directive != "connect-src" || items[0].Count != 1 {
		t.Fatalf("most recent first with a default directive: %+v", items[0])
	}
	if items[1].Origin != "https://momento.corp.example" || items[1].Directive != "connect-src" || items[1].Count != 5 || !items[1].Allowed {
		t.Fatalf("repeated origin: %+v", items[1])
	}
	if !items[0].Allowed {
		t.Fatal("wildcard allow list entry must mark the origin as allowed")
	}
	for i := range maxTrackingViolations + 10 {
		moment = moment.Add(time.Second)
		recorder.record("https://host"+strings.Repeat("x", i%7)+strings.Repeat("y", i/7)+".example/", "img-src", "/")
	}
	if got := len(recorder.violations); got != maxTrackingViolations {
		t.Fatalf("recorder holds %d entries, want %d", got, maxTrackingViolations)
	}
	recorder.forget()
	if len(recorder.list(trackingConfig{})) != 0 {
		t.Fatal("forget left entries behind")
	}
	if got := addAllowedHost("https://a.example", "https://B.example/"); got != "https://a.example, https://B.example" {
		t.Fatalf("addAllowedHost = %q", got)
	}
	if got := addAllowedHost("https://a.example, https://b.example", "https://A.example"); got != "https://a.example, https://b.example" {
		t.Fatalf("duplicate origin appended: %q", got)
	}
	if got := addAllowedHost("", "https://a.example"); got != "https://a.example" {
		t.Fatalf("first origin: %q", got)
	}
}

func metaNonce(t *testing.T, document string) string {
	t.Helper()
	match := regexp.MustCompile(`<meta property="csp-nonce" content="([^"]+)">`).FindStringSubmatch(document)
	if match == nil {
		t.Fatalf("page without a csp-nonce meta tag: %s", document)
	}
	return match[1]
}

// TestTrackingSnippetServedUnderNoncePolicy walks the operator's path: nothing
// on a fresh installation, then Momento through the same-origin proxy, then a
// pasted snippet with an external origin, then off again.
func TestTrackingSnippetServedUnderNoncePolicy(t *testing.T) {
	env := newTestEnv(t)

	fresh := env.do(http.MethodGet, "/", nil)
	if fresh.Code != http.StatusOK || strings.Contains(fresh.Body.String(), "<script") {
		t.Fatalf("fresh installation served a script: %d %s", fresh.Code, fresh.Body.String())
	}
	if policy := fresh.Header().Get("Content-Security-Policy"); policy != contentSecurityPolicy(metaNonce(t, fresh.Body.String())) {
		t.Fatalf("fresh installation policy changed: %s", policy)
	}
	if probe := env.do(http.MethodGet, "/momento/tracker.js", nil); probe.Code != http.StatusNotFound {
		t.Fatalf("momento proxy answered %d while tracking is off", probe.Code)
	}

	// Switching on without a provider, or a provider without its address, is
	// refused so the screen never saves a configuration that does nothing.
	if response := env.do(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"tracking.enabled": "true"}}); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "tracking_incomplete") {
		t.Fatalf("enabling without a provider returned %d: %s", response.Code, response.Body.String())
	}
	if response := env.do(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"tracking.enabled": "true", "tracking.provider": "momento", "tracking.momento_site_id": "vf"}}); response.Code != http.StatusBadRequest {
		t.Fatalf("momento without an address returned %d: %s", response.Code, response.Body.String())
	}

	var collectorRequests []*http.Request
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		collectorRequests = append(collectorRequests, r.Clone(r.Context()))
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte("/* momento tracker */"))
	}))
	t.Cleanup(collector.Close)
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{
		"tracking.enabled": "true", "tracking.provider": "momento", "tracking.momento_url": collector.URL + "/", "tracking.momento_site_id": "vf-01",
	}}, http.StatusOK)

	page := env.do(http.MethodGet, "/visits", nil)
	body := page.Body.String()
	nonce := metaNonce(t, body)
	snippet := `<script nonce="` + nonce + `" async src="/momento/tracker.js" data-site-id="vf-01" data-environment="prd" data-contract-version="1" data-endpoint="/momento"></script>`
	if !strings.Contains(body, snippet+"\n</head>") {
		t.Fatalf("page lacks the momento snippet in <head>:\n%s", body)
	}
	policy := page.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "script-src 'self' 'nonce-"+nonce+"'") || !strings.Contains(policy, "report-uri "+cspReportPath) || strings.Contains(policy, "127.0.0.1") {
		t.Fatalf("page policy: %s", policy)
	}
	if strings.Contains(strings.SplitN(policy, "script-src", 2)[1], "'unsafe-inline'") {
		t.Fatalf("scripts opened with 'unsafe-inline': %s", policy)
	}

	admin := env.do(http.MethodGet, "/admin/settings", nil)
	if strings.Contains(admin.Body.String(), "<script") || strings.Contains(admin.Header().Get("Content-Security-Policy"), "report-uri") {
		t.Fatalf("administrative page carried the snippet with include_admin off:\n%s", admin.Body.String())
	}
	api := env.do(http.MethodGet, "/api/v1/version", nil)
	if header := api.Header().Get("Content-Security-Policy"); strings.Contains(header, "report-uri") || strings.Contains(header, "'nonce-") && strings.Contains(strings.SplitN(header, "script-src", 2)[1], "'nonce-") {
		t.Fatalf("API path received the page tracking policy: %s", header)
	}

	// The proxy carries the tracker from the collector, without the visitor's
	// session cookie, and stops answering the moment tracking is off.
	proxied := env.do(http.MethodGet, "/momento/tracker.js?v=1", nil)
	if proxied.Code != http.StatusOK || proxied.Body.String() != "/* momento tracker */" {
		t.Fatalf("proxied tracker: %d %s", proxied.Code, proxied.Body.String())
	}
	if len(collectorRequests) != 1 || collectorRequests[0].URL.Path != "/tracker.js" || collectorRequests[0].URL.RawQuery != "v=1" {
		t.Fatalf("collector saw %+v", collectorRequests)
	}
	if collectorRequests[0].Header.Get("Cookie") != "" || collectorRequests[0].Header.Get("X-Forwarded-For") == "" {
		t.Fatalf("collector request headers: %v", collectorRequests[0].Header)
	}

	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"tracking.include_admin": "true", "tracking.placement": "body"}}, http.StatusOK)
	admin = env.do(http.MethodGet, "/admin/settings", nil)
	if !strings.Contains(admin.Body.String(), `<script nonce="`+metaNonce(t, admin.Body.String())+`" async src="/momento/tracker.js"`) || !strings.Contains(admin.Body.String(), `data-endpoint="/momento"></script>`+"\n</body>") {
		t.Fatalf("include_admin with body placement:\n%s", admin.Body.String())
	}

	// A pasted snippet: its origins enter the policy without any policy work.
	custom := `<script src="https://tracker.corp.example/t.js" data-x="1"></script><script>window.__ep="https://beacon.corp.example/collect";</script>`
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"tracking.provider": "custom", "tracking.custom_snippet": custom}}, http.StatusOK)
	page = env.do(http.MethodGet, "/", nil)
	body = page.Body.String()
	nonce = metaNonce(t, body)
	if strings.Count(body, `nonce="`+nonce+`"`) != 2 || !strings.Contains(body, `<script nonce="`+nonce+`" src="https://tracker.corp.example/t.js"`) {
		t.Fatalf("every script tag must carry the request nonce:\n%s", body)
	}
	policy = page.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "script-src 'self' 'nonce-"+nonce+"' https://tracker.corp.example https://beacon.corp.example") || !strings.Contains(policy, "connect-src 'self' https://tracker.corp.example https://beacon.corp.example") {
		t.Fatalf("custom snippet policy: %s", policy)
	}
	if response := env.do(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"tracking.custom_snippet": strings.Repeat("<script>1</script>", 500)}}); response.Code != http.StatusBadRequest {
		t.Fatalf("an oversized snippet was saved: %d", response.Code)
	}
	if probe := env.do(http.MethodGet, "/momento/tracker.js", nil); probe.Code != http.StatusNotFound {
		t.Fatalf("momento proxy answered %d for another provider", probe.Code)
	}

	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"tracking.enabled": "false"}}, http.StatusOK)
	off := env.do(http.MethodGet, "/", nil)
	if strings.Contains(off.Body.String(), "<script") || off.Header().Get("Content-Security-Policy") != contentSecurityPolicy(metaNonce(t, off.Body.String())) {
		t.Fatalf("switching tracking off did not restore the strict policy: %s", off.Header().Get("Content-Security-Policy"))
	}
}

func TestTrackingViolationsReportedAndAllowed(t *testing.T) {
	env := newTestEnv(t)
	env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{
		"tracking.enabled": "true", "tracking.provider": "custom", "tracking.custom_snippet": `<script src="https://tracker.corp.example/t.js"></script>`,
	}}, http.StatusOK)

	report := func(blocked, directive string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, cspReportPath, strings.NewReader(`{"csp-report":{"blocked-uri":"`+blocked+`","effective-directive":"`+directive+`","document-uri":"https://visit.corp.example/visits"}}`))
		request.RemoteAddr = "10.0.0.9:5000"
		request.Header.Set("Content-Type", "application/csp-report")
		response := httptest.NewRecorder()
		env.handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("report answered %d: %s", response.Code, response.Body.String())
		}
	}
	report("https://beacon.corp.example/collect/v1/events", "connect-src")
	report("https://beacon.corp.example/collect/v1/events", "connect-src")
	report("https://beacon.corp.example/p.gif", "img-src")
	report("https://tracker.corp.example/t.js", "script-src-elem")

	listed := env.json(http.MethodGet, "/api/v1/admin/tracking/violations", nil, http.StatusOK)
	items, _ := listed["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("expected three distinct origin/directive pairs, got %v", items)
	}
	byKey := map[string]map[string]any{}
	for _, item := range items {
		entry, _ := item.(map[string]any)
		byKey[entry["directive"].(string)+" "+entry["origin"].(string)] = entry
	}
	if beacon := byKey["connect-src https://beacon.corp.example"]; beacon == nil || beacon["count"] != float64(2) || beacon["allowed"] != false || beacon["page"] != "https://visit.corp.example/visits" {
		t.Fatalf("beacon violation: %v", byKey)
	}
	if tracker := byKey["script-src-elem https://tracker.corp.example"]; tracker == nil || tracker["allowed"] != true {
		t.Fatalf("an origin the snippet already names must show as allowed: %v", byKey)
	}

	if response := env.do(http.MethodPost, "/api/v1/admin/tracking/allow", map[string]string{"origin": "beacon.corp.example"}); response.Code != http.StatusBadRequest {
		t.Fatalf("a bare host was accepted as an origin: %d", response.Code)
	}
	allowed := env.json(http.MethodPost, "/api/v1/admin/tracking/allow", map[string]string{"origin": "https://beacon.corp.example/collect"}, http.StatusOK)
	if allowed["allowedHosts"] != "https://beacon.corp.example" {
		t.Fatalf("allow list after the click: %v", allowed)
	}
	env.json(http.MethodPost, "/api/v1/admin/tracking/allow", map[string]string{"origin": "https://beacon.corp.example"}, http.StatusOK)
	settings := env.json(http.MethodGet, "/api/v1/settings", nil, http.StatusOK)
	for _, item := range settings["items"].([]any) {
		entry := item.(map[string]any)
		if entry["key"] == "tracking.allowed_hosts" && entry["value"] != "https://beacon.corp.example" {
			t.Fatalf("allowed_hosts setting = %v", entry["value"])
		}
	}
	page := env.do(http.MethodGet, "/", nil)
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "connect-src 'self' https://tracker.corp.example https://beacon.corp.example") {
		t.Fatalf("allowed origin missing from the policy: %s", page.Header().Get("Content-Security-Policy"))
	}
	listed = env.json(http.MethodGet, "/api/v1/admin/tracking/violations", nil, http.StatusOK)
	for _, item := range listed["items"].([]any) {
		if entry := item.(map[string]any); entry["allowed"] != true {
			t.Fatalf("violation still shown as blocked after allowing: %v", entry)
		}
	}
	audit := env.json(http.MethodGet, "/api/v1/admin/audit-logs?action=settings.update", nil, http.StatusOK)
	if entries, _ := audit["items"].([]any); len(entries) < 2 {
		t.Fatalf("allowing an origin must be audited like any settings change: %v", audit)
	}

	env.json(http.MethodDelete, "/api/v1/admin/tracking/violations", nil, http.StatusNoContent)
	listed = env.json(http.MethodGet, "/api/v1/admin/tracking/violations", nil, http.StatusOK)
	if items, _ := listed["items"].([]any); len(items) != 0 {
		t.Fatalf("violations survived clearing: %v", items)
	}

	// Non-administrators cannot read or change the list.
	env.createLocalUser("tracking-viewer", RoleUser, "")
	env.login("tracking-viewer", testAdminPassword)
	if response := env.do(http.MethodGet, "/api/v1/admin/tracking/violations", nil); response.Code != http.StatusForbidden {
		t.Fatalf("requester read violations: %d", response.Code)
	}
}
