package handler

import (
	"encoding/json"
	goerrors "errors"
	"slices"
	"strconv"

	"api/internal/platform/devapi"
	"api/internal/platform/site/dto"
	siteModel "api/internal/platform/site/model"
	"api/internal/platform/site/perm"
	"api/internal/platform/site/service"
	"api/pkg/errors"
	"api/pkg/response"
	"api/pkg/utils"

	"github.com/gofiber/fiber/v3"
)

var renOnlyScopes = []string{"image:upload", "artifact:upload", "chat:read", "chat:write"}

func addsRenOnlyScope(scopes []string) bool {
	for _, s := range scopes {
		if slices.Contains(renOnlyScopes, s) {
			return true
		}
	}
	return false
}

func addsNewRenOnlyScope(reqScopes, curScopes []string) bool {
	for _, s := range reqScopes {
		if slices.Contains(renOnlyScopes, s) && !slices.Contains(curScopes, s) {
			return true
		}
	}
	return false
}

const renSensitiveFieldMsg = "仅 ren（莲）可授予 image:upload / artifact:upload / chat:read / chat:write scope 或开启自动同意"

type SiteHandler struct {
	siteService *service.SiteService
}

func NewSiteHandler(siteService *service.SiteService) *SiteHandler {
	return &SiteHandler{siteService: siteService}
}

func callerRoles(c fiber.Ctx) []string {
	roles, _ := c.Locals("user_roles").([]string)
	return roles
}

func callerUserID(c fiber.Ctx) uint {
	id, _ := c.Locals("user_id").(uint)
	return id
}

func (h *SiteHandler) managesAll(c fiber.Ctx) bool {
	return perm.Resolver.Can(callerRoles(c), perm.SitesManageAll)
}

func mayManage(managesAll bool, callerID uint, createdBy *uint) bool {
	if managesAll {
		return true
	}
	return callerID != 0 && createdBy != nil && *createdBy == callerID
}

func isMine(callerID uint, cl *siteModel.OAuthClient) bool {
	is := func(id *uint) bool { return callerID != 0 && id != nil && *id == callerID }
	return is(cl.CreatedByUserID) || is(cl.OwnerUserID)
}

const notOwnerMsg = "只能查看和管理自己创建的站点 / 客户端"

func (h *SiteHandler) List(c fiber.Ctx) error {
	var sites []siteModel.Site
	var err error
	if h.managesAll(c) {
		sites, err = h.siteService.List(c.Context())
	} else {
		sites, err = h.siteService.ListByCreator(c.Context(), callerUserID(c))
	}
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	result := make([]dto.SiteResponse, len(sites))
	for i, s := range sites {
		result[i] = dto.SiteResponse{
			ID:          s.ID,
			Name:        s.Name,
			Domain:      s.Domain,
			Description: s.Description,
			CreatedAt:   s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
	}

	return response.Success(c, result)
}

func (h *SiteHandler) Get(c fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}

	site, err := h.siteService.GetByID(c.Context(), uint(id))
	if err != nil {
		return response.NotFound(c, errors.ErrSiteNotFound)
	}
	if !mayManage(h.managesAll(c), callerUserID(c), site.CreatedByUserID) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, notOwnerMsg)
	}

	return response.Success(c, dto.SiteResponse{
		ID:          site.ID,
		Name:        site.Name,
		Domain:      site.Domain,
		Description: site.Description,
		CreatedAt:   site.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (h *SiteHandler) Create(c fiber.Ctx) error {
	var req dto.CreateSiteRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	if h.siteService.DomainExists(c.Context(), req.Domain) {
		return response.BadRequest(c, errors.ErrSiteAlreadyExists)
	}

	site := &siteModel.Site{
		Name:        req.Name,
		Domain:      req.Domain,
		Description: req.Description,
	}
	if uid := callerUserID(c); uid != 0 {
		site.CreatedByUserID = &uid
	}

	if err := h.siteService.Create(c.Context(), site); err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, dto.SiteResponse{
		ID:          site.ID,
		Name:        site.Name,
		Domain:      site.Domain,
		Description: site.Description,
		CreatedAt:   site.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (h *SiteHandler) Update(c fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}

	var req dto.UpdateSiteRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	site, err := h.siteService.GetByID(c.Context(), uint(id))
	if err != nil {
		return response.NotFound(c, errors.ErrSiteNotFound)
	}
	if !mayManage(h.managesAll(c), callerUserID(c), site.CreatedByUserID) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, notOwnerMsg)
	}

	if req.Name != nil {
		site.Name = *req.Name
	}
	if req.Description != nil {
		site.Description = *req.Description
	}

	if err := h.siteService.Update(c.Context(), site); err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, dto.SiteResponse{
		ID:          site.ID,
		Name:        site.Name,
		Domain:      site.Domain,
		Description: site.Description,
		CreatedAt:   site.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (h *SiteHandler) Delete(c fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}

	site, err := h.siteService.GetByID(c.Context(), uint(id))
	if err != nil {
		return response.NotFound(c, errors.ErrSiteNotFound)
	}
	if !mayManage(h.managesAll(c), callerUserID(c), site.CreatedByUserID) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, notOwnerMsg)
	}

	clients, err := h.siteService.GetOAuthClientsBySiteID(c.Context(), uint(id))
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	if len(clients) > 0 {
		return response.BadRequestMsg(c, errors.ErrOperationFailed, "站点下仍有 OAuth 客户端，请先删除")
	}

	if err := h.siteService.Delete(c.Context(), uint(id)); err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, nil)
}

