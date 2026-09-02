package app

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hkjang/visitflow/internal/platform"
	"github.com/jackc/pgx/v5"
)

const (
	kioskCookie      = "visitflow_kiosk"
	kioskCSRFCookie  = "visitflow_kiosk_csrf"
	kioskTokenPrefix = "vfk_"
)

// kioskDevice is an unattended lobby tablet. It authenticates with a device
// token instead of a person's login and is scoped to one site, so a stolen
// tablet cannot read another site's visitors.
type kioskDevice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	SiteID  string `json:"siteId"`
	LobbyID string `json:"lobbyId,omitempty"`
}

// user projects the device onto the lobby role so the existing site-scope and
// RBAC checks apply unchanged. The empty ID keeps audit rows attributed to no
// person while the device name is recorded in the audit details.
func (d kioskDevice) user() User {
	return User{ID: "", Username: "kiosk:" + d.ID, DisplayName: d.Name, Role: RoleLobby, Source: "kiosk", SiteScope: []string{d.SiteID}}
}

func (s *Server) kioskFromRequest(r *http.Request) (kioskDevice, bool) {
	cookie, err := r.Cookie(kioskCookie)
	if err != nil || !strings.HasPrefix(cookie.Value, kioskTokenPrefix) {
		return kioskDevice{}, false
	}
	device, err := s.lookupKioskDevice(r, cookie.Value)
	if err != nil {
		return kioskDevice{}, false
	}
	return device, true
}

func (s *Server) lookupKioskDevice(r *http.Request, token string) (kioskDevice, error) {
	var device kioskDevice
	var lobbyID *string
	err := s.db.QueryRow(r.Context(), `SELECT id,name,site_id,lobby_id FROM kiosk_devices
		WHERE token_hash=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, s.keys.Digest("kiosk:"+token)).
		Scan(&device.ID, &device.Name, &device.SiteID, &lobbyID)
	if err != nil {
		return kioskDevice{}, err
	}
	if lobbyID != nil {
		device.LobbyID = *lobbyID
	}
	_, _ = s.db.Exec(r.Context(), `UPDATE kiosk_devices SET last_seen_at=now() WHERE id=$1`, device.ID)
	return device, nil
}

// kioskCSRFValid enforces a double-submit cookie on state-changing kiosk calls.
// The device cookie is SameSite=Strict, and the readable companion cookie must
// be echoed in the request header.
func kioskCSRFValid(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	cookie, err := r.Cookie(kioskCSRFCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	provided := r.Header.Get("X-CSRF-Token")
	return provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(cookie.Value)) == 1
}

func kioskFrom(r *http.Request) (kioskDevice, bool) {
	device, ok := r.Context().Value(kioskContextKey).(kioskDevice)
	return device, ok
}

// enrollKiosk exchanges the one-time device token an administrator issued for
// the long-lived cookies the tablet then uses.
func (s *Server) enrollKiosk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	token := strings.TrimSpace(in.Token)
	if !strings.HasPrefix(token, kioskTokenPrefix) {
		writeError(w, http.StatusUnauthorized, "invalid_kiosk_token", "키오스크 등록 토큰이 올바르지 않습니다")
		return
	}
	device, err := s.lookupKioskDevice(r, token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_kiosk_token", "키오스크 등록 토큰이 만료되었거나 폐기되었습니다")
		return
	}
	csrf, err := platform.RandomToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kiosk_enroll_failed", "키오스크를 등록하지 못했습니다")
		return
	}
	expires := time.Now().AddDate(1, 0, 0)
	http.SetCookie(w, &http.Cookie{Name: kioskCookie, Value: token, Path: "/", HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
	http.SetCookie(w, &http.Cookie{Name: kioskCSRFCookie, Value: csrf, Path: "/", HttpOnly: false, Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
	s.audit(r.Context(), "", "kiosk.enroll", "kiosk_device", device.ID, r.RemoteAddr, map[string]string{"name": device.Name})
	writeJSON(w, http.StatusOK, map[string]any{"device": device, "csrfToken": csrf})
}

func (s *Server) listKioskDevices(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT k.id,k.name,k.site_id,s.name,k.lobby_id,COALESCE(l.name,''),k.prefix,k.expires_at,k.last_seen_at,k.revoked_at,k.created_at
		FROM kiosk_devices k JOIN sites s ON s.id=k.site_id LEFT JOIN lobbies l ON l.id=k.lobby_id ORDER BY k.created_at DESC`)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, siteID, siteName, lobbyName, prefix string
		var lobbyID *string
		var expiresAt, lastSeenAt, revokedAt *time.Time
		var createdAt time.Time
		if rows.Scan(&id, &name, &siteID, &siteName, &lobbyID, &lobbyName, &prefix, &expiresAt, &lastSeenAt, &revokedAt, &createdAt) == nil {
			items = append(items, map[string]any{"id": id, "name": name, "siteId": siteID, "siteName": siteName, "lobbyId": lobbyID, "lobbyName": lobbyName,
				"prefix": prefix, "expiresAt": expiresAt, "lastSeenAt": lastSeenAt, "revokedAt": revokedAt, "createdAt": createdAt,
				"active": revokedAt == nil && (expiresAt == nil || expiresAt.After(time.Now()))})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createKioskDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string `json:"name"`
		SiteID    string `json:"siteId"`
		LobbyID   string `json:"lobbyId"`
		ValidDays int    `json:"validDays"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || in.SiteID == "" {
		writeError(w, http.StatusBadRequest, "required_fields", "기기 이름과 사업장은 필수입니다")
		return
	}
	if in.ValidDays < 0 || in.ValidDays > 3650 {
		writeError(w, http.StatusBadRequest, "invalid_validity", "유효기간은 0(무기한)~3650일이어야 합니다")
		return
	}
	if in.LobbyID != "" {
		var matches bool
		if err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM lobbies WHERE id=$1 AND site_id=$2)`, in.LobbyID, in.SiteID).Scan(&matches); err != nil || !matches {
			writeError(w, http.StatusBadRequest, "lobby_site_mismatch", "선택한 로비가 해당 사업장에 속하지 않습니다")
			return
		}
	}
	random, err := platform.RandomToken(32)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	token := kioskTokenPrefix + random
	id := newID()
	u, _ := userFrom(r)
	var expiresAt *time.Time
	if in.ValidDays > 0 {
		value := time.Now().AddDate(0, 0, in.ValidDays)
		expiresAt = &value
	}
	_, err = s.db.Exec(r.Context(), `INSERT INTO kiosk_devices(id,name,site_id,lobby_id,token_hash,prefix,created_by,expires_at)
		VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8)`, id, in.Name, in.SiteID, in.LobbyID, s.keys.Digest("kiosk:"+token), token[:12], u.ID, expiresAt)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "kiosk.create", "kiosk_device", id, r.RemoteAddr, map[string]any{"name": in.Name, "siteId": in.SiteID, "validDays": in.ValidDays})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "name": in.Name, "token": token, "expiresAt": expiresAt,
		"enrollPath": "/kiosk?token=" + token,
	})
}

