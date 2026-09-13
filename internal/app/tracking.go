package app

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
)

// Visit tracking. An administrator picks a tracker in the settings screen and
// the served pages carry its snippet. The hard part is not the <script> tag but
// the content security policy: the app locks scripts to its own origin, so a
// pasted snippet would be refused silently. Every page therefore gets a
// per-request nonce on the snippet's script tags, the origins the snippet
// names are added to the policy, and while tracking is on the browser reports
// what it still refused so the settings screen can show it.

const (
	trackingProviderNone    = "none"
	trackingProviderMomento = "momento"
	trackingProviderGA4     = "ga4"
	trackingProviderGTM     = "gtm"
	trackingProviderMatomo  = "matomo"
	trackingProviderCustom  = "custom"

	// trackingMaxSnippetBytes bounds a pasted snippet. A loader is a few hundred
	// bytes; anything larger is a mistake that would be served on every page.
	trackingMaxSnippetBytes = 8 * 1024

	// momentoProxyPrefix is the same-origin path the app forwards to the Momento
	// collector, so a Momento install needs no external origin in the policy.
	momentoProxyPrefix = "/momento"

	// cspReportPath receives what the browser refused while tracking is on.
	cspReportPath = "/api/v1/tracking/csp-report"
)

var trackingProviders = []string{trackingProviderNone, trackingProviderMomento, trackingProviderGA4, trackingProviderGTM, trackingProviderMatomo, trackingProviderCustom}

type trackingConfig struct {
	Enabled            bool
	Provider           string
	MomentoURL         string
	MomentoSiteID      string
	MomentoEnvironment string
	MomentoProxy       bool
	MeasurementID      string
	MatomoURL          string
	MatomoSiteID       string
	CustomSnippet      string
	AllowedHosts       string
	IncludeAdmin       bool
	Placement          string
}

// trackingConfigFrom builds the configuration from a settings reader, which is
// the stored value in normal operation and the pending value while a settings
// update is being validated.
func trackingConfigFrom(get func(key string) string) trackingConfig {
	config := trackingConfig{
		Enabled:            get("tracking.enabled") == "true",
		Provider:           strings.ToLower(strings.TrimSpace(get("tracking.provider"))),
		MomentoURL:         strings.TrimSpace(get("tracking.momento_url")),
		MomentoSiteID:      strings.TrimSpace(get("tracking.momento_site_id")),
		MomentoEnvironment: strings.TrimSpace(get("tracking.momento_environment")),
		MomentoProxy:       get("tracking.momento_proxy") != "false",
		MeasurementID:      strings.TrimSpace(get("tracking.measurement_id")),
		MatomoURL:          strings.TrimSpace(get("tracking.matomo_url")),
		MatomoSiteID:       strings.TrimSpace(get("tracking.matomo_site_id")),
		CustomSnippet:      strings.TrimSpace(get("tracking.custom_snippet")),
		AllowedHosts:       get("tracking.allowed_hosts"),
		IncludeAdmin:       get("tracking.include_admin") == "true",
		Placement:          strings.ToLower(strings.TrimSpace(get("tracking.placement"))),
	}
	if config.Provider == "" {
		config.Provider = trackingProviderNone
	}
	if config.MomentoEnvironment == "" {
		config.MomentoEnvironment = "prd"
	}
	if config.Placement != "body" {
		config.Placement = "head"
	}
	return config
}

func (s *Server) trackingConfig(ctx context.Context) trackingConfig {
	return trackingConfigFrom(func(key string) string {
		value, _ := s.getSetting(ctx, key)
		return value
	})
}

// active reports whether a page at path should carry the snippet. API and
// health paths never do, and administrative pages only when asked for, because
// console traffic is rarely the visitor data anybody wants to count.
func (c trackingConfig) active(path string) bool {
	if !c.Enabled || c.Provider == trackingProviderNone {
		return false
	}
	if isAPIPath(path) || path == "/healthz" || path == "/readyz" || path == "/metrics" || strings.HasPrefix(path, momentoProxyPrefix+"/") {
		return false
	}
	if !c.IncludeAdmin && (path == "/admin" || strings.HasPrefix(path, "/admin/")) {
		return false
	}
	return strings.TrimSpace(c.snippet("")) != ""
}