func (h *SiteHandler) ListClients(c fiber.Ctx) error {
	var clients []siteModel.OAuthClient
	var err error
	if h.managesAll(c) {
		clients, err = h.siteService.ListOAuthClients(c.Context())
	} else {
		clients, err = h.siteService.ListOAuthClientsByCreator(c.Context(), callerUserID(c))
	}
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	result := make([]dto.OAuthClientResponse, len(clients))
	for i, cl := range clients {
		result[i] = toOAuthClientResponse(&cl, callerUserID(c))
	}

	return response.Success(c, result)
}

func (h *SiteHandler) GetSiteClients(c fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}

	site, err := h.siteService.GetByID(c.Context(), uint(id))
	if err != nil {
		return response.NotFound(c, errors.ErrSiteNotFound)
	}
	managesAll := h.managesAll(c)
	if !mayManage(managesAll, callerUserID(c), site.CreatedByUserID) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, notOwnerMsg)
	}

	var clients []siteModel.OAuthClient
	if managesAll {
		clients, err = h.siteService.GetOAuthClientsBySiteID(c.Context(), uint(id))
	} else {
		clients, err = h.siteService.GetOAuthClientsBySiteIDAndCreator(c.Context(), uint(id), callerUserID(c))
	}
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	result := make([]dto.OAuthClientResponse, len(clients))
	for i, cl := range clients {
		result[i] = toOAuthClientResponse(&cl, callerUserID(c))
	}

	return response.Success(c, result)
}

func (h *SiteHandler) CreateClient(c fiber.Ctx) error {
	var req dto.CreateOAuthClientRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	roles := callerRoles(c)
	if !perm.Resolver.Can(roles, perm.ClientsPrivilegedConfig) &&
		(addsRenOnlyScope(req.AllowedScopes) || req.AutoConsent) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, renSensitiveFieldMsg)
	}

	if !perm.Resolver.Can(roles, perm.ClientsPrivilegedConfig) {
		req.DisplayOrder = 0
	}

	site, err := h.siteService.GetByID(c.Context(), req.SiteID)
	if err != nil {
		return response.NotFound(c, errors.ErrSiteNotFound)
	}
	if !mayManage(perm.Resolver.Can(roles, perm.SitesManageAll), callerUserID(c), site.CreatedByUserID) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, notOwnerMsg)
	}

	// Default grants include BOTH authorization_code and refresh_token —
	// real-world clients almost always need both, and shipping
	// authorization_code only causes silent re-login storms 15 minutes
	// after every login (the JWT TTL) because the OAuth server's
	// refresh-grant check rejects them.
	grants := req.Grants
	if grants == nil {
		grants = []string{"authorization_code", "refresh_token"}
	}

	var createdBy *uint
	if uid := callerUserID(c); uid != 0 {
		createdBy = &uid
	}

	client, secret, err := h.siteService.CreateOAuthClient(
		c.Context(),
		req.SiteID,
		req.Name,
		req.RedirectURIs,
		grants,
		req.AllowedScopes,
		req.IsPublic,
		req.AutoConsent,
		req.RefreshTokenTTLSeconds,
		req.Listed,
		req.LogoURL,
		req.Tagline,
		req.DisplayOrder,
		createdBy,
	)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, dto.OAuthClientCreatedResponse{
		OAuthClientResponse: toOAuthClientResponse(client, callerUserID(c)),
		Secret:              secret,
	})
}

