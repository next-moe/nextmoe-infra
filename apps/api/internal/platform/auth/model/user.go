package model

import (
	"strings"
	"time"

	siteModel "api/internal/platform/site/model"

	"gorm.io/gorm"
)

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

type User struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	UUID           string  `gorm:"type:uuid;uniqueIndex;default:gen_random_uuid()" json:"uuid"`
	Name           string  `gorm:"size:17;uniqueIndex;not null" json:"name"`
	Email          string  `gorm:"size:255;uniqueIndex;not null" json:"email"`
	Password       *string `gorm:"size:255" json:"-"`
	KungalPassword *string `gorm:"size:255" json:"-"`
	MoyuPassword   *string `gorm:"size:255" json:"-"`
	Avatar         string  `gorm:"size:255;default:''" json:"avatar"`

	AvatarImageHash *string `gorm:"size:64;index" json:"avatar_image_hash,omitempty"`

	Bio         string `gorm:"size:107;default:''" json:"bio"`
	Moemoepoint int    `gorm:"default:0" json:"moemoepoint"`
	Status      int    `gorm:"default:0" json:"status"`

	AdultConfirmedAt *time.Time `gorm:"index" json:"adult_confirmed_at,omitempty"`
	NSFWDisplay      string     `gorm:"column:nsfw_display;size:8;not null;default:'blur'" json:"nsfw_display"`

	AnonymizedAt  *time.Time     `gorm:"index" json:"anonymized_at,omitempty"`
	OriginalEmail *string        `gorm:"size:255" json:"-"`
	IP            string         `gorm:"size:45;default:''" json:"-"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`

	SiteData      []UserSiteData   `gorm:"foreignKey:UserID" json:"site_data,omitempty"`
	Sessions      []Session        `gorm:"foreignKey:UserID" json:"-"`
	OAuthAccounts []OAuthAccount   `gorm:"foreignKey:UserID" json:"oauth_accounts,omitempty"`
	Followers     []UserFollow     `gorm:"foreignKey:FollowingID" json:"-"`
	Following     []UserFollow     `gorm:"foreignKey:FollowerID" json:"-"`
	Roles         []siteModel.Role `gorm:"many2many:user_roles;" json:"roles,omitempty"`
}

func (u *User) RoleNames() []string {
	names := make([]string, len(u.Roles))
	for i, r := range u.Roles {
		names[i] = r.Name
	}
	return names
}

func (User) TableName() string {
	return "users"
}

func (u *User) IsPasswordSet() bool {
	return u.Password != nil && *u.Password != ""
}

func (u *User) HasLegacyPassword() bool {
	return (u.KungalPassword != nil && *u.KungalPassword != "") ||
		(u.MoyuPassword != nil && *u.MoyuPassword != "")
}

func (u *User) IsBanned() bool {
	return u.Status == 1
}

func (u *User) IsAnonymized() bool {
	return u.AnonymizedAt != nil
}
