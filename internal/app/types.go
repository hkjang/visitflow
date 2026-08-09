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
}

func (u User) IsAdmin() bool { return u.Role == RoleAdmin || u.Role == RoleSuperAdmin }
func (u User) CanManageLobby() bool {
	return u.Role == RoleLobby || u.Role == RoleSecurity || u.IsAdmin()
}
func (u User) CanApprove() bool {
	return u.Role == RoleDeptManager || u.Role == RoleSecurity || u.IsAdmin()
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
	Consent   bool     `json:"consent"`
}

type VisitInput struct {
	SiteID       string         `json:"siteId"`
	LobbyID      string         `json:"lobbyId,omitempty"`
	DepartmentID string         `json:"departmentId,omitempty"`
	HostUserID   string         `json:"hostUserId,omitempty"`
	StartAt      time.Time      `json:"startAt"`
	EndAt        time.Time      `json:"endAt"`
	Purpose      string         `json:"purpose"`
	PlaceDetail  string         `json:"placeDetail,omitempty"`
	Notes        string         `json:"notes,omitempty"`
	Visitors     []VisitorInput `json:"visitors"`
	Recurrence   map[string]any `json:"recurrence,omitempty"`
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
	VisitorCount   int       `json:"visitorCount"`
	PrimaryVisitor string    `json:"primaryVisitor"`
	Company        string    `json:"company,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type ctxKey string

const userContextKey ctxKey = "user"
const csrfContextKey ctxKey = "csrf"
const apiScopesContextKey ctxKey = "api_scopes"
