package app

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hkjang/visitflow/internal/database"
	"github.com/hkjang/visitflow/internal/platform"
	"golang.org/x/crypto/bcrypt"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._@-]{3,64}$`)

var assignableRoles = map[string]bool{RoleUser: true, RoleLobby: true, RoleDeptManager: true, RoleSecurity: true, RoleAuditor: true, RoleAdmin: true, RoleSuperAdmin: true}

// temporaryPassword is shown to the administrator once; the account holder is
// forced to replace it at first login.
func temporaryPassword() (string, error) {
	random, err := platform.RandomToken(12)
	if err != nil {
		return "", err
	}
	return "Vf-" + strings.NewReplacer("-", "x", "_", "y").Replace(random), nil
}

// createUser adds a local account for deployments without Keycloak. Roles above
// the caller's own level are refused the same way updateUser refuses them.
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username     string   `json:"username"`
		DisplayName  string   `json:"displayName"`
		Email        string   `json:"email"`
		Role         string   `json:"role"`
		DepartmentID string   `json:"departmentId"`
		SiteScope    []string `json:"siteScope"`
		Password     string   `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Email = strings.TrimSpace(in.Email)
	if in.Role == "" {
		in.Role = RoleUser
	}
	if !usernamePattern.MatchString(in.Username) {
		writeError(w, http.StatusBadRequest, "invalid_username", "아이디는 3~64자의 영문, 숫자, . _ @ - 만 사용할 수 있습니다")
		return
	}
	if in.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name_required", "표시 이름은 필수입니다")
		return
	}
	if !assignableRoles[in.Role] {
		writeError(w, http.StatusBadRequest, "invalid_role", "권한 값이 올바르지 않습니다")
		return
	}
	actor, _ := userFrom(r)
	if in.Role == RoleSuperAdmin && actor.Role != RoleSuperAdmin {
		writeError(w, http.StatusForbidden, "super_admin_required", "최고 관리자 계정은 최고 관리자만 만들 수 있습니다")
		return
	}
	password := in.Password
	generated := false
	if password == "" {
		var err error
		if password, err = temporaryPassword(); err != nil {
			notFoundOrServer(w, err)
			return
		}
		generated = true
	}
	if len(password) < 12 {
		writeError(w, http.StatusBadRequest, "weak_password", "비밀번호는 12자 이상이어야 합니다")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if in.SiteScope == nil {
		in.SiteScope = []string{}
	}
	id := newID()
	_, err = s.db.Exec(r.Context(), `INSERT INTO users(id,username,password_hash,display_name,email,role,source,department_id,site_scope,role_override,must_change_password)
		VALUES($1,$2,$3,$4,NULLIF($5,''),$6,'local',NULLIF($7,''),$8,true,true)`,
		id, in.Username, string(hash), in.DisplayName, in.Email, in.Role, in.DepartmentID, in.SiteScope)
	if database.IsConstraint(err, "users_username_key") {
		writeError(w, http.StatusConflict, "duplicate_username", "이미 사용 중인 아이디입니다")
		return
	}
	if database.IsConstraint(err, "users_department_id_fkey") {
		writeError(w, http.StatusBadRequest, "unknown_department", "부서를 찾을 수 없습니다")
		return
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), actor.ID, "user.create", "user", id, clientIP(r), map[string]any{"username": in.Username, "role": in.Role, "generatedPassword": generated})
	response := map[string]any{"id": id, "username": in.Username, "mustChangePassword": true}
	if generated {
		response["temporaryPassword"] = password
	}
	writeJSON(w, http.StatusCreated, response)
}

