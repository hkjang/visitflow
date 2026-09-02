package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestParseTrustedProxiesAcceptsAddressesBlocksAndPrivate(t *testing.T) {
	prefixes, invalid := parseTrustedProxies("10.0.0.5, 192.168.10.0/24\n2001:db8::1")
	if len(invalid) != 0 {
		t.Fatalf("valid entries were rejected: %v", invalid)
	}
	if len(prefixes) != 3 {
		t.Fatalf("parsed %d prefixes, want 3", len(prefixes))
	}
	if addr, _ := parseRequestAddr("192.168.10.77"); !trustedProxy(addr, prefixes) {
		t.Fatal("an address inside the configured block was not trusted")
	}
	if addr, _ := parseRequestAddr("192.168.11.1"); trustedProxy(addr, prefixes) {
		t.Fatal("an address outside every block was trusted")
	}
	if addr, _ := parseRequestAddr("10.0.0.5"); !trustedProxy(addr, prefixes) {
		t.Fatal("a single configured address was not trusted")
	}
	if addr, _ := parseRequestAddr("10.0.0.6"); trustedProxy(addr, prefixes) {
		t.Fatal("a single configured address widened into its whole network")
	}

	private, invalid := parseTrustedProxies("private")
	if len(invalid) != 0 || len(private) == 0 {
		t.Fatalf("the private keyword did not expand: %v %v", private, invalid)
	}
	if addr, _ := parseRequestAddr("172.16.4.9"); !trustedProxy(addr, private) {
		t.Fatal("private must cover RFC1918 addresses")
	}
	if addr, _ := parseRequestAddr("203.0.113.9"); trustedProxy(addr, private) {
		t.Fatal("private must not cover public addresses")
	}

	if _, invalid := parseTrustedProxies("10.0.0.0/8 not-an-address 999.1.1.1"); len(invalid) != 2 {
		t.Fatalf("invalid entries were not reported: %v", invalid)
	}
}

func TestResolveClientAddrIgnoresForwardedHeadersFromUntrustedPeers(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-For", "203.0.113.9")
	header.Set("X-Real-IP", "203.0.113.9")

	if got := resolveClientAddr("198.51.100.7:41234", header, nil); got != "198.51.100.7" {
		t.Fatalf("without a trusted proxy list the peer must be used, got %q", got)
	}
	trusted, _ := parseTrustedProxies("10.9.0.1")
	if got := resolveClientAddr("198.51.100.7:41234", header, trusted); got != "198.51.100.7" {
		t.Fatalf("a peer that is not a listed proxy must not be able to rewrite its address, got %q", got)
	}
}

func TestResolveClientAddrUsesForwardedChainFromTrustedProxies(t *testing.T) {
	trusted, _ := parseTrustedProxies("10.9.0.0/24 private")
	header := http.Header{}
	header.Set("X-Forwarded-For", "203.0.113.9, 10.9.0.8")
	if got := resolveClientAddr("10.9.0.1:5000", header, trusted); got != "203.0.113.9" {
		t.Fatalf("the closest untrusted hop must win, got %q", got)
	}

	// A caller that appends its own header still cannot claim another address:
	// the entry it controls is the rightmost one the proxy did not write.
	spoofed := http.Header{}
	spoofed.Set("X-Forwarded-For", "8.8.8.8, 203.0.113.9")
	if got := resolveClientAddr("10.9.0.1:5000", spoofed, trusted); got != "203.0.113.9" {
		t.Fatalf("a spoofed leading entry changed the result, got %q", got)
	}

	multi := http.Header{}
	multi.Add("X-Forwarded-For", "203.0.113.9")
	multi.Add("X-Forwarded-For", "10.9.0.8")
	if got := resolveClientAddr("10.9.0.1:5000", multi, trusted); got != "203.0.113.9" {
		t.Fatalf("a chain split across header lines was misread, got %q", got)
	}

	internal := http.Header{}
	internal.Set("X-Forwarded-For", "192.168.4.20, 10.9.0.8")
	if got := resolveClientAddr("10.9.0.1:5000", internal, trusted); got != "192.168.4.20" {
		t.Fatalf("an all-internal chain must report the original caller, got %q", got)
	}

	realIP := http.Header{}
	realIP.Set("X-Real-IP", "203.0.113.44")
	if got := resolveClientAddr("10.9.0.1:5000", realIP, trusted); got != "203.0.113.44" {
		t.Fatalf("X-Real-IP is the only hint a single proxy sends, got %q", got)
	}

	if got := resolveClientAddr("10.9.0.1:5000", http.Header{}, trusted); got != "10.9.0.1" {
		t.Fatalf("a proxy that forwards nothing must fall back to the peer, got %q", got)
	}
}

func TestResolveClientAddrNormalisesAddressForms(t *testing.T) {
	trusted, _ := parseTrustedProxies("::1")
	header := http.Header{}
	header.Set("X-Forwarded-For", "[2001:db8::5]:9999")
	if got := resolveClientAddr("[::1]:8080", header, trusted); got != "2001:db8::5" {
		t.Fatalf("a bracketed IPv6 entry with a port was not parsed, got %q", got)
	}
	if got := resolveClientAddr("", http.Header{}, nil); got != "unknown" {
		t.Fatalf("an empty peer address = %q, want unknown", got)
	}
	if got := resolveClientAddr("::ffff:192.0.2.4", http.Header{}, nil); got != "192.0.2.4" {
		t.Fatalf("an IPv4-mapped peer was not unmapped, got %q", got)
	}
}

// A request carrying a forged header must keep counting against its own address
// while security.trusted_proxies is unset, which is the default installation.
func TestResolveClientIPMiddlewareIgnoresHeadersWithoutConfiguredProxies(t *testing.T) {
	server := NewServer(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{}, "test", "test", "test")
	seen := ""
	probe := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = clientIP(r) })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	request.RemoteAddr = "198.51.100.7:41234"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	server.resolveClientIP(probe).ServeHTTP(httptest.NewRecorder(), request)
	if seen != "198.51.100.7" {
		t.Fatalf("the middleware attributed the request to %q", seen)
	}
}

func TestValidateTrustedProxiesSetting(t *testing.T) {
	if message := validateSettingValue("security.trusted_proxies", ""); message != "" {
		t.Fatalf("an empty proxy list must be allowed: %s", message)
	}
	if message := validateSettingValue("security.trusted_proxies", "10.0.0.0/8 private 2001:db8::1"); message != "" {
		t.Fatalf("a valid proxy list was rejected: %s", message)
	}
	if message := validateSettingValue("security.trusted_proxies", "10.0.0.0/8 nope"); !strings.Contains(message, "nope") {
		t.Fatalf("an invalid proxy list was accepted: %q", message)
	}
}

func TestClientIPFallsBackToThePeerWithoutTheMiddleware(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.168.10.4:53112"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientIP(request); got != "192.168.10.4" {
		t.Fatalf("clientIP() = %q", got)
	}
}