// validate reports what the chosen provider still needs, in the words the
// settings screen shows. An empty string means the configuration is complete.
func (c trackingConfig) validate() string {
	if !c.Enabled {
		return ""
	}
	switch c.Provider {
	case trackingProviderNone:
		return "방문 추적을 켜려면 Provider를 선택하세요"
	case trackingProviderMomento:
		if c.MomentoURL == "" || c.MomentoSiteID == "" {
			return "Momento에는 수집기 주소와 사이트 ID가 필요합니다"
		}
		if originOf(c.MomentoURL) == "" {
			return "Momento 수집기 주소는 http(s) URL이어야 합니다"
		}
	case trackingProviderGA4, trackingProviderGTM:
		if c.MeasurementID == "" {
			return "GA4·GTM에는 Measurement ID가 필요합니다"
		}
	case trackingProviderMatomo:
		if c.MatomoURL == "" || c.MatomoSiteID == "" {
			return "Matomo에는 서버 주소와 사이트 ID가 필요합니다"
		}
		if originOf(c.MatomoURL) == "" {
			return "Matomo 서버 주소는 http(s) URL이어야 합니다"
		}
	case trackingProviderCustom:
		if c.CustomSnippet == "" {
			return "직접 입력 Provider에는 추적 코드가 필요합니다"
		}
	default:
		return "추적 Provider는 " + strings.Join(trackingProviders, ", ") + " 중 하나여야 합니다"
	}
	if len(c.CustomSnippet) > trackingMaxSnippetBytes {
		return fmt.Sprintf("추적 코드는 %d바이트를 넘을 수 없습니다", trackingMaxSnippetBytes)
	}
	return ""
}

// snippet renders the markup to inject. Every script tag carries the nonce so
// the policy can stay strict.
func (c trackingConfig) snippet(nonce string) string {
	switch c.Provider {
	case trackingProviderMomento:
		site := html.EscapeString(c.MomentoSiteID)
		if site == "" {
			return ""
		}
		src, endpoint := "", ""
		if c.MomentoProxy {
			// The same-origin proxy keeps the collector out of the policy: the
			// loader and its beacons both stay on this host.
			src = momentoProxyPrefix + "/tracker.js"
			endpoint = ` data-endpoint="` + momentoProxyPrefix + `"`
		} else {
			base := strings.TrimRight(c.MomentoURL, "/")
			if base == "" {
				return ""
			}
			src = html.EscapeString(base) + "/tracker.js"
		}
		return withScriptNonce(fmt.Sprintf(`<script async src="%s" data-site-id="%s" data-environment="%s" data-contract-version="1"%s></script>`,
			src, site, html.EscapeString(c.MomentoEnvironment), endpoint), nonce)
	case trackingProviderGA4:
		id := html.EscapeString(c.MeasurementID)
		if id == "" {
			return ""
		}
		return withScriptNonce(fmt.Sprintf(`<script async src="https://www.googletagmanager.com/gtag/js?id=%s"></script>
<script>window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}gtag('js',new Date());gtag('config','%s');</script>`, id, id), nonce)
	case trackingProviderGTM:
		id := html.EscapeString(c.MeasurementID)
		if id == "" {
			return ""
		}
		return withScriptNonce(fmt.Sprintf(`<script>(function(w,d,s,l,i){w[l]=w[l]||[];w[l].push({'gtm.start':new Date().getTime(),event:'gtm.js'});var f=d.getElementsByTagName(s)[0],j=d.createElement(s),dl=l!='dataLayer'?'&l='+l:'';j.async=true;j.src='https://www.googletagmanager.com/gtm.js?id='+i+dl;f.parentNode.insertBefore(j,f);})(window,document,'script','dataLayer','%s');</script>`, id), nonce)
	case trackingProviderMatomo:
		base := strings.TrimRight(c.MatomoURL, "/")
		site := html.EscapeString(c.MatomoSiteID)
		if base == "" || site == "" {
			return ""
		}
		return withScriptNonce(fmt.Sprintf(`<script>var _paq=window._paq=window._paq||[];_paq.push(['trackPageView']);_paq.push(['enableLinkTracking']);(function(){var u="%s/";_paq.push(['setTrackerUrl',u+'matomo.php']);_paq.push(['setSiteId','%s']);var d=document,g=d.createElement('script'),s=d.getElementsByTagName('script')[0];g.async=true;g.src=u+'matomo.js';s.parentNode.insertBefore(g,s);})();</script>`, html.EscapeString(base), site), nonce)
	case trackingProviderCustom:
		return withScriptNonce(c.CustomSnippet, nonce)
	}
	return ""
}

// policySources lists the extra origins the snippet needs in script-src,
// connect-src and img-src, derived from the provider so a common setup needs
// no policy knowledge at all.
func (c trackingConfig) policySources() (scripts, connects, images []string) {
	add := func(origin string) {
		scripts = append(scripts, origin)
		connects = append(connects, origin)
		images = append(images, origin)
	}
	switch c.Provider {
	case trackingProviderMomento:
		if !c.MomentoProxy {
			if origin := originOf(c.MomentoURL); origin != "" {
				add(origin)
			}
		}
	case trackingProviderGA4, trackingProviderGTM:
		scripts = append(scripts, "https://www.googletagmanager.com")
		connects = append(connects, "https://www.google-analytics.com", "https://analytics.google.com", "https://*.google-analytics.com")
		images = append(images, "https://www.google-analytics.com", "https://www.googletagmanager.com")
	case trackingProviderMatomo:
		if origin := originOf(c.MatomoURL); origin != "" {
			add(origin)
		}
	}
	// A pasted snippet names the addresses it loads and reports to, so those
	// origins are allowed without anybody reading a policy error first.
	for _, origin := range snippetOrigins(c.CustomSnippet) {
		add(origin)
	}
	for _, host := range allowedHostList(c.AllowedHosts) {
		add(host)
	}
	return scripts, connects, images
}

