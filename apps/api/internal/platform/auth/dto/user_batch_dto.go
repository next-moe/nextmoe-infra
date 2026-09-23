package dto

import shopModel "api/internal/platform/shop/model"

type UserBrief struct {
	ID              uint     `json:"id"`
	UUID            string   `json:"uuid"`
	Name            string   `json:"name"`
	Avatar          string   `json:"avatar"`
	AvatarImageHash *string  `json:"avatar_image_hash,omitempty"`
	Bio             string   `json:"bio"`
	Status          int      `json:"status"`
	Roles           []string `json:"roles"`
	SiteRoles       []string `json:"site_roles,omitempty"`
	CreatedAt       string   `json:"created_at"`

	Cosmetics shopModel.Cosmetics `json:"cosmetics,omitempty"`
}

type BatchGetUsersRequest struct {
	IDs []uint `query:"ids" validate:"required,min=1,max=100"`
}

type BatchGetUsersResponse struct {
	Users    []UserBrief `json:"users"`
	NotFound []uint      `json:"not_found"`
}

type SearchUsersResponse struct {
	Users []UserBrief `json:"users"`
}