// resetUserPassword issues a temporary password, ends every session of the
// account and forces a change at the next login.
func (s *Server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	actor, _ := userFrom(r)
	var in struct {
		ViaEmail bool `json:"viaEmail"`
	}
	if r.ContentLength > 0 && !decodeJSON(w, r, &in) {
		return
	}
	var role, source, email string
	if err := s.db.QueryRow(r.Context(), `SELECT role,source,COALESCE(email,'') FROM users WHERE id=$1`, id).Scan(&role, &source, &email); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if in.ViaEmail {
		// The user sets their own password from a mailed link; nobody sees a
		// temporary secret.
		if source != "local" {
			writeError(w, http.StatusBadRequest, "oidc_user", "SSO 사용자의 비밀번호는 Keycloak에서 초기화하세요")
			return
		}
		if role == RoleSuperAdmin && actor.Role != RoleSuperAdmin {
			writeError(w, http.StatusForbidden, "super_admin_required", "최고 관리자 계정은 최고 관리자만 초기화할 수 있습니다")
			return
		}
		if _, enabled := s.smtpConfig(r.Context()); !enabled {
			writeError(w, http.StatusConflict, "smtp_disabled", "SMTP가 비활성화되어 있어 메일로 보낼 수 없습니다")
			return
		}
		if email == "" {
			writeError(w, http.StatusConflict, "email_missing", "사용자에게 등록된 이메일이 없습니다")
			return
		}
		sent, err := s.issuePasswordReset(r.Context(), r, id, actor.ID)
		if err != nil {
			writeError(w, http.StatusBadGateway, "mail_failed", "재설정 메일을 보내지 못했습니다: "+err.Error())
			return
		}
		if _, err := s.db.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
			notFoundOrServer(w, err)
			return
		}
		s.audit(r.Context(), actor.ID, "user.password_reset_mail", "user", id, clientIP(r), map[string]any{"sent": sent})
		writeJSON(w, http.StatusOK, map[string]any{"emailSent": sent, "to": maskEmail(email)})
		return
	}
	if source != "local" {
		writeError(w, http.StatusBadRequest, "oidc_user", "SSO 사용자의 비밀번호는 Keycloak에서 초기화하세요")
		return
	}
	if role == RoleSuperAdmin && actor.Role != RoleSuperAdmin {
		writeError(w, http.StatusForbidden, "super_admin_required", "최고 관리자 계정은 최고 관리자만 초기화할 수 있습니다")
		return
	}
	password, err := temporaryPassword()
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2,must_change_password=true,updated_at=now() WHERE id=$1`, id, string(hash)); err != nil {
		notFoundOrServer(w, err)
		return
	}
	sessions, err := tx.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM auth_throttle WHERE key='user:'||lower((SELECT username FROM users WHERE id=$1))`, id); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), actor.ID, "user.password_reset", "user", id, clientIP(r), map[string]any{"revokedSessions": sessions.RowsAffected()})
	writeJSON(w, http.StatusOK, map[string]any{"temporaryPassword": password, "revokedSessions": sessions.RowsAffected(), "mustChangePassword": true})
}

// revokeUserSessions signs the account out everywhere and revokes its personal
// API keys, which is what an off-boarding or a suspected leak needs at once.
func (s *Server) revokeUserSessions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	actor, _ := userFrom(r)
	var role string
	if err := s.db.QueryRow(r.Context(), `SELECT role FROM users WHERE id=$1`, id).Scan(&role); err != nil {
		notFoundOrServer(w, err)
		return
	}
	if role == RoleSuperAdmin && actor.Role != RoleSuperAdmin {
		writeError(w, http.StatusForbidden, "super_admin_required", "최고 관리자 계정은 최고 관리자만 종료할 수 있습니다")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	sessions, err := tx.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	keys, err := tx.Exec(r.Context(), `UPDATE api_keys SET revoked_at=now(),grace_until=NULL WHERE user_id=$1 AND revoked_at IS NULL`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), actor.ID, "user.sessions_revoke", "user", id, clientIP(r), map[string]any{"revokedSessions": sessions.RowsAffected(), "revokedKeys": keys.RowsAffected()})
	writeJSON(w, http.StatusOK, map[string]any{"revokedSessions": sessions.RowsAffected(), "revokedKeys": keys.RowsAffected()})
}
