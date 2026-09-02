package app

import "time"

const (
	RoleUser        = "user"
	RoleLobby       = "lobby"
	RoleDeptManager = "dept_manager"
	RoleSecurity    = "security"
	RoleAuditor     = "auditor"
	RoleAdmin       = "admin"
	RoleSuperAdmin  = "super_admin"
)

type User struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	DisplayName  string     `json:"displayName"`
	Email        string     `json:"email,omitempty"`
	EmployeeID   *string    `json:"employeeId,omitempty"`
	Role         string     `json:"role"`
	Source       string     `json:"source"`
	LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
	DepartmentID *string    `json:"departmentId,omitempty"`
	SiteScope    []string   `json:"siteScope,omitempty"`
	// Delegate receives this user's approvals and arrival notifications while
	// DelegateUntil is in the future.
	DelegateUserID *string    `json:"delegateUserId,omitempty"`
	DelegateUntil  *time.Time `json:"delegateUntil,omitempty"`
	// ApprovalDelegate is true while a department manager has delegated to this
	// user, which grants approval rights for that manager's department.
	ApprovalDelegate bool `json:"approvalDelegate,omitempty"`
	// MustChangePassword is set after an administrator issued a temporary
	// password; until cleared only /auth/me, /auth/password and logout work.
	MustChangePassword bool `json:"mustChangePassword,omitempty"`
}

// HasActiveDelegate reports whether approvals and notifications should route to
// the delegate right now.
func (u User) HasActiveDelegate(now time.Time) bool {
	return u.DelegateUserID != nil && *u.DelegateUserID != "" && u.DelegateUntil != nil && u.DelegateUntil.After(now)
}

func (u User) IsAdmin() bool { return u.Role == RoleAdmin || u.Role == RoleSuperAdmin }
func (u User) CanManageLobby() bool {
	return u.Role == RoleLobby || u.Role == RoleSecurity || u.IsAdmin()
}
func (u User) CanApprove() bool {
	return u.Role == RoleDeptManager || u.Role == RoleSecurity || u.IsAdmin() || u.ApprovalDelegate
}
func (u User) CanAudit() bool {
	return u.Role == RoleAuditor || u.Role == RoleSecurity || u.IsAdmin()
}

type Setting struct {
	Key        string `json:"key"`
	Value      string `json:"value"`
	Secret     bool   `json:"secret"`
	Configured bool   `json:"configured"`
}

type VisitorInput struct {
	Name      string   `json:"name"`
	Phone     string   `json:"phone"`
	Email     string   `json:"email,omitempty"`
	Company   string   `json:"company,omitempty"`
	Title     string   `json:"title,omitempty"`
	Vehicle   string   `json:"vehicle,omitempty"`
	Equipment []string `json:"equipment,omitempty"`
	Locale    string   `json:"locale,omitempty"`
	Consent   bool     `json:"consent"`
}

type VisitInput struct {
	SiteID       string          `json:"siteId"`
	LobbyID      string          `json:"lobbyId,omitempty"`
	DepartmentID string          `json:"departmentId,omitempty"`
	HostUserID   string          `json:"hostUserId,omitempty"`
	VisitTypeID  string          `json:"visitTypeId,omitempty"`
	StartAt      time.Time       `json:"startAt"`
	EndAt        time.Time       `json:"endAt"`
	Purpose      string          `json:"purpose"`
	PlaceDetail  string          `json:"placeDetail,omitempty"`
	Notes        string          `json:"notes,omitempty"`
	Visitors     []VisitorInput  `json:"visitors"`
	Recurrence   map[string]any  `json:"recurrence,omitempty"`
	Checklist    map[string]bool `json:"checklist,omitempty"`
}

// VisitType is the site's compliance profile for a kind of visit: which
// declarations the requester must acknowledge and whether approval is forced.
type VisitType struct {
	ID                     string `json:"id"`
	Code                   string `json:"code"`
	Name                   string `json:"name"`
	Description            string `json:"description,omitempty"`
	RequiresNDA            bool   `json:"requiresNda"`
	RequiresSafetyBriefing bool   `json:"requiresSafetyBriefing"`
	RequiresVehicle        bool   `json:"requiresVehicle"`
	RequiresEquipment      bool   `json:"requiresEquipment"`
	RequiresApproval       bool   `json:"requiresApproval"`
	Active                 bool   `json:"active"`
	SortOrder              int    `json:"sortOrder"`
}

type VisitSummary struct {
	ID             string    `json:"id"`
	RequestNo      string    `json:"requestNo"`
	HostUserID     string    `json:"hostUserId"`
	HostName       string    `json:"hostName"`
	DepartmentID   *string   `json:"departmentId,omitempty"`
	DepartmentName string    `json:"departmentName,omitempty"`
	SiteID         string    `json:"siteId"`
	SiteName       string    `json:"siteName"`
	LobbyID        *string   `json:"lobbyId,omitempty"`
	LobbyName      string    `json:"lobbyName,omitempty"`
	StartAt        time.Time `json:"startAt"`
	EndAt          time.Time `json:"endAt"`
	Purpose        string    `json:"purpose"`
	PlaceDetail    string    `json:"placeDetail,omitempty"`
	Status         string    `json:"status"`
	Source         string    `json:"source"`
	VisitTypeID    *string   `json:"visitTypeId,omitempty"`
	VisitTypeName  string    `json:"visitTypeName,omitempty"`
	ApprovalReason string    `json:"approvalReason,omitempty"`
	SeriesID       string    `json:"seriesId,omitempty"`
	VisitorCount   int       `json:"visitorCount"`
	PrimaryVisitor string    `json:"primaryVisitor"`
	Company        string    `json:"company,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type ctxKey string

const userContextKey ctxKey = "user"
const csrfContextKey ctxKey = "csrf"
const apiScopesContextKey ctxKey = "api_scopes"
const apiKeyAuthContextKey ctxKey = "api_key_auth"
const kioskContextKey ctxKey = "kiosk_device"
