package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"path"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/hkjang/visitflow/internal/database"
	"github.com/hkjang/visitflow/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	db            *pgxpool.Pool
	keys          *platform.Keyring
	logger        *slog.Logger
	webFS         fs.FS
	version       string
	commit        string
	builtAt       string
	eventsMu      sync.RWMutex
	events        map[chan string]struct{}
	publicLimiter *rateLimiter
	metrics       *serverMetrics

	limitCacheMu      sync.Mutex
	limitCacheValue   int
	limitCacheExpires time.Time

	proxyCacheMu      sync.Mutex
	proxyCacheValue   []netip.Prefix
	proxyCacheExpires time.Time

	settingsMu    sync.RWMutex
	settingsCache map[string]cachedSetting
}

func NewServer(db *pgxpool.Pool, keys *platform.Keyring, logger *slog.Logger, webFS fs.FS, version, commit, builtAt string) *Server {
	return &Server{
		db: db, keys: keys, logger: logger, webFS: webFS,
		version: version, commit: commit, builtAt: builtAt,
		events:        make(map[chan string]struct{}),
		publicLimiter: newRateLimiter(time.Minute),
		metrics:       newServerMetrics(),
	}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, s.resolveClientIP, s.recoverer, s.securityHeaders, s.accessLog,
		middleware.Compress(5, "application/json", "text/html", "text/css", "text/plain", "text/csv", "application/javascript", "text/javascript", "image/svg+xml", "application/manifest+json"))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", s.ready)
	r.Get("/metrics", s.prometheusMetrics)
	r.With(s.publicRateLimit("qr-image")).Get("/img/visitor/{qrcode_file_seq}.jpg", s.publicVisitorQRJPEG)
	r.With(s.publicRateLimit("qr-image")).Head("/img/visitor/{qrcode_file_seq}.jpg", s.publicVisitorQRJPEG)
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/version", s.versionInfo)
		r.Get("/auth/config", s.authConfig)
		r.With(s.publicRateLimit("login")).Post("/auth/login", s.localLogin)
		r.With(s.publicRateLimit("password-reset")).Post("/auth/password-reset/request", s.requestPasswordReset)
		r.With(s.publicRateLimit("password-reset")).Get("/auth/password-reset/{token}", s.checkPasswordReset)
		r.With(s.publicRateLimit("password-reset")).Post("/auth/password-reset/{token}", s.completePasswordReset)
		r.Get("/auth/oidc/start", s.oidcStart)
		r.Get("/auth/oidc/callback", s.oidcCallback)
		r.Get("/openapi.json", s.openAPI)
		r.Group(func(r chi.Router) {
			r.Use(s.publicRateLimit("pass"))
			r.Get("/public/passes/{token}", s.publicPass)
			r.Get("/public/passes/{token}/qr.png", s.publicPassQR)
		})
		r.Group(func(r chi.Router) {
			r.Use(s.publicRateLimit("registration"))
			r.Get("/public/registrations/{token}", s.publicRegistration)
			r.Post("/public/registrations/{token}", s.submitPublicRegistration)
		})
		r.With(s.publicRateLimit("kiosk")).Post("/kiosk/enroll", s.enrollKiosk)
		// Lobby endpoints authenticate with either a staff session or a kiosk
		// device cookie, so an unattended tablet never needs a person's login.
		r.Group(func(r chi.Router) {
			r.Use(s.authenticateLobby, s.requireLobby)
			r.Get("/lobby/today", s.lobbyToday)
			r.Get("/lobby/current", s.lobbyCurrent)
			r.Get("/lobby/roster", s.emergencyRoster)
			r.Get("/lobby/stream", s.lobbyStream)
			r.Post("/lobby/walk-ins", s.createWalkIn)
			r.Post("/qr/verify", s.verifyQR)
			r.Post("/checkins", s.checkIn)
			r.Post("/checkins/manual", s.manualCheckIn)
			r.Post("/checkouts", s.checkOut)
		})
		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Get("/auth/me", s.me)
			r.Post("/auth/logout", s.logout)
			r.Post("/auth/password", s.changePassword)
			r.Patch("/profile", s.updateProfile)
			r.Get("/profile/notifications", s.getMailPreferences)
			r.Put("/profile/notifications", s.updateMailPreferences)
			r.Get("/reference-data", s.referenceData)
			r.Get("/dashboard", s.personalDashboard)
			r.Get("/visits", s.listVisits)
			r.Post("/visits", s.createVisit)
			r.Post("/visits/import/preview", s.previewVisitorImport)
			r.Get("/visits/{visitID}", s.getVisit)
			r.Put("/visits/{visitID}", s.updateVisit)
			r.Post("/visits/{visitID}/cancel", s.cancelVisit)
			r.Post("/visits/{visitID}/cancel-series", s.cancelSeries)
			r.Post("/visitor-visits/{visitorVisitID}/cancel", s.cancelParticipant)
			r.Post("/visits/{visitID}/approve", s.approveVisit)
			r.Post("/visits/{visitID}/reject", s.rejectVisit)
			r.Post("/visits/{visitID}/notifications/resend", s.resendVisitNotification)
			r.Post("/visitor-visits/{visitorVisitID}/qr/reissue", s.reissueQR)
			r.Post("/visitor-visits/{visitorVisitID}/invitation", s.createRegistrationInvitation)
			r.Delete("/visitor-visits/{visitorVisitID}/invitation", s.revokeRegistrationInvitation)
			r.Get("/visit-templates", s.listVisitTemplates)
			r.Post("/visit-templates", s.createVisitTemplate)
			r.Get("/visit-templates/{templateID}", s.getVisitTemplate)
			r.Put("/visit-templates/{templateID}", s.updateVisitTemplate)
			r.Delete("/visit-templates/{templateID}", s.deleteVisitTemplate)
			r.Get("/frequent-visitors", s.listFrequentVisitors)
			r.Post("/frequent-visitors", s.createFrequentVisitor)
			r.Put("/frequent-visitors/{frequentVisitorID}", s.updateFrequentVisitor)
			r.Delete("/frequent-visitors/{frequentVisitorID}", s.deleteFrequentVisitor)
			r.Get("/guides", s.listPublishedGuides)
			r.Get("/guides/{guideID}", s.getPublishedGuide)
			r.Get("/api-keys", s.listAPIKeys)
			r.Get("/api-key-policy", s.apiKeyPolicy)
			r.Post("/api-keys", s.createAPIKey)
			r.Patch("/api-keys/{keyID}", s.updateAPIKey)
			r.Post("/api-keys/{keyID}/rotate", s.rotateAPIKey)
			r.Delete("/api-keys/{keyID}", s.revokeAPIKey)
			r.Group(func(r chi.Router) {
				r.Use(s.requireAudit)
				r.Get("/admin/audit-logs", s.auditLogs)
				r.Get("/admin/audit-logs.csv", s.exportAuditLogsCSV)
			})
			r.Group(func(r chi.Router) {
				r.Use(s.requireSecurity)
				r.Get("/admin/visitors", s.listVisitors)
				r.Get("/admin/visitors/{visitorID}", s.visitorDetail)
				r.Post("/admin/visitors/{visitorID}/erase", s.eraseVisitor)
				r.Get("/admin/visits.csv", s.exportVisitsCSV)
				r.Get("/admin/watchlist", s.listWatchlist)
				r.Post("/admin/watchlist", s.createWatchlist)
				r.Delete("/admin/watchlist/{entryID}", s.deleteWatchlist)
			})
			r.Group(func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/admin/dashboard", s.adminDashboard)
				r.Get("/admin/metrics", s.adminMetrics)
				r.Get("/admin/statistics", s.statistics)
				r.Get("/admin/statistics.csv", s.exportStatisticsCSV)
				r.Get("/admin/notifications", s.listNotifications)
				r.Post("/admin/notifications/retry-failed", s.retryFailedNotifications)
				r.Post("/admin/notifications/{notificationID}/retry", s.retryNotification)
				r.Post("/admin/notifications/{notificationID}/cancel", s.cancelNotification)
				r.Get("/admin/notification-apis", s.listNotificationAPIs)
				r.Post("/admin/notification-apis", s.createNotificationAPI)
				r.Put("/admin/notification-apis/{apiID}", s.updateNotificationAPI)
				r.Post("/admin/notification-apis/{apiID}/test", s.testNotificationAPI)
				r.Delete("/admin/notification-apis/{apiID}", s.deleteNotificationAPI)
				r.Get("/admin/notification-rules", s.listNotificationRules)
				r.Post("/admin/notification-rules", s.createNotificationRule)
				r.Put("/admin/notification-rules/{ruleID}", s.updateNotificationRule)
				r.Delete("/admin/notification-rules/{ruleID}", s.deleteNotificationRule)
				r.Get("/admin/users", s.listUsers)
				r.Post("/admin/users", s.createUser)
				r.Patch("/admin/users/{userID}", s.updateUser)
				r.Post("/admin/users/{userID}/password-reset", s.resetUserPassword)
				r.Post("/admin/users/{userID}/sessions/revoke", s.revokeUserSessions)
				r.Post("/admin/sites", s.upsertSite)
				r.Post("/admin/lobbies", s.upsertLobby)
				r.Post("/admin/organizations", s.upsertDepartment)
				r.Get("/admin/visit-types", s.listVisitTypes)
				r.Post("/admin/visit-types", s.upsertVisitType)
				r.Put("/admin/visit-types/{visitTypeID}", s.upsertVisitType)
				r.Delete("/admin/visit-types/{visitTypeID}", s.deleteVisitType)
				r.Get("/admin/kiosk-devices", s.listKioskDevices)
				r.Post("/admin/kiosk-devices", s.createKioskDevice)
				r.Delete("/admin/kiosk-devices/{deviceID}", s.revokeKioskDevice)
				r.Get("/admin/guides", s.listAdminGuides)
				r.Post("/admin/guides", s.createGuide)
				r.Put("/admin/guides/{guideID}", s.updateGuide)
				r.Delete("/admin/guides/{guideID}", s.deleteGuide)
				r.Get("/settings", s.listSettings)
				r.Get("/settings/export", s.exportSettings)
				r.Put("/settings", s.updateSettings)
				r.Post("/settings/oidc/test", s.testOIDC)
				r.Post("/settings/smtp/test", s.testSMTP)
			})
		})
	})
	r.With(s.authenticate).Post("/mcp", s.mcp)
	r.Handle("/*", s.spaHandler())
	return r
}

