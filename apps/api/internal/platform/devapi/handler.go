package devapi

import (
	"encoding/json"
	goerrors "errors"
	"strconv"
	"time"

	apperr "api/pkg/errors"
	"api/pkg/response"

	devapiPerm "api/internal/platform/devapi/perm"
	siteModel "api/internal/platform/site/model"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type AdminHandler struct {
	svc *AdminService
}

func NewAdminHandler(svc *AdminService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// policyWriteGate is passed in rather than imported: the whole group already
// sits behind devapi.manage, and only these two routes additionally need
// devapi.policy_manage. Wiring it at the composition root keeps this package
// free of a dependency on the middleware package.
func (h *AdminHandler) Register(r fiber.Router, policyWriteGate fiber.Handler) {
	r.Get("/apps", h.ListApps)
	r.Patch("/apps/:client_id", h.PatchApp)
	r.Post("/apps/:client_id/approve", h.ApproveApp)
	r.Post("/apps/:client_id/decline", h.DeclineApp)
	r.Post("/apps/:client_id/archive", h.ArchiveApp)
	r.Delete("/apps/:client_id", h.DeleteApp)
	r.Post("/apps/:client_id/keys", h.MintKey)
	r.Get("/apps/:client_id/keys", h.ListKeys)
	r.Post("/apps/:client_id/keys/:id/rotate", h.RotateKey)
	// Same swap as the portal face: revoke is a POST now so DELETE can be a
	// delete. See the note in SelfServiceHandler.Register for why the two
	// deploy in either order without risk.
	r.Post("/apps/:client_id/keys/:id/revoke", h.RevokeKey)
	r.Delete("/apps/:client_id/keys/:id", h.DeleteKey)
	r.Get("/keys", h.ListAllKeys)
	r.Get("/policies", h.ListPolicies)
	r.Put("/policies/:capability", policyWriteGate, h.SetPolicy)
	r.Delete("/policies/:capability", policyWriteGate, h.ResetPolicy)
}

type patchAppRequest struct {
	OwnerUserID             *uint   `json:"owner_user_id"`
	DevEnabled              *bool   `json:"dev_enabled"`
	DevTier                 *string `json:"dev_tier"`
	DevRatePerMin           *int    `json:"dev_rate_per_min"`
	DevQuotaDaily           *int    `json:"dev_quota_daily"`
	StoreSettlementEligible *bool   `json:"store_settlement_eligible"`
}

type mintKeyRequest struct {
	Name   string   `json:"name"`
	Test   bool     `json:"test"`
	Scopes []string `json:"scopes"`
}

type appView struct {
	ClientID                string `json:"client_id"`
	Name                    string `json:"name"`
	OwnerUserID             *uint  `json:"owner_user_id,omitempty"`
	DevEnabled              bool   `json:"dev_enabled"`
	DevTier                 string `json:"dev_tier"`
	DevRatePerMin           int    `json:"dev_rate_per_min"`
	DevQuotaDaily           int    `json:"dev_quota_daily"`
	KeyCount                int64  `json:"key_count"`
	ReviewStatus            string `json:"review_status"`
	ReviewNote              string `json:"review_note,omitempty"`
	ArchivedAt              string `json:"archived_at,omitempty"`
	StoreSettlementEligible bool   `json:"store_settlement_eligible"`
	// The two disqualifiers for a hard delete that no cleanup can ever clear.
	// Keys, usage, store links and logins can all go away; being able to sign a
	// user in, or being a site's login client, cannot. The console shows these
	// so it can withhold a button that would refuse forever, rather than
	// re-deriving the guard and drifting from it.
	LoginClient bool   `json:"login_client"`
	BoundSite   bool   `json:"bound_site"`
	CreatedAt   string `json:"created_at"`
}

func toAppView(app *siteModel.OAuthClient, keyCount int64) appView {
	v := appView{
		ClientID:                app.ID,
		Name:                    app.Name,
		OwnerUserID:             app.OwnerUserID,
		DevEnabled:              app.DevEnabled,
		DevTier:                 app.DevTier,
		DevRatePerMin:           app.DevRatePerMin,
		DevQuotaDaily:           app.DevQuotaDaily,
		KeyCount:                keyCount,
		ReviewStatus:            app.DevReviewStatus,
		ReviewNote:              app.DevReviewNote,
		StoreSettlementEligible: app.StoreSettlementEligible,
		LoginClient:             app.CanSignInUsers(),
		BoundSite:               app.SiteID != nil,
		CreatedAt:               app.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if app.DevArchivedAt != nil {
		v.ArchivedAt = app.DevArchivedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return v
}

type keyView struct {
	ID         uint     `json:"id"`
	ClientID   string   `json:"client_id"`
	Name       string   `json:"name"`
	KeyPrefix  string   `json:"key_prefix"`
	Last4      string   `json:"last4"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  string   `json:"expires_at,omitempty"`
	RevokedAt  string   `json:"revoked_at,omitempty"`
	LastUsedAt string   `json:"last_used_at,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

type mintedKeyView struct {
	keyView
	Key string `json:"key"`
}

func (h *AdminHandler) ListApps(c fiber.Ctx) error {
	apps, err := h.svc.ListApps(c.Context(), c.Query("status", AppFilterEnabled))
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	out := make([]appView, len(apps))
	for i, a := range apps {
		out[i] = toAppView(a.Client, a.KeyCount)
	}
	return response.Success(c, out)
}

func (h *AdminHandler) PatchApp(c fiber.Ctx) error {
	clientID := c.Params("client_id")
	if clientID == "" {
		return response.BadRequest(c, apperr.ErrMissingParam)
	}
	var req patchAppRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, apperr.ErrBadRequest)
	}
	cfg := AppConfig{
		OwnerUserID:             req.OwnerUserID,
		DevEnabled:              req.DevEnabled,
		DevTier:                 req.DevTier,
		DevRatePerMin:           req.DevRatePerMin,
		DevQuotaDaily:           req.DevQuotaDaily,
		StoreSettlementEligible: req.StoreSettlementEligible,
	}
	roles, _ := c.Locals("user_roles").([]string)
	if !devapiPerm.Resolver.Can(roles, devapiPerm.PolicyManage) {
		moves, err := h.svc.MovesSettlement(c.Context(), clientID, cfg)
		if goerrors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, apperr.ErrNotFound)
		}
		if err != nil {
			return response.InternalError(c, apperr.ErrOperationFailed)
		}
		if moves {
			return response.ForbiddenMsg(c, apperr.ErrForbidden, "改应用归属或 DLsite 分成资格需要 devapi.policy_manage（ren）")
		}
	}
	app, err := h.svc.UpdateAppConfig(c.Context(), clientID, cfg)
	if goerrors.Is(err, ErrInvalidTier) {
		return response.BadRequestMsg(c, apperr.ErrValidationFailed, "invalid tier (want free|trusted|internal)")
	}
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, toAppView(app, 0))
}

