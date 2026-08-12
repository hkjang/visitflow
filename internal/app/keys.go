package app

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hkjang/visitflow/internal/platform"
)

func (s *Server) apiKeyPolicy(w http.ResponseWriter, r *http.Request) {
	allowed, _ := s.getSetting(r.Context(), "security.api_key_allowed_scopes")
	days, _ := strconv.Atoi(settingOr(s, r.Context(), "security.api_key_days", "90"))
	maxActive, _ := strconv.Atoi(settingOr(s, r.Context(), "security.api_key_max_active", "10"))
	writeJSON(w, http.StatusOK, map[string]any{"allowedScopes": strings.Fields(allowed), "defaultExpiryDays": days, "maxActiveKeys": maxActive})
}

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	rows, err := s.db.Query(r.Context(), `SELECT id,name,prefix,scopes,version,rotated_from,created_at,expires_at,last_used_at,revoked_at,grace_until FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC`, u.ID)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, prefix string
		var scopes []string
		var version int
		var rotated *string
		var created time.Time
		var expires, lastUsed, revoked, grace *time.Time
		if rows.Scan(&id, &name, &prefix, &scopes, &version, &rotated, &created, &expires, &lastUsed, &revoked, &grace) == nil {
			items = append(items, map[string]any{"id": id, "name": name, "prefix": prefix, "scopes": scopes, "version": version, "rotatedFrom": rotated, "createdAt": created, "expiresAt": expires, "lastUsedAt": lastUsed, "revokedAt": revoked, "graceUntil": grace})
		}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name          string   `json:"name"`
		Scopes        []string `json:"scopes"`
		ExpiresInDays *int     `json:"expiresInDays"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Name == "" {
		writeError(w, 400, "name_required", "키 이름은 필수입니다")
		return
	}
	if len(in.Scopes) == 0 {
		in.Scopes = []string{"read", "mcp"}
	}
	if !s.validScopes(r, in.Scopes) {
		writeError(w, 400, "invalid_scopes", "관리자가 허용한 키 범위만 선택할 수 있습니다")
		return
	}
	u, _ := userFrom(r)
	maxActive := 10
	if v, _ := s.getSetting(r.Context(), "security.api_key_max_active"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 1 && n <= 100 {
			maxActive = n
		}
	}
	var active int
	if err := s.db.QueryRow(r.Context(), `SELECT count(*) FROM api_keys WHERE user_id=$1 AND (revoked_at IS NULL OR grace_until>now()) AND (expires_at IS NULL OR expires_at>now())`, u.ID).Scan(&active); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if active >= maxActive {
		writeError(w, http.StatusConflict, "api_key_limit", "활성 개인 키 한도에 도달했습니다")
		return
	}
	days := 90
	if v, _ := s.getSetting(r.Context(), "security.api_key_days"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			days = n
		}
	}
	if in.ExpiresInDays != nil {
		days = *in.ExpiresInDays
	}
	if days < 1 || days > 3650 {
		writeError(w, 400, "invalid_expiry", "유효기간은 1~3650일입니다")
		return
	}
	rawPart, err := platform.RandomToken(32)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	raw := "vf_" + rawPart
	prefix := raw[:13]
	id := newID()
	expires := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	_, err = s.db.Exec(r.Context(), `INSERT INTO api_keys(id,user_id,name,prefix,secret_hash,scopes,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, u.ID, in.Name, prefix, s.keys.Digest(raw), in.Scopes, expires)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "api_key.create", "api_key", id, r.RemoteAddr, map[string]any{"name": in.Name, "scopes": in.Scopes})
	writeJSON(w, 201, map[string]any{"id": id, "key": raw, "prefix": prefix, "expiresAt": expires, "message": "이 키는 다시 표시되지 않습니다. 안전한 곳에 보관하세요."})
}

