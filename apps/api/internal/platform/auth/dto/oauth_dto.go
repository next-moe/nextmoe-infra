package dto

type AuthorizeRequest struct {
	ClientID            string `query:"client_id" json:"client_id" validate:"required"`
	RedirectURI         string `query:"redirect_uri" json:"redirect_uri" validate:"required,url"`
	ResponseType        string `query:"response_type" json:"response_type" validate:"required,oneof=code"`
	Scope               string `query:"scope" json:"scope"`
	State               string `query:"state" json:"state" validate:"required"`
	CodeChallenge       string `query:"code_challenge" json:"code_challenge"`
	CodeChallengeMethod string `query:"code_challenge_method" json:"code_challenge_method" validate:"omitempty,oneof=S256"`
	Prompt              string `query:"prompt" json:"prompt" validate:"omitempty,oneof=login select_account none"`
	LoginHint           string `query:"login_hint" json:"login_hint"`
	Nonce               string `query:"nonce" json:"nonce"`
}

type AuthorizeErrorRequest struct {
	ClientID    string `json:"client_id" validate:"required"`
	RedirectURI string `json:"redirect_uri" validate:"required,url"`
	State       string `json:"state"`
	Error       string `json:"error" validate:"required,oneof=access_denied login_required interaction_required consent_required account_selection_required"`
}

type TokenRequest struct {
	GrantType    string `json:"grant_type" form:"grant_type" validate:"required,oneof=authorization_code refresh_token"`
	Code         string `json:"code" form:"code"`
	RedirectURI  string `json:"redirect_uri" form:"redirect_uri"`
	ClientID     string `json:"client_id" form:"client_id" validate:"required"`
	ClientSecret string `json:"client_secret" form:"client_secret"`
	RefreshToken string `json:"refresh_token" form:"refresh_token"`
	CodeVerifier string `json:"code_verifier" form:"code_verifier"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

type UserInfoResponse struct {
	ID        uint     `json:"id"`
	Sub       string   `json:"sub"`
	Name      string   `json:"name,omitempty"`
	Email     string   `json:"email,omitempty"`
	Picture   string   `json:"picture,omitempty"`
	Roles     []string `json:"roles"`
	SiteRoles []string `json:"site_roles,omitempty"`
	UpdatedAt int64    `json:"updated_at,omitempty"`

	AdultConfirmed *bool  `json:"adult_confirmed,omitempty"`
	NSFWDisplay    string `json:"nsfw_display,omitempty"`
}

type AuthorizationCode struct {
	Code         string
	ClientID     string
	UserUUID     string
	RedirectURI  string
	Scope        string
	CodeVerifier string
	ExpiresAt    int64
}