type declineAppRequest struct {
	Reason string `json:"reason"`
}

func (h *AdminHandler) ApproveApp(c fiber.Ctx) error {
	reviewer, _ := c.Locals("user_id").(uint)
	app, err := h.svc.ApproveApp(c.Context(), c.Params("client_id"), reviewer)
	return h.respondAppReview(c, app, err)
}

func (h *AdminHandler) DeclineApp(c fiber.Ctx) error {
	var req declineAppRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, apperr.ErrBadRequest)
	}
	reviewer, _ := c.Locals("user_id").(uint)
	app, err := h.svc.DeclineApp(c.Context(), c.Params("client_id"), reviewer, req.Reason)
	return h.respondAppReview(c, app, err)
}

func (h *AdminHandler) ArchiveApp(c fiber.Ctx) error {
	app, err := h.svc.ArchiveApp(c.Context(), c.Params("client_id"))
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	n, _ := h.svc.repo.CountKeysByClient(c.Context(), app.ID)
	return response.Success(c, toAppView(app, n))
}

func (h *AdminHandler) DeleteApp(c fiber.Ctx) error {
	err := h.svc.DeleteApp(c.Context(), c.Params("client_id"))
	switch {
	case goerrors.Is(err, gorm.ErrRecordNotFound):
		return response.NotFound(c, apperr.ErrNotFound)
	case goerrors.Is(err, ErrAppNotArchived):
		return response.Error(c, fiber.StatusConflict, apperr.ErrValidationFailed,
			"archive this application before deleting it")
	case goerrors.Is(err, ErrAppHasReferences):
		return response.Error(c, fiber.StatusConflict, apperr.ErrValidationFailed,
			"this application has keys, usage, store links or logins behind it, or can sign users in — archive is as far as deletion goes")
	case err != nil:
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, nil)
}

func (h *AdminHandler) respondAppReview(c fiber.Ctx, app *siteModel.OAuthClient, err error) error {
	switch {
	case goerrors.Is(err, gorm.ErrRecordNotFound):
		return response.NotFound(c, apperr.ErrNotFound)
	case goerrors.Is(err, ErrAppReviewNotPending):
		return response.Error(c, fiber.StatusConflict, apperr.ErrValidationFailed,
			"only a pending application can be reviewed")
	case goerrors.Is(err, ErrAppReviewNeedsReason):
		return response.BadRequestMsg(c, apperr.ErrValidationFailed, "a decline needs a reason")
	case goerrors.Is(err, ErrAppReviewNoteTooLong):
		return response.BadRequestMsg(c, apperr.ErrValidationFailed, "reason too long (max 2000)")
	case err != nil:
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	n, _ := h.svc.repo.CountKeysByClient(c.Context(), app.ID)
	return response.Success(c, toAppView(app, n))
}

func (h *AdminHandler) MintKey(c fiber.Ctx) error {
	clientID := c.Params("client_id")
	if clientID == "" {
		return response.BadRequest(c, apperr.ErrMissingParam)
	}
	var req mintKeyRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, apperr.ErrBadRequest)
	}
	if req.Name == "" {
		return response.BadRequestMsg(c, apperr.ErrValidationFailed, "name is required")
	}
	createdBy, _ := c.Locals("user_id").(uint)
	key, plaintext, err := h.svc.MintKey(c.Context(), clientID, MintKeyInput{
		Name:   req.Name,
		Test:   req.Test,
		Scopes: req.Scopes,
	}, createdBy)
	if goerrors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound(c, apperr.ErrNotFound)
	}
	if goerrors.Is(err, ErrAppArchived) {
		return response.Error(c, fiber.StatusConflict, apperr.ErrValidationFailed,
			"this application is archived — re-enable it before minting a key")
	}
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, mintedKeyView{keyView: toKeyView(key), Key: plaintext})
}