func (h *SiteHandler) UpdateClient(c fiber.Ctx) error {
	clientID := c.Params("id")
	if clientID == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}

	var req dto.UpdateOAuthClientRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	// The edit form re-sends every field, so an empty optional string arrives as
	// a non-nil pointer to "". Normalize "" → nil (= leave unchanged) so it skips
	// the validators — go-validator's omitempty only skips a NIL pointer, not a
	// pointer to "", so "" would otherwise trip the url tag. (To swap a logo set a
	// new one; emptying the field leaves the current value as-is.)
	if req.LogoURL != nil && *req.LogoURL == "" {
		req.LogoURL = nil
	}
	if req.Tagline != nil && *req.Tagline == "" {
		req.Tagline = nil
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	cur, err := h.siteService.GetOAuthClient(c.Context(), clientID)
	if err != nil {
		return response.NotFound(c, errors.ErrOperationFailed)
	}
	if !mayManage(h.managesAll(c), callerUserID(c), cur.CreatedByUserID) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, notOwnerMsg)
	}

	if !perm.Resolver.Can(callerRoles(c), perm.ClientsPrivilegedConfig) {
		var curScopes []string
		_ = json.Unmarshal(cur.AllowedScopes, &curScopes)
		addsScope := req.AllowedScopes != nil && addsNewRenOnlyScope(req.AllowedScopes, curScopes)
		enablesAutoConsent := req.AutoConsent != nil && *req.AutoConsent && !cur.AutoConsent
		if addsScope || enablesAutoConsent {
			return response.ForbiddenMsg(c, errors.ErrForbidden, renSensitiveFieldMsg)
		}
		req.DisplayOrder = nil
	}

	client, err := h.siteService.UpdateOAuthClient(
		c.Context(),
		clientID,
		req.Name,
		req.RedirectURIs,
		req.Grants,
		req.AllowedScopes,
		req.AutoConsent,
		req.RefreshTokenTTLSeconds,
		req.Listed,
		req.LogoURL,
		req.Tagline,
		req.DisplayOrder,
	)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, toOAuthClientResponse(client, callerUserID(c)))
}

func (h *SiteHandler) UpdateClientStorage(c fiber.Ctx) error {
	clientID := c.Params("id")
	if clientID == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}

	var req dto.UpdateClientStorageRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	cur, err := h.siteService.GetOAuthClient(c.Context(), clientID)
	if err != nil {
		return response.NotFound(c, errors.ErrOperationFailed)
	}
	if !mayManage(h.managesAll(c), callerUserID(c), cur.CreatedByUserID) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, notOwnerMsg)
	}
	if !perm.Resolver.Can(callerRoles(c), perm.ClientsStorageConfig) {
		if (req.ArtifactEnabled && !cur.ArtifactEnabled) || (req.ImageEnabled && !cur.ImageEnabled) {
			return response.ForbiddenMsg(c, errors.ErrForbidden, renSensitiveFieldMsg)
		}
	}

	client, err := h.siteService.UpdateOAuthClientStorage(c.Context(), clientID, service.StorageConfig{
		ArtifactEnabled:         req.ArtifactEnabled,
		ArtifactSiteKey:         req.ArtifactSiteKey,
		ArtifactCDNBase:         req.ArtifactCDNBase,
		ArtifactAllowedMime:     req.ArtifactAllowedMime,
		ArtifactMaxFileSize:     req.ArtifactMaxFileSize,
		ArtifactQuotaDaily:      req.ArtifactQuotaDaily,
		ArtifactQuotaBytesDaily: req.ArtifactQuotaBytesDaily,
		ImageEnabled:            req.ImageEnabled,
		ImageSiteKey:            req.ImageSiteKey,
		ImageCDNBase:            req.ImageCDNBase,
		ImageAllowedPresets:     req.ImageAllowedPresets,
		ImageMaxFileSize:        req.ImageMaxFileSize,
		ImageQuotaDaily:         req.ImageQuotaDaily,
		ImageQuotaBytesDaily:    req.ImageQuotaBytesDaily,
	})
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, toOAuthClientResponse(client, callerUserID(c)))
}