func (s *Server) updateAPIKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || !s.validScopes(r, in.Scopes) {
		writeError(w, http.StatusBadRequest, "invalid_key_policy", "키 이름과 관리자가 허용한 범위를 확인하세요")
		return
	}
	u, _ := userFrom(r)
	id := chi.URLParam(r, "keyID")
	var beforeName string
	var beforeScopes []string
	if err := s.db.QueryRow(r.Context(), `SELECT name,scopes FROM api_keys WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, id, u.ID).Scan(&beforeName, &beforeScopes); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err := s.db.Exec(r.Context(), `UPDATE api_keys SET name=$3,scopes=$4 WHERE id=$1 AND user_id=$2`, id, u.ID, in.Name, in.Scopes); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "api_key.policy_update", "api_key", id, r.RemoteAddr, map[string]any{
		"before": map[string]any{"name": beforeName, "scopes": beforeScopes},
		"after":  map[string]any{"name": in.Name, "scopes": in.Scopes},
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rotateAPIKey(w http.ResponseWriter, r *http.Request) {
	oldID := chi.URLParam(r, "keyID")
	u, _ := userFrom(r)
	var name string
	var scopes []string
	var version int
	err := s.db.QueryRow(r.Context(), `SELECT name,scopes,version FROM api_keys WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, oldID, u.ID).Scan(&name, &scopes, &version)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	graceHours := 24
	if v, _ := s.getSetting(r.Context(), "security.rotation_grace_hours"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 0 && n <= 168 {
			graceHours = n
		}
	}
	expiryDays := 90
	if v, _ := s.getSetting(r.Context(), "security.api_key_days"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 1 && n <= 3650 {
			expiryDays = n
		}
	}
	expires := time.Now().Add(time.Duration(expiryDays) * 24 * time.Hour)
	rawPart, _ := platform.RandomToken(32)
	raw := "vf_" + rawPart
	prefix := raw[:13]
	newIDValue := newID()
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	graceUntil := time.Now().Add(time.Duration(graceHours) * time.Hour)
	_, err = tx.Exec(r.Context(), `UPDATE api_keys SET revoked_at=now(),grace_until=$3 WHERE id=$1 AND user_id=$2`, oldID, u.ID, graceUntil)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO api_keys(id,user_id,name,prefix,secret_hash,scopes,version,rotated_from,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, newIDValue, u.ID, name, prefix, s.keys.Digest(raw), scopes, version+1, oldID, expires)
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "api_key.rotate", "api_key", newIDValue, r.RemoteAddr, map[string]any{"rotatedFrom": oldID, "graceUntil": graceUntil})
	writeJSON(w, 201, map[string]any{"id": newIDValue, "key": raw, "prefix": prefix, "version": version + 1, "oldKeyGraceUntil": graceUntil, "message": "새 키는 다시 표시되지 않습니다. 기존 키는 유예기간 후 만료됩니다."})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "keyID")
	u, _ := userFrom(r)
	tag, err := s.db.Exec(r.Context(), `UPDATE api_keys SET revoked_at=now(),grace_until=NULL WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, u.ID)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "not_found", "활성 API 키가 없습니다")
		return
	}
	s.audit(r.Context(), u.ID, "api_key.revoke", "api_key", id, r.RemoteAddr, nil)
	w.WriteHeader(204)
}

func (s *Server) validScopes(r *http.Request, scopes []string) bool {
	if len(scopes) == 0 || len(scopes) > 3 {
		return false
	}
	allowedValue, _ := s.getSetting(r.Context(), "security.api_key_allowed_scopes")
	if strings.TrimSpace(allowedValue) == "" {
		allowedValue = "read write mcp"
	}
	allowed := map[string]bool{}
	for _, scope := range strings.Fields(allowedValue) {
		allowed[scope] = true
	}
	seen := map[string]bool{}
	for _, v := range scopes {
		if !allowed[v] || (v != "read" && v != "write" && v != "mcp") {
			return false
		}
		if seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
