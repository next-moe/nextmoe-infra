package devapi

import (
	goerrors "errors"
	"strconv"

	apperr "api/pkg/errors"
	"api/pkg/response"

	siteModel "api/internal/platform/site/model"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type SelfServiceHandler struct {
	svc *SelfServiceService
}

func NewSelfServiceHandler(svc *SelfServiceService) *SelfServiceHandler {
	return &SelfServiceHandler{svc: svc}
}

func (h *SelfServiceHandler) Register(r fiber.Router) {
	r.Get("/policies", h.Policies)
	r.Post("/apps", h.CreateApp)
	r.Get("/apps", h.ListApps)
	r.Get("/apps/:client_id", h.GetApp)
	r.Patch("/apps/:client_id", h.UpdateApp)
	r.Delete("/apps/:client_id", h.ArchiveApp)
	r.Post("/apps/:client_id/resubmit", h.ResubmitApp)
	r.Post("/apps/:client_id/keys", h.MintKey)
	r.Get("/apps/:client_id/keys", h.ListKeys)
	r.Post("/apps/:client_id/keys/:id/rotate", h.RotateKey)
	// Revoke moved off DELETE on 2026-08-29 so DELETE could mean what it says.
	// The swap is safe to deploy in either order only because DeleteKey refuses
	// a key that is not already revoked: a stale portal still sending DELETE to
	// revoke gets a 409, never a silent hard delete of a live credential.
	r.Post("/apps/:client_id/keys/:id/revoke", h.RevokeKey)
	r.Delete("/apps/:client_id/keys/:id", h.DeleteKey)
	r.Get("/apps/:client_id/usage", h.Usage)
	r.Get("/usage", h.OwnerUsage)
}

type createAppRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	UserLogin   *userLoginRequest `json:"user_login"`
}

type updateAppRequest struct {
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	UserLogin   *userLoginRequest `json:"user_login"`
}

type userLoginRequest struct {
	RedirectURIs []string `json:"redirect_uris"`
	Scopes       []string `json:"scopes"`
}

func (r *userLoginRequest) toUserLogin() *UserLoginRequest {
	if r == nil {
		return nil
	}
	return &UserLoginRequest{RedirectURIs: r.RedirectURIs, Scopes: r.Scopes}
}

type selfMintKeyRequest struct {
	Name   string   `json:"name"`
	Test   bool     `json:"test"`
	Scopes []string `json:"scopes"`
}

