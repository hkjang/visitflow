package app

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// VisitFlow attributes login lockouts, public rate limits, consent records and
// audit entries to a client address, so that address must not be something the
// caller can choose. Forwarded headers are therefore only read when the request
// actually arrives from an address the operator listed in
// security.trusted_proxies; with the setting empty the peer address is used and
// X-Forwarded-For is ignored entirely.
type clientIPKey struct{}

// privateProxyToken lets an operator write one word instead of listing every
// internal range, which is how an on-premises reverse proxy is normally placed.
const privateProxyToken = "private"

var privateProxyPrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}

// parseTrustedProxies reads a whitespace or comma separated list of addresses,
// CIDR blocks and the "private" keyword. Unparseable entries are reported so the
// settings form can reject them instead of silently trusting nothing.
func parseTrustedProxies(value string) ([]netip.Prefix, []string) {
	prefixes := []netip.Prefix{}
	invalid := []string{}
	for _, entry := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
		if strings.EqualFold(entry, privateProxyToken) {
			prefixes = append(prefixes, privateProxyPrefixes...)
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			prefixes = append(prefixes, netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()))
			continue
		}
		invalid = append(invalid, entry)
	}
	return prefixes, invalid
}

func trustedProxy(addr netip.Addr, trusted []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// parseRequestAddr accepts the forms proxies actually emit: a bare address, an
// address with a port, and a bracketed IPv6 address with a port.
func parseRequestAddr(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, false
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.Unmap(), true
	}
	if addrPort, err := netip.ParseAddrPort(value); err == nil {
		return addrPort.Addr().Unmap(), true
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		if addr, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
			return addr.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

// resolveClientAddr walks the forwarding chain from the peer outwards and stops
// at the first address that is not itself a trusted proxy: that is the closest
// hop VisitFlow can still hold responsible. An address appended by an attacker
// sits further left in the chain and can only push the result towards addresses
// it does not control.
func resolveClientAddr(remoteAddr string, header http.Header, trusted []netip.Prefix) string {
	peer, ok := parseRequestAddr(remoteAddr)
	if !ok {
		return "unknown"
	}
	if len(trusted) == 0 || !trustedProxy(peer, trusted) {
		return peer.String()
	}
	chain := []netip.Addr{}
	for _, value := range header.Values("X-Forwarded-For") {
		for _, entry := range strings.Split(value, ",") {
			if addr, ok := parseRequestAddr(entry); ok {
				chain = append(chain, addr)
			}
		}
	}
	for index := len(chain) - 1; index >= 0; index-- {
		if !trustedProxy(chain[index], trusted) {
			return chain[index].String()
		}
	}
	if len(chain) > 0 {
		// Every hop is internal, so the original caller is internal too.
		return chain[0].String()
	}
	if addr, ok := parseRequestAddr(header.Get("X-Real-IP")); ok {
		return addr.String()
	}
	return peer.String()
}

// trustedProxies caches the parsed setting: it is consulted on every request and
// re-reading plus re-parsing it per request would cost more than the lookup it
// protects.
func (s *Server) trustedProxies(ctx context.Context) []netip.Prefix {
	s.proxyCacheMu.Lock()
	if time.Now().Before(s.proxyCacheExpires) {
		cached := s.proxyCacheValue
		s.proxyCacheMu.Unlock()
		return cached
	}
	s.proxyCacheMu.Unlock()
	prefixes, _ := parseTrustedProxies(settingOr(s, ctx, "security.trusted_proxies", ""))
	s.proxyCacheMu.Lock()
	s.proxyCacheValue, s.proxyCacheExpires = prefixes, time.Now().Add(trustedProxyCacheTTL)
	s.proxyCacheMu.Unlock()
	return prefixes
}

const trustedProxyCacheTTL = 10 * time.Second

// resolveClientIP replaces chi's middleware.RealIP, which trusts X-Forwarded-For
// on every request. It publishes the resolved address on the request context so
// handlers do not each repeat the lookup.
func (s *Server) resolveClientIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := resolveClientAddr(r.RemoteAddr, r.Header, s.trustedProxies(r.Context()))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
	})
}

// clientIP reports the address the request is attributed to. Handlers invoked
// outside the router still get the peer address rather than an empty string.
func clientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok && ip != "" {
		return ip
	}
	return resolveClientAddr(r.RemoteAddr, r.Header, nil)
}
