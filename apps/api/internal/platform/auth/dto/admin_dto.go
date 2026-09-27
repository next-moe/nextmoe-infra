package dto

import "time"

type AssignSiteRoleRequest struct {
	SiteID    uint       `json:"site_id" validate:"required"`
	RoleName  string     `json:"role_name" validate:"required"`
	Note      string     `json:"note"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type SiteRoleResponse struct {
	SiteID    uint       `json:"site_id"`
	SiteName  string     `json:"site_name"`
	RoleName  string     `json:"role_name"`
	GrantedBy uint       `json:"granted_by"`
	GrantedAt time.Time  `json:"granted_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Note      string     `json:"note,omitempty"`
}

type UserListRequest struct {
	Page     int    `query:"page" validate:"omitempty,min=1"`
	Limit    int    `query:"limit" validate:"omitempty,min=1,max=100"`
	Search   string `query:"search"`
	Status   *int   `query:"status"`
	SortBy   string `query:"sort_by" validate:"omitempty,oneof=created_at name email moemoepoint"`
	SortDesc bool   `query:"sort_desc"`
}

type UserListResponse struct {
	Users      []UserResponse `json:"users"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	Limit      int            `json:"limit"`
	TotalPages int            `json:"total_pages"`
}

type UpdateUserRequest struct {
	Name   *string `json:"name" validate:"omitempty,kun_name"`
	Email  *string `json:"email" validate:"omitempty,email"`
	Avatar *string `json:"avatar" validate:"omitempty,url"`
	Bio    *string `json:"bio" validate:"omitempty,max=107"`
	About  *string `json:"about" validate:"omitempty,max=500"`
	Status *int    `json:"status" validate:"omitempty,oneof=0 1"`
}

type BanUserRequest struct {
	Reason string `json:"reason" validate:"required,min=1,max=500"`
}

type AssignRoleRequest struct {
	Role string `json:"role" validate:"required,oneof=user creator moderator admin"`
}

type UserDetailResponse struct {
	UserResponse
	IP            string                 `json:"ip"`
	SessionCount  int                    `json:"session_count"`
	OAuthAccounts int                    `json:"oauth_accounts"`
	SiteData      []UserSiteDataResponse `json:"site_data"`
	SiteRoles     []SiteRoleResponse     `json:"site_roles"`
}

type UserSiteDataResponse struct {
	SiteID   uint   `json:"site_id"`
	SiteName string `json:"site_name"`
	Status   int    `json:"status"`
}