type selfAppView struct {
	ClientID     string         `json:"client_id"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	DevEnabled   bool           `json:"dev_enabled"`
	Tier         string         `json:"tier"`
	RatePerMin   int            `json:"rate_per_min"`
	QuotaDaily   int            `json:"quota_daily"`
	KeyCount     int64          `json:"key_count"`
	CreatedAt    string         `json:"created_at"`
	ReviewStatus string         `json:"review_status"`
	ReviewNote   string         `json:"review_note,omitempty"`
	UserLogin    *userLoginView `json:"user_login"`
}

type userLoginView struct {
	RedirectURIs []string `json:"redirect_uris"`
	Scopes       []string `json:"scopes"`
	PKCERequired bool     `json:"pkce_required"`
}

func (h *SelfServiceHandler) Policies(c fiber.Ctx) error {
	modes, err := h.svc.EffectivePolicies(c.Context())
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, modes)
}

func (h *SelfServiceHandler) CreateApp(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	var req createAppRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, apperr.ErrBadRequest)
	}
	app, err := h.svc.CreateApp(c.Context(), ownerID, req.Name, req.Description, req.UserLogin.toUserLogin())
	if resp, handled := selfServicePolicyError(c, err); handled {
		return resp
	}
	if msg, bad := selfServiceBadRequest(err); bad {
		return response.BadRequestMsg(c, apperr.ErrValidationFailed, msg)
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, toSelfAppView(app, 0))
}

func (h *SelfServiceHandler) ResubmitApp(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	app, err := h.svc.ResubmitApp(c.Context(), ownerID, c.Params("client_id"))
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if resp, handled := selfServicePolicyError(c, err); handled {
		return resp
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	n, _ := h.svc.repo.CountKeysByClient(c.Context(), app.ID)
	return response.Success(c, toSelfAppView(app, n))
}

// The states the platform policy layer can put an owner in: a capability turned
// off (403) and an application that has not cleared review (409). The bool is
// load-bearing — every response.* helper returns nil on a written response, so
// a caller testing the returned error for nil falls straight through to the 500.
func selfServicePolicyError(c fiber.Ctx, err error) (error, bool) {
	switch {
	case goerrors.Is(err, ErrCapabilityDisabled):
		return response.ForbiddenMsg(c, apperr.ErrForbidden,
			"this is currently turned off by the platform — contact us if you need it"), true
	case goerrors.Is(err, ErrAppNotApproved):
		return response.Error(c, fiber.StatusConflict, apperr.ErrValidationFailed,
			"this application has not cleared review yet"), true
	case goerrors.Is(err, ErrAppNotDeclined):
		return response.Error(c, fiber.StatusConflict, apperr.ErrValidationFailed,
			"only a declined application can be resubmitted"), true
	default:
		return nil, false
	}
}

func (h *SelfServiceHandler) ListApps(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	apps, err := h.svc.ListApps(c.Context(), ownerID)
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	out := make([]selfAppView, len(apps))
	for i, a := range apps {
		out[i] = toSelfAppView(a.Client, a.KeyCount)
	}
	return response.Success(c, out)
}

func (h *SelfServiceHandler) GetApp(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	view, err := h.svc.GetApp(c.Context(), ownerID, c.Params("client_id"))
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, toSelfAppView(view.Client, view.KeyCount))
}

func (h *SelfServiceHandler) UpdateApp(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	var req updateAppRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, apperr.ErrBadRequest)
	}
	app, err := h.svc.UpdateApp(c.Context(), ownerID, c.Params("client_id"), req.Name, req.Description, req.UserLogin.toUserLogin())
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if resp, handled := selfServicePolicyError(c, err); handled {
		return resp
	}
	if msg, bad := selfServiceBadRequest(err); bad {
		return response.BadRequestMsg(c, apperr.ErrValidationFailed, msg)
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	n, _ := h.svc.repo.CountKeysByClient(c.Context(), app.ID)
	return response.Success(c, toSelfAppView(app, n))
}

func (h *SelfServiceHandler) ArchiveApp(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	err := h.svc.ArchiveApp(c.Context(), ownerID, c.Params("client_id"))
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if resp, handled := selfServicePolicyError(c, err); handled {
		return resp
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, nil)
}

func (h *SelfServiceHandler) MintKey(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	var req selfMintKeyRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, apperr.ErrBadRequest)
	}
	key, plaintext, err := h.svc.MintKey(c.Context(), ownerID, c.Params("client_id"), MintKeyInput{
		Name:   req.Name,
		Test:   req.Test,
		Scopes: req.Scopes,
	})
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if resp, handled := selfServicePolicyError(c, err); handled {
		return resp
	}
	if msg, bad := selfServiceBadRequest(err); bad {
		return response.BadRequestMsg(c, apperr.ErrValidationFailed, msg)
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, mintedKeyView{keyView: toKeyView(key), Key: plaintext})
}

func (h *SelfServiceHandler) ListKeys(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	keys, err := h.svc.ListKeys(c.Context(), ownerID, c.Params("client_id"))
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	out := make([]keyView, len(keys))
	for i := range keys {
		out[i] = toKeyView(&keys[i])
	}
	return response.Success(c, out)
}

func (h *SelfServiceHandler) RotateKey(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	keyID, ok := parseIDParam(c)
	if !ok {
		return response.BadRequest(c, apperr.ErrInvalidID)
	}
	key, plaintext, err := h.svc.RotateKey(c.Context(), ownerID, c.Params("client_id"), keyID)
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if resp, handled := selfServicePolicyError(c, err); handled {
		return resp
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	if key == nil {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	return response.Success(c, mintedKeyView{keyView: toKeyView(key), Key: plaintext})
}

func (h *SelfServiceHandler) RevokeKey(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	keyID, ok := parseIDParam(c)
	if !ok {
		return response.BadRequest(c, apperr.ErrInvalidID)
	}
	found, err := h.svc.RevokeKey(c.Context(), ownerID, c.Params("client_id"), keyID)
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	if !found {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	return response.Success(c, nil)
}

func (h *SelfServiceHandler) DeleteKey(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	keyID, ok := parseIDParam(c)
	if !ok {
		return response.BadRequest(c, apperr.ErrInvalidID)
	}
	found, err := h.svc.DeleteKey(c.Context(), ownerID, c.Params("client_id"), keyID)
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if resp, handled := keyDeleteConflict(c, err); handled {
		return resp
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	if !found {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	return response.Success(c, nil)
}

// Shared by both faces: the guard is an integrity rule about the audit trail,
// not a permission, so an operator gets the same two conflicts as an owner.
func keyDeleteConflict(c fiber.Ctx, err error) (error, bool) {
	switch {
	case goerrors.Is(err, ErrKeyNotRevoked):
		return response.Error(c, fiber.StatusConflict, apperr.ErrValidationFailed,
			"revoke this key before deleting it"), true
	case goerrors.Is(err, ErrKeyHasHistory):
		return response.Error(c, fiber.StatusConflict, apperr.ErrValidationFailed,
			"this key has served requests — it can be revoked but not deleted"), true
	default:
		return nil, false
	}
}

func (h *SelfServiceHandler) Usage(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	days := clampDays(c.Query("days"))
	rows, err := h.svc.Usage(c.Context(), ownerID, c.Params("client_id"), days)
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	if rows == nil {
		rows = []UsageDayFace{}
	}
	return response.Success(c, rows)
}

func (h *SelfServiceHandler) OwnerUsage(c fiber.Ctx) error {
	ownerID, ok := ownerFromCtx(c)
	if !ok {
		return response.Unauthorized(c, apperr.ErrAuthUnauthorized)
	}
	summary, err := h.svc.OwnerUsage(c.Context(), ownerID, clampDays(c.Query("days")))
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, summary)
}

func ownerFromCtx(c fiber.Ctx) (uint, bool) {
	id, ok := c.Locals("user_id").(uint)
	if !ok || id == 0 {
		return 0, false
	}
	return id, true
}

func clampDays(raw string) int {
	days, err := strconv.Atoi(raw)
	if err != nil || days <= 0 {
		return 7
	}
	if days > 30 {
		return 30
	}
	return days
}

func selfServiceBadRequest(err error) (string, bool) {
	switch {
	case err == nil:
		return "", false
	case goerrors.Is(err, ErrAppLimitReached):
		return "application limit reached (max 5 per account)", true
	case goerrors.Is(err, ErrKeyLimitReached):
		return "active key limit reached (max 5 per application)", true
	case goerrors.Is(err, ErrScopeNotAllowed):
		return "scope not permitted (want catalog:read or store:read)", true
	case goerrors.Is(err, ErrNameRequired):
		return "name is required", true
	case goerrors.Is(err, ErrNameTooLong):
		return "name too long (max 100)", true
	case goerrors.Is(err, ErrDescTooLong):
		return "description too long (max 100)", true
	case goerrors.Is(err, ErrRedirectURIRequired):
		return "user_login needs at least one redirect_uri", true
	case goerrors.Is(err, ErrTooManyRedirectURIs):
		return "too many redirect URIs (max 5)", true
	case goerrors.Is(err, ErrRedirectURIInvalid):
		return "redirect URI must be https://, or http:// on the 127.0.0.1 / [::1] loopback for a native app (no wildcards, no fragments)", true
	case goerrors.Is(err, ErrUserScopeNotAllowed):
		return "scope not permitted for a self-service app (want openid / profile / email / playtime:read / playtime:write / folder:read / folder:write / catalog:edit / catalog:read)", true
	case goerrors.Is(err, ErrAppNameReserved):
		return "application name may not claim to be NextMoe or an official application", true
	default:
		return "", false
	}
}

func toSelfAppView(app *siteModel.OAuthClient, keyCount int64) selfAppView {
	rate, quota := effectiveAppLimits(app)
	return selfAppView{
		ClientID:     app.ID,
		Name:         app.Name,
		Description:  app.Tagline,
		DevEnabled:   app.DevEnabled,
		Tier:         app.DevTier,
		RatePerMin:   rate,
		QuotaDaily:   quota,
		KeyCount:     keyCount,
		CreatedAt:    app.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		ReviewStatus: app.DevReviewStatus,
		ReviewNote:   app.DevReviewNote,
		UserLogin:    toUserLoginView(app),
	}
}

func effectiveAppLimits(app *siteModel.OAuthClient) (rate, quota int) {
	defRate, defQuota, unlimited := TierLimits(app.DevTier)
	if unlimited {
		return 0, 0
	}
	rate, quota = defRate, defQuota
	if app.DevRatePerMin > 0 {
		rate = app.DevRatePerMin
	}
	if app.DevQuotaDaily > 0 {
		quota = app.DevQuotaDaily
	}
	return rate, quota
}
