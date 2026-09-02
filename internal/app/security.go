package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// encryptionKeyCanary is stored encrypted in settings the first time a database
// is opened with a valid key. Every later start decrypts it, so a wrong or
// rotated ENCRYPTION_KEY fails at boot instead of surfacing later as scattered
// per-record decryption failures.
const encryptionKeyCanary = "visitflow-encryption-key-canary-v1"

var errEncryptionKeyMismatch = errors.New("ENCRYPTION_KEY does not match the key this database was encrypted with; restore the original key or the encrypted data cannot be read")

// EnsureEncryptionKey verifies the configured key against the database before
// the service accepts traffic. On a database that predates the canary the key is
// checked against an existing ciphertext sample instead, so an upgrade started
// with the wrong key is still rejected.
func (s *Server) EnsureEncryptionKey(ctx context.Context) error {
	var stored string
	err := s.db.QueryRow(ctx, `SELECT value FROM settings WHERE key='security.key_check'`).Scan(&stored)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if stored != "" {
		plain, decryptErr := s.keys.Decrypt(stored)
		if decryptErr != nil || plain != encryptionKeyCanary {
			return errEncryptionKeyMismatch
		}
		return nil
	}
	if err := s.verifyKeyAgainstExistingData(ctx); err != nil {
		return err
	}
	encrypted, err := s.keys.Encrypt(encryptionKeyCanary)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO settings(key,value,secret) VALUES('security.key_check',$1,true)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value,updated_at=now() WHERE settings.value=''`, encrypted)
	return err
}

// verifyKeyAgainstExistingData decrypts one existing ciphertext, if the database
// already holds any. An empty database returns nil: there is nothing to protect.
func (s *Server) verifyKeyAgainstExistingData(ctx context.Context) error {
	samples := []string{
		`SELECT value FROM settings WHERE secret AND value<>'' AND key<>'security.key_check' LIMIT 1`,
		`SELECT name_encrypted FROM visitors WHERE erased_at IS NULL LIMIT 1`,
		`SELECT token_encrypted FROM qr_tokens LIMIT 1`,
		`SELECT recipient_encrypted FROM notifications LIMIT 1`,
	}
	for _, query := range samples {
		var sample string
		err := s.db.QueryRow(ctx, query).Scan(&sample)
		if errors.Is(err, pgx.ErrNoRows) || sample == "" {
			continue
		}
		if err != nil {
			return err
		}
		if _, decryptErr := s.keys.Decrypt(sample); decryptErr != nil {
			return errEncryptionKeyMismatch
		}
		return nil
	}
	return nil
}

func (s *Server) throttlePolicy(ctx context.Context) (maxAttempts, lockoutMinutes int) {
	maxAttempts, _ = strconv.Atoi(settingOr(s, ctx, "security.login_max_attempts", "5"))
	lockoutMinutes, _ = strconv.Atoi(settingOr(s, ctx, "security.login_lockout_minutes", "15"))
	if maxAttempts < 1 || maxAttempts > 100 {
		maxAttempts = 5
	}
	if lockoutMinutes < 1 || lockoutMinutes > 1440 {
		lockoutMinutes = 15
	}
	return maxAttempts, lockoutMinutes
}

// loginLock reports the remaining lockout for any of the supplied throttle keys.
func (s *Server) loginLock(ctx context.Context, keys []string) time.Duration {
	if len(keys) == 0 {
		return 0
	}
	var lockedUntil time.Time
	err := s.db.QueryRow(ctx, `SELECT locked_until FROM auth_throttle WHERE key=ANY($1::text[]) AND locked_until>now() ORDER BY locked_until DESC LIMIT 1`, keys).Scan(&lockedUntil)
	if err != nil {
		return 0
	}
	remaining := time.Until(lockedUntil)
	if remaining < time.Second {
		return time.Second
	}
	return remaining
}

// ipLockoutMultiplier widens the per-address threshold. Many users share one
// egress address behind NAT or a proxy, so the address counter only catches
// spraying attacks while the account counter protects individual logins.
const ipLockoutMultiplier = 10

func (s *Server) recordLoginFailure(ctx context.Context, keys []string) {
	maxAttempts, lockoutMinutes := s.throttlePolicy(ctx)
	for _, key := range keys {
		threshold := maxAttempts
		if strings.HasPrefix(key, "ip:") {
			threshold = maxAttempts * ipLockoutMultiplier
		}
		_, err := s.db.Exec(ctx, `INSERT INTO auth_throttle(key,failures,first_failure_at,last_failure_at)
			VALUES($1,1,now(),now())
			ON CONFLICT (key) DO UPDATE SET
				failures=CASE WHEN auth_throttle.last_failure_at<now()-($2::int*interval '1 minute') THEN 1 ELSE auth_throttle.failures+1 END,
				first_failure_at=CASE WHEN auth_throttle.last_failure_at<now()-($2::int*interval '1 minute') THEN now() ELSE auth_throttle.first_failure_at END,
				last_failure_at=now(),
				locked_until=CASE
					WHEN (CASE WHEN auth_throttle.last_failure_at<now()-($2::int*interval '1 minute') THEN 1 ELSE auth_throttle.failures+1 END)>=$3
					THEN now()+($2::int*interval '1 minute') ELSE auth_throttle.locked_until END`, key, lockoutMinutes, threshold)
		if err != nil {
			s.logger.Error("login throttle update failed", "error", err)
		}
	}
}

func (s *Server) clearLoginFailures(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM auth_throttle WHERE key=ANY($1::text[])`, keys); err != nil {
		s.logger.Error("login throttle reset failed", "error", err)
	}
}