// allowedHostList splits the administrator's allow list, which accepts commas,
// spaces or new lines between entries.
func allowedHostList(raw string) []string {
	return strings.FieldsFunc(raw, func(letter rune) bool {
		return letter == ',' || letter == ' ' || letter == '\n' || letter == '\r' || letter == '\t'
	})
}

// snippetOrigins lists every http(s) origin written into a snippet: the script
// it loads, the endpoint it posts to, the pixel it requests.
func snippetOrigins(snippet string) []string {
	origins := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for index := 0; index < len(snippet); {
		start := indexFold(snippet[index:], "http")
		if start < 0 {
			break
		}
		start += index
		end := start
		for end < len(snippet) && !isURLBoundary(snippet[end]) {
			end++
		}
		index = end
		origin := originOf(snippet[start:end])
		if origin == "" {
			continue
		}
		if _, duplicate := seen[origin]; duplicate {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	return origins
}

// isURLBoundary reports the characters that cannot appear in a URL written
// inside HTML or JavaScript, which is where each address ends.
func isURLBoundary(letter byte) bool {
	switch letter {
	case '"', '\'', '`', '<', '>', ' ', '\t', '\n', '\r', ')', ',', ';', '\\', '+':
		return true
	}
	return false
}

// originOf reduces an address to scheme://host, or "" when it is not an
// absolute http(s) address.
func originOf(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	// A host cut off before its port ("http://127.0.0.1:" + port) is not an
	// origin a browser would accept in the policy.
	if err != nil || parsed.Hostname() == "" || strings.HasSuffix(parsed.Host, ":") {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + strings.ToLower(parsed.Host)
}

// withScriptNonce adds the nonce to every script tag that does not already
// carry one, which is what lets a pasted snippet run under a strict policy.
func withScriptNonce(snippet, nonce string) string {
	if nonce == "" || snippet == "" {
		return snippet
	}
	var builder strings.Builder
	remaining := snippet
	for {
		index := indexFold(remaining, "<script")
		if index < 0 {
			builder.WriteString(remaining)
			return builder.String()
		}
		end := index + len("<script")
		builder.WriteString(remaining[:end])
		tag := remaining[end:]
		if closing := strings.IndexByte(tag, '>'); closing >= 0 {
			tag = tag[:closing]
		}
		if indexFold(tag, "nonce=") < 0 {
			builder.WriteString(` nonce="` + html.EscapeString(nonce) + `"`)
		}
		remaining = remaining[end:]
	}
}

// indexFold finds sub in s ignoring ASCII case and returns an index into s.
//
// strings.ToLower is the obvious way and the wrong one: it changes byte
// lengths for some runes — U+212A KELVIN SIGN is three bytes and folds to a
// one-byte 'k', U+0130 'İ' is two and folds to three — so an index taken from
// the folded copy lands somewhere else in the original and the nonce ends up
// inside the tag name. Every needle here is ASCII, and folding only ASCII
// keeps every byte in place.
func indexFold(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if foldASCII(s[i+j]) != foldASCII(sub[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func foldASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// injectTrackingSnippet places the snippet before </head> or </body>. It
// reports false when the document has neither, so the caller keeps the strict
// policy rather than opening it for a snippet that is not on the page.
func injectTrackingSnippet(document, snippet, placement string) (string, bool) {
	if strings.TrimSpace(snippet) == "" {
		return document, false
	}
	closers := []string{"</head>", "</body>"}
	if placement == "body" {
		closers = []string{"</body>", "</head>"}
	}
	for _, closer := range closers {
		if index := indexFold(document, closer); index >= 0 {
			return document[:index] + snippet + "\n" + document[index:], true
		}
	}
	return document, false
}

// trackingContentSecurityPolicy is the page policy with what the configured
// tracker needs: the request nonce in script-src, the origins the snippet names,
// and a report-uri so refused requests reach the settings screen. When tracking
// is off for the path it is exactly the strict policy every request already
// receives, so switching tracking off narrows the policy again on its own.
func trackingContentSecurityPolicy(config trackingConfig, path, nonce string) string {
	if nonce == "" || !config.active(path) {
		return contentSecurityPolicy(nonce)
	}
	scripts, connects, images := config.policySources()
	scripts = append([]string{"'nonce-" + nonce + "'"}, scripts...)
	return pageContentSecurityPolicy(nonce, scripts, connects, images, true)
}
