package dto

type RegisterRequest struct {
	Name      string `json:"name" validate:"required,kun_name"`
	Email     string `json:"email" validate:"required,email"`
	Password  string `json:"password" validate:"required,min=6,max=100"`
	Code      string `json:"code" validate:"required,len=6"`
	UserAgent string `json:"-"`
	IPAddress string `json:"-"`
	BrowserID string `json:"-"`
}

type SendRegisterCodeRequest struct {
	Name  string `json:"name" validate:"required,kun_name"`
	Email string `json:"email" validate:"required,email"`
}

type LoginRequest struct {
	Account   string `json:"account" validate:"required"`
	Password  string `json:"password" validate:"required"`
	UserAgent string `json:"-"`
	IPAddress string `json:"-"`
	BrowserID string `json:"-"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type SessionBrief struct {
	Sub             string   `json:"sub"`
	Name            string   `json:"name"`
	Email           string   `json:"email"`
	Avatar          string   `json:"avatar"`
	AvatarImageHash *string  `json:"avatar_image_hash,omitempty"`
	Roles           []string `json:"roles"`
	Active          bool     `json:"active"`
	LastUsedAt      string   `json:"last_used_at,omitempty"`
}

type ListSessionsResponse struct {
	Items []SessionBrief `json:"items"`
}

type AccountSubRequest struct {
	Sub string `json:"sub" validate:"required"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password" validate:"required,min=6,max=100"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}

type ResetPasswordRequest struct {
	Token    string `json:"token" validate:"required"`
	Password string `json:"password" validate:"required,min=6,max=100"`
}

type ResetPasswordConfirmRequest struct {
	Token    string `json:"token" validate:"required"`
	Password string `json:"password" validate:"required,min=6,max=100"`
}

type SendEmailChangeCodeRequest struct {
	NewEmail string `json:"new_email" validate:"required,email"`
}

type ChangeEmailRequest struct {
	Code     string `json:"code" validate:"required,len=6"`
	NewEmail string `json:"new_email" validate:"required,email"`
}

type UpdateProfileRequest struct {
	Name            *string `json:"name,omitempty"              validate:"omitempty,kun_name"`
	Avatar          *string `json:"avatar,omitempty"            validate:"omitempty,max=255"`
	AvatarImageHash *string `json:"avatar_image_hash,omitempty" validate:"omitempty,max=64"`
	Bio             *string `json:"bio,omitempty"               validate:"omitempty,max=107"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type UserResponse struct {
	ID              uint     `json:"id,omitempty"`
	UUID            string   `json:"uuid"`
	Name            string   `json:"name"`
	Email           string   `json:"email"`
	Avatar          string   `json:"avatar"`
	AvatarImageHash *string  `json:"avatar_image_hash,omitempty"`
	Bio             string   `json:"bio"`
	Moemoepoint     int      `json:"moemoepoint"`
	Status          int      `json:"status"`
	IsAnonymized    bool     `json:"is_anonymized"`
	OriginalEmail   string   `json:"original_email,omitempty"`
	Roles           []string `json:"roles"`
	CreatedAt       string   `json:"created_at"`

	// Filled by the /auth/me family only. Login, Register and federation
	// complete build this same struct and simply never assign these two
	// fields, so omitempty drops them and all three stay uniform; read
	// /auth/me for the values.
	//
	// A 2026-09-22 revision of this comment blamed GORM for it — "Register
	// answers with the record it just INSERTed and nsfw_display never entered
	// the INSERT". Measured on 2026-09-23, that is false: a `default` tag
	// holding a parseable literal is written into the INSERT and set back on
	// the struct, so Register does hold "hide". The uniformity is a choice
	// here, not a GORM limitation.
	AdultConfirmedAt *string `json:"adult_confirmed_at,omitempty"`
	NSFWDisplay      string  `json:"nsfw_display,omitempty"`
}

type LoginResponse struct {
	User        UserResponse `json:"user"`
	AccessToken string       `json:"access_token"`
}

type RefreshResponse struct {
	AccessToken string `json:"access_token"`
}