// rateLimiter is a fixed-window in-memory counter for unauthenticated endpoints.
// It protects the public pass, QR image and QR verification routes from token
// enumeration without adding a database round trip per request.
type rateLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	entries map[string]*rateEntry
}

type rateEntry struct {
	count      int
	windowEnds time.Time
}

func newRateLimiter(window time.Duration) *rateLimiter {
	return &rateLimiter{window: window, entries: map[string]*rateEntry{}}
}

// allow consumes one request for key and reports whether it stays within limit.
func (l *rateLimiter) allow(key string, limit int, now time.Time) (bool, time.Duration) {
	if limit <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[key]
	if !ok || now.After(entry.windowEnds) {
		entry = &rateEntry{windowEnds: now.Add(l.window)}
		l.entries[key] = entry
	}
	entry.count++
	if entry.count > limit {
		return false, time.Until(entry.windowEnds)
	}
	return true, 0
}

func (l *rateLimiter) cleanup(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, entry := range l.entries {
		if now.After(entry.windowEnds) {
			delete(l.entries, key)
		}
	}
}

// publicRateLimitPerMinute caches the configured limit briefly: the value is
// read on every unauthenticated request, and a database round trip per request
// would defeat the purpose of a cheap in-memory limiter.
func (s *Server) publicRateLimitPerMinute(ctx context.Context) int {
	s.limitCacheMu.Lock()
	if time.Now().Before(s.limitCacheExpires) {
		limit := s.limitCacheValue
		s.limitCacheMu.Unlock()
		return limit
	}
	s.limitCacheMu.Unlock()
	limit, _ := strconv.Atoi(settingOr(s, ctx, "security.public_rate_limit_per_minute", "60"))
	if limit < 1 || limit > 100000 {
		limit = 60
	}
	s.limitCacheMu.Lock()
	s.limitCacheValue, s.limitCacheExpires = limit, time.Now().Add(10*time.Second)
	s.limitCacheMu.Unlock()
	return limit
}

func (s *Server) publicRateLimit(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := s.publicRateLimitPerMinute(r.Context())
			allowed, retryAfter := s.publicLimiter.allow(scope+"|"+clientIP(r), limit, time.Now())
			if !allowed {
				s.metrics.rateLimited.Add(1)
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
				writeError(w, http.StatusTooManyRequests, "rate_limited", "요청이 너무 많습니다. 잠시 후 다시 시도하세요")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