type cspNonceKey struct{}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonceBytes := make([]byte, 16)
		if _, err := rand.Read(nonceBytes); err != nil {
			s.logger.Error("csp nonce generation failed", "error", err)
		}
		nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy(nonce))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), cspNonceKey{}, nonce)))
	})
}

// contentSecurityPolicy nonces the stylesheets Material UI injects at runtime
// instead of allowing every inline style. Style attributes stay allowed because
// MUI transitions and layout primitives set them per element.
func contentSecurityPolicy(nonce string) string {
	styleElem := "'self'"
	if nonce != "" {
		styleElem = "'self' 'nonce-" + nonce + "'"
	}
	return "default-src 'self'; img-src 'self' data: blob:; style-src " + styleElem +
		"; style-src-elem " + styleElem + "; style-src-attr 'unsafe-inline'; script-src 'self'; connect-src 'self'; " +
		"worker-src 'self'; manifest-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
}

// fallbackContentSecurityPolicy is used when the nonce could not be published
// to the page. Both style directives must allow inline sources here, because
// style-src-elem overrides style-src for <style> elements.
func fallbackContentSecurityPolicy() string {
	policy := contentSecurityPolicy("")
	policy = strings.Replace(policy, "style-src 'self';", "style-src 'self' 'unsafe-inline';", 1)
	return strings.Replace(policy, "style-src-elem 'self';", "style-src-elem 'self' 'unsafe-inline';", 1)
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		s.metrics.observeStatus(status)
		level := slog.LevelInfo
		if strings.HasPrefix(r.URL.Path, "/assets/") && status < 400 {
			level = slog.LevelDebug
		}
		s.logger.Log(r.Context(), level, "request", "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.logger.Error("panic", "error", v, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal_error", "요청을 처리하지 못했습니다")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_unavailable", "데이터베이스 연결을 확인하세요")
		return
	}
	applied, err := database.SchemaVersion(ctx, s.db)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "스키마 버전을 확인하지 못했습니다")
		return
	}
	expected := database.ExpectedSchemaVersion()
	status := "ready"
	code := http.StatusOK
	if applied < expected {
		status, code = "migration_pending", http.StatusServiceUnavailable
	}
	var backlog, oldestSeconds int
	_ = s.db.QueryRow(ctx, `SELECT count(*),COALESCE(EXTRACT(EPOCH FROM now()-min(next_attempt_at))::int,0)
		FROM notifications WHERE status IN ('queued','failed') AND next_attempt_at<=now()`).Scan(&backlog, &oldestSeconds)
	writeJSON(w, code, map[string]any{
		"status": status, "schemaVersion": applied, "expectedSchemaVersion": expected,
		"encryptionKey": "verified", "notificationBacklog": backlog, "notificationOldestSeconds": oldestSeconds,
	})
}