func (s *Server) revokeKioskDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "deviceID")
	tag, err := s.db.Exec(r.Context(), `UPDATE kiosk_devices SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		notFoundOrServer(w, pgx.ErrNoRows)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "kiosk.revoke", "kiosk_device", id, r.RemoteAddr, nil)
	w.WriteHeader(http.StatusNoContent)
}

// emergencyRoster lists everyone currently inside, grouped by site and lobby.
// Evacuation drills need the list on paper, so the payload is deliberately small
// enough for the lobby client to cache offline.
func (s *Server) emergencyRoster(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	rows, err := s.db.Query(r.Context(), `SELECT s.name,COALESCE(l.name,''),p.name_encrypted,COALESCE(p.company,''),p.phone_encrypted,h.display_name,COALESCE(o.name,''),vv.checked_in_at,COALESCE(vv.badge_no,''),COALESCE(NULLIF(v.place_detail,''),'')
		FROM visitor_visits vv
		JOIN visits v ON v.id=vv.visit_id
		JOIN visitors p ON p.id=vv.visitor_id
		JOIN users h ON h.id=v.host_user_id
		JOIN sites s ON s.id=v.site_id
		LEFT JOIN lobbies l ON l.id=v.lobby_id
		LEFT JOIN organizations o ON o.id=v.department_id
		WHERE vv.status='CHECKED_IN' AND ($1<>'lobby' OR cardinality($2::text[])=0 OR v.site_id=ANY($2::text[]))
		ORDER BY s.name,l.name,vv.checked_in_at`, u.Role, u.SiteScope)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var site, lobby, nameEnc, company, phoneEnc, host, department, badge, place string
		var checkedIn *time.Time
		if rows.Scan(&site, &lobby, &nameEnc, &company, &phoneEnc, &host, &department, &checkedIn, &badge, &place) == nil {
			items = append(items, map[string]any{
				"site": site, "lobby": lobby, "visitor": s.decryptOptional(nameEnc), "company": company,
				"phone": s.decryptOptional(phoneEnc), "host": host, "department": department,
				"checkedInAt": checkedIn, "badgeNo": badge, "placeDetail": place,
			})
		}
	}
	if err := rows.Err(); err != nil {
		notFoundOrServer(w, err)
		return
	}
	actor := u.ID
	details := map[string]any{"count": len(items)}
	if device, ok := kioskFrom(r); ok {
		details["kioskDevice"] = device.Name
	}
	s.audit(r.Context(), actor, "roster.view", "visitor_visit", "", r.RemoteAddr, details)
	writeJSON(w, http.StatusOK, map[string]any{"generatedAt": time.Now(), "count": len(items), "items": items})
}
