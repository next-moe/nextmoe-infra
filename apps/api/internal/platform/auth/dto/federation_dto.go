package dto

type FederationCompleteRequest struct {
	Token     string `json:"token" validate:"required"`
	Name      string `json:"name" validate:"required,kun_name"`
	Password  string `json:"password" validate:"required,min=6,max=100"`
	Email     string `json:"email"`
	Code      string `json:"code"`
	UserAgent string `json:"-"`
	IPAddress string `json:"-"`
	BrowserID string `json:"-"`
}

type FederationPendingResponse struct {
	Provider      string `json:"provider"`
	SuggestedName string `json:"suggested_name"`
	Email         string `json:"email"`
	EmailLocked   bool   `json:"email_locked"`
}

type FederationProviderItem struct {
	Name string `json:"name"`
}

type FederationProvidersResponse struct {
	Providers []FederationProviderItem `json:"providers"`
}