func (s *Server) versionInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"name": "VisitFlow", "version": s.version, "commit": s.commit, "builtAt": s.builtAt})
}

func init() {
	// Go's built-in table has no entry for .webmanifest, and a PWA manifest
	// served as text/plain is ignored by browsers.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

func (s *Server) spaHandler() http.Handler {
	assets := http.FileServer(http.FS(s.webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/img/") || r.URL.Path == "/mcp" || r.URL.Path == "/metrics" {
			http.NotFound(w, r)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "." {
			if f, err := s.webFS.Open(p); err == nil {
				_ = f.Close()
				if strings.HasPrefix(p, "assets/") {
					// Vite names these by content hash, so they never change in place.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else if p == "sw.js" {
					w.Header().Set("Cache-Control", "no-cache")
				}
				assets.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(s.webFS, "index.html")
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "ui_unavailable", "UI 빌드가 포함되지 않았습니다")
			return
		}
		nonce, _ := r.Context().Value(cspNonceKey{}).(string)
		document, injected := injectCSPNonce(string(b), nonce)
		if !injected {
			// Without the meta tag the UI cannot nonce its runtime stylesheets,
			// so fall back rather than serving an unstyled page.
			w.Header().Set("Content-Security-Policy", fallbackContentSecurityPolicy())
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(document))
	})
}

// injectCSPNonce publishes the request nonce to the SPA, which hands it to the
// Material UI style cache so every injected <style> carries it.
func injectCSPNonce(document, nonce string) (string, bool) {
	if nonce == "" {
		return document, false
	}
	index := strings.Index(strings.ToLower(document), "<head>")
	if index < 0 {
		return document, false
	}
	insertAt := index + len("<head>")
	meta := `<meta property="csp-nonce" content="` + nonce + `">`
	return document[:insertAt] + meta + document[insertAt:], true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "요청 형식을 확인하세요")
		return false
	}
	return true
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
}

func userFrom(r *http.Request) (User, bool) {
	u, ok := r.Context().Value(userContextKey).(User)
	return u, ok
}

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.EmployeeID, &u.Role, &u.Source, &u.LastLoginAt)
	return u, err
}

func (s *Server) audit(ctx context.Context, userID, action, resourceType, resourceID, ip string, details any) {
	b, _ := json.Marshal(details)
	_, err := s.db.Exec(ctx, `INSERT INTO audit_logs(actor_user_id, action, resource_type, resource_id, ip_address, details) VALUES(NULLIF($1,''),$2,$3,NULLIF($4,''),$5,$6)`, userID, action, resourceType, resourceID, ip, b)
	if err != nil {
		s.logger.Error("audit log failed", "error", err, "action", action)
	}
}

func notFoundOrServer(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "대상을 찾을 수 없습니다")
		return
	}
	writeError(w, http.StatusInternalServerError, "database_error", "데이터를 처리하지 못했습니다")
}
