package dto

type CreateSiteRequest struct {
	Name        string `json:"name" validate:"required,max=50"`
	Domain      string `json:"domain" validate:"required,max=255"`
	Description string `json:"description"`
}

type UpdateSiteRequest struct {
	Name        *string `json:"name" validate:"omitempty,max=50"`
	Description *string `json:"description"`
}

type SiteResponse struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

type CreateOAuthClientRequest struct {
	SiteID                 uint     `json:"site_id" validate:"required"`
	Name                   string   `json:"name" validate:"required,max=100"`
	RedirectURIs           []string `json:"redirect_uris" validate:"required,min=1"`
	Grants                 []string `json:"grants" validate:"omitempty,min=1,dive,oneof=authorization_code refresh_token"`
	AllowedScopes          []string `json:"allowed_scopes"`
	IsPublic               bool     `json:"is_public"`
	AutoConsent            bool     `json:"auto_consent"`
	RefreshTokenTTLSeconds *int     `json:"refresh_token_ttl_seconds" validate:"omitempty,min=60"`
	Listed                 bool     `json:"listed"`
	LogoURL                string   `json:"logo_url" validate:"omitempty,url,max=255"`
	Tagline                string   `json:"tagline" validate:"omitempty,max=100"`
	DisplayOrder           int      `json:"display_order" validate:"omitempty,min=0"`
}

type UpdateOAuthClientRequest struct {
	Name                   *string  `json:"name" validate:"omitempty,max=100"`
	RedirectURIs           []string `json:"redirect_uris" validate:"omitempty,min=1"`
	Grants                 []string `json:"grants" validate:"omitempty,min=1,dive,oneof=authorization_code refresh_token"`
	AllowedScopes          []string `json:"allowed_scopes"`
	AutoConsent            *bool    `json:"auto_consent"`
	RefreshTokenTTLSeconds *int     `json:"refresh_token_ttl_seconds" validate:"omitempty,min=60"`
	Listed                 *bool    `json:"listed"`
	LogoURL                *string  `json:"logo_url" validate:"omitempty,url,max=255"`
	Tagline                *string  `json:"tagline" validate:"omitempty,max=100"`
	DisplayOrder           *int     `json:"display_order" validate:"omitempty,min=0"`
}

type OAuthClientResponse struct {
	ID                     string                   `json:"id"`
	SiteID                 *uint                    `json:"site_id,omitempty"`
	Name                   string                   `json:"name"`
	RedirectURIs           []string                 `json:"redirect_uris"`
	Grants                 []string                 `json:"grants"`
	AllowedScopes          []string                 `json:"allowed_scopes"`
	IsPublic               bool                     `json:"is_public"`
	AutoConsent            bool                     `json:"auto_consent"`
	RefreshTokenTTLSeconds int                      `json:"refresh_token_ttl_seconds"`
	Listed                 bool                     `json:"listed"`
	LogoURL                string                   `json:"logo_url"`
	Tagline                string                   `json:"tagline"`
	DisplayOrder           int                      `json:"display_order"`
	CreatedAt              string                   `json:"created_at"`
	Storage                OAuthClientStorageConfig `json:"storage"`
}

type OAuthClientStorageConfig struct {
	ArtifactEnabled         bool     `json:"artifact_enabled"`
	ArtifactSiteKey         string   `json:"artifact_site_key"`
	ArtifactCDNBase         string   `json:"artifact_cdn_base"`
	ArtifactAllowedMime     []string `json:"artifact_allowed_mime"`
	ArtifactMaxFileSize     int64    `json:"artifact_max_file_size"`
	ArtifactQuotaDaily      int      `json:"artifact_quota_daily"`
	ArtifactQuotaBytesDaily int64    `json:"artifact_quota_bytes_daily"`
	ImageEnabled            bool     `json:"image_enabled"`
	ImageSiteKey            string   `json:"image_site_key"`
	ImageCDNBase            string   `json:"image_cdn_base"`
	ImageAllowedPresets     []string `json:"image_allowed_presets"`
	ImageMaxFileSize        int64    `json:"image_max_file_size"`
	ImageQuotaDaily         int      `json:"image_quota_daily"`
	ImageQuotaBytesDaily    int64    `json:"image_quota_bytes_daily"`
}

type UpdateClientStorageRequest struct {
	ArtifactEnabled         bool     `json:"artifact_enabled"`
	ArtifactSiteKey         string   `json:"artifact_site_key" validate:"max=32"`
	ArtifactCDNBase         string   `json:"artifact_cdn_base" validate:"max=255"`
	ArtifactAllowedMime     []string `json:"artifact_allowed_mime"`
	ArtifactMaxFileSize     int64    `json:"artifact_max_file_size" validate:"min=0"`
	ArtifactQuotaDaily      int      `json:"artifact_quota_daily" validate:"min=0"`
	ArtifactQuotaBytesDaily int64    `json:"artifact_quota_bytes_daily" validate:"min=0"`
	ImageEnabled            bool     `json:"image_enabled"`
	ImageSiteKey            string   `json:"image_site_key" validate:"max=32"`
	ImageCDNBase            string   `json:"image_cdn_base" validate:"max=255"`
	ImageAllowedPresets     []string `json:"image_allowed_presets"`
	ImageMaxFileSize        int64    `json:"image_max_file_size" validate:"min=0"`
	ImageQuotaDaily         int      `json:"image_quota_daily" validate:"min=0"`
	ImageQuotaBytesDaily    int64    `json:"image_quota_bytes_daily" validate:"min=0"`
}

type OAuthClientCreatedResponse struct {
	OAuthClientResponse
	Secret string `json:"secret"`
}
