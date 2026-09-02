package app

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// VisitFlow keeps language handling to a base tag ("ko", "en", "ja", "zh").
// Notification rules and visitor records store the same normalized value, so an
// administrator can add one rule per language without inventing region variants.
var knownLocales = map[string]bool{"ko": true, "en": true, "ja": true, "zh": true}

func normalizeLocale(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if index := strings.IndexAny(value, "-_"); index > 0 {
		value = value[:index]
	}
	if !knownLocales[value] {
		return ""
	}
	return value
}

func (s *Server) supportedLocales(ctx context.Context) []string {
	configured := strings.Fields(settingOr(s, ctx, "general.supported_locales", "ko en"))
	seen := map[string]bool{}
	locales := []string{}
	for _, item := range configured {
		normalized := normalizeLocale(item)
		if normalized != "" && !seen[normalized] {
			seen[normalized] = true
			locales = append(locales, normalized)
		}
	}
	if len(locales) == 0 {
		locales = []string{"ko"}
	}
	return locales
}

func (s *Server) defaultLocale(ctx context.Context) string {
	locale := normalizeLocale(settingOr(s, ctx, "general.default_locale", "ko-KR"))
	if locale == "" {
		locale = "ko"
	}
	return locale
}

// negotiateLocale resolves the language for a public page from, in order: an
// explicit query value, the visitor's stored preference, the Accept-Language
// header, and finally the configured default.
func (s *Server) negotiateLocale(ctx context.Context, requested, stored, acceptLanguage string) string {
	supported := s.supportedLocales(ctx)
	allowed := map[string]bool{}
	for _, locale := range supported {
		allowed[locale] = true
	}
	for _, candidate := range []string{requested, stored} {
		if normalized := normalizeLocale(candidate); normalized != "" && allowed[normalized] {
			return normalized
		}
	}
	if best := bestAcceptLanguage(acceptLanguage, allowed); best != "" {
		return best
	}
	if fallback := s.defaultLocale(ctx); allowed[fallback] {
		return fallback
	}
	return supported[0]
}

type localeWeight struct {
	locale string
	weight float64
	order  int
}

func bestAcceptLanguage(header string, allowed map[string]bool) string {
	candidates := []localeWeight{}
	for index, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		locale := normalizeLocale(fields[0])
		if locale == "" || !allowed[locale] {
			continue
		}
		weight := 1.0
		for _, field := range fields[1:] {
			field = strings.TrimSpace(field)
			if strings.HasPrefix(field, "q=") {
				if parsed, err := strconv.ParseFloat(strings.TrimPrefix(field, "q="), 64); err == nil {
					weight = parsed
				}
			}
		}
		candidates = append(candidates, localeWeight{locale: locale, weight: weight, order: index})
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(a, b int) bool {
		if candidates[a].weight != candidates[b].weight {
			return candidates[a].weight > candidates[b].weight
		}
		return candidates[a].order < candidates[b].order
	})
	return candidates[0].locale
}