func (h *SiteHandler) DeleteClient(c fiber.Ctx) error {
	clientID := c.Params("id")
	if clientID == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}

	cur, err := h.siteService.GetOAuthClient(c.Context(), clientID)
	if err != nil {
		return response.NotFound(c, errors.ErrOperationFailed)
	}
	if !mayManage(h.managesAll(c), callerUserID(c), cur.CreatedByUserID) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, notOwnerMsg)
	}

	switch err := h.siteService.DeleteOAuthClient(c.Context(), clientID); {
	case goerrors.Is(err, devapi.ErrAppNotArchived):
		return response.Error(c, fiber.StatusConflict, errors.ErrValidationFailed,
			"this is a developer application — archive it from the developer console before deleting it")
	case goerrors.Is(err, devapi.ErrAppHasReferences):
		return response.Error(c, fiber.StatusConflict, errors.ErrValidationFailed,
			"this client has keys, usage, store links or logins behind it, or can sign users in — it cannot be deleted")
	case err != nil:
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, nil)
}

func toOAuthClientResponse(cl *siteModel.OAuthClient, callerID uint) dto.OAuthClientResponse {
	var redirectURIs []string
	_ = json.Unmarshal(cl.RedirectURIs, &redirectURIs)

	var grants []string
	_ = json.Unmarshal(cl.Grants, &grants)

	var allowedScopes []string
	if len(cl.AllowedScopes) > 0 {
		_ = json.Unmarshal(cl.AllowedScopes, &allowedScopes)
	}

	var artifactMime []string
	if len(cl.ArtifactAllowedMime) > 0 {
		_ = json.Unmarshal(cl.ArtifactAllowedMime, &artifactMime)
	}
	var imagePresets []string
	if len(cl.ImageAllowedPresets) > 0 {
		_ = json.Unmarshal(cl.ImageAllowedPresets, &imagePresets)
	}

	return dto.OAuthClientResponse{
		ID:                     cl.ID,
		SiteID:                 cl.SiteID,
		Name:                   cl.Name,
		RedirectURIs:           redirectURIs,
		Grants:                 grants,
		AllowedScopes:          allowedScopes,
		IsPublic:               cl.IsPublic,
		AutoConsent:            cl.AutoConsent,
		RefreshTokenTTLSeconds: cl.RefreshTokenTTLSeconds,
		Listed:                 cl.Listed,
		LogoURL:                cl.LogoURL,
		Tagline:                cl.Tagline,
		DisplayOrder:           cl.DisplayOrder,
		CreatedAt:              cl.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		CreatedByUserID:        cl.CreatedByUserID,
		OwnerUserID:            cl.OwnerUserID,
		DevEnabled:             cl.DevEnabled,
		Mine:                   isMine(callerID, cl),
		Storage: dto.OAuthClientStorageConfig{
			ArtifactEnabled:         cl.ArtifactEnabled,
			ArtifactSiteKey:         cl.ArtifactSiteKey,
			ArtifactCDNBase:         cl.ArtifactCDNBase,
			ArtifactAllowedMime:     artifactMime,
			ArtifactMaxFileSize:     cl.ArtifactMaxFileSize,
			ArtifactQuotaDaily:      cl.ArtifactQuotaDaily,
			ArtifactQuotaBytesDaily: cl.ArtifactQuotaBytesDaily,
			ImageEnabled:            cl.ImageEnabled,
			ImageSiteKey:            cl.ImageSiteKey,
			ImageCDNBase:            cl.ImageCDNBase,
			ImageAllowedPresets:     imagePresets,
			ImageMaxFileSize:        cl.ImageMaxFileSize,
			ImageQuotaDaily:         cl.ImageQuotaDaily,
			ImageQuotaBytesDaily:    cl.ImageQuotaBytesDaily,
		},
	}
}