func (h *AdminHandler) ListKeys(c fiber.Ctx) error {
	clientID := c.Params("client_id")
	if clientID == "" {
		return response.BadRequest(c, apperr.ErrMissingParam)
	}
	keys, err := h.svc.ListKeys(c.Context(), clientID)
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	out := make([]keyView, len(keys))
	for i := range keys {
		out[i] = toKeyView(&keys[i])
	}
	return response.Success(c, out)
}

func (h *AdminHandler) RotateKey(c fiber.Ctx) error {
	clientID := c.Params("client_id")
	keyID, ok := parseIDParam(c)
	if !ok {
		return response.BadRequest(c, apperr.ErrInvalidID)
	}
	if _, err := h.requireKeyOfClient(c, clientID, keyID); err != nil {
		return err
	}
	createdBy, _ := c.Locals("user_id").(uint)
	key, plaintext, err := h.svc.RotateKey(c.Context(), keyID, createdBy)
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, mintedKeyView{keyView: toKeyView(key), Key: plaintext})
}

func (h *AdminHandler) RevokeKey(c fiber.Ctx) error {
	clientID := c.Params("client_id")
	keyID, ok := parseIDParam(c)
	if !ok {
		return response.BadRequest(c, apperr.ErrInvalidID)
	}
	if _, err := h.requireKeyOfClient(c, clientID, keyID); err != nil {
		return err
	}
	if err := h.svc.RevokeKey(c.Context(), keyID); err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, nil)
}

func (h *AdminHandler) DeleteKey(c fiber.Ctx) error {
	clientID := c.Params("client_id")
	keyID, ok := parseIDParam(c)
	if !ok {
		return response.BadRequest(c, apperr.ErrInvalidID)
	}
	key, err := h.requireKeyOfClient(c, clientID, keyID)
	if err != nil {
		return err
	}
	// One call, one error: keyDeleteConflict answers only the two 409s, so a
	// plain database failure must still be checked for separately or it lands
	// on the success path.
	delErr := h.svc.DeleteKey(c.Context(), key)
	if resp, handled := keyDeleteConflict(c, delErr); handled {
		return resp
	}
	if delErr != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, nil)
}

type adminKeyView struct {
	keyView
	AppName     string `json:"app_name"`
	OwnerUserID *uint  `json:"owner_user_id,omitempty"`
	State       string `json:"state"`
}

func (h *AdminHandler) ListAllKeys(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, total, err := h.svc.ListAllKeys(c.Context(), KeyListFilter{
		ClientID: c.Query("client_id"),
		State:    c.Query("state", KeyStateAll),
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	now := time.Now()
	items := make([]adminKeyView, len(rows))
	for i := range rows {
		items[i] = adminKeyView{
			keyView:     toKeyView(&rows[i].DeveloperAPIKey),
			AppName:     rows[i].AppName,
			OwnerUserID: rows[i].OwnerUserID,
			State:       rows[i].DeveloperAPIKey.State(now),
		}
	}
	return response.Success(c, fiber.Map{
		"items": items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *AdminHandler) requireKeyOfClient(c fiber.Ctx, clientID string, keyID uint) (*DeveloperAPIKey, error) {
	key, err := h.svc.GetKeyForClient(c.Context(), clientID, keyID)
	if goerrors.Is(err, gorm.ErrRecordNotFound) || key == nil {
		return nil, response.NotFound(c, apperr.ErrNotFound)
	}
	if err != nil {
		return nil, response.InternalError(c, apperr.ErrOperationFailed)
	}
	return key, nil
}

func parseIDParam(c fiber.Ctx) (uint, bool) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return 0, false
	}
	return uint(id), true
}

func toKeyView(k *DeveloperAPIKey) keyView {
	var scopes []string
	if len(k.Scopes) > 0 {
		_ = json.Unmarshal(k.Scopes, &scopes)
	}
	v := keyView{
		ID:        k.ID,
		ClientID:  k.ClientID,
		Name:      k.Name,
		KeyPrefix: k.KeyPrefix,
		Last4:     k.Last4,
		Scopes:    scopes,
		CreatedAt: k.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if k.ExpiresAt != nil {
		v.ExpiresAt = k.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if k.RevokedAt != nil {
		v.RevokedAt = k.RevokedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if k.LastUsedAt != nil {
		v.LastUsedAt = k.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return v
}
