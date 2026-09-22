package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/service"
	"api/pkg/errors"
	"api/pkg/response"
	"api/pkg/utils"

	"github.com/gofiber/fiber/v3"
)

const PreferencesScope = "preferences"

type PreferenceHandler struct {
	svc *service.PreferenceService
}

func NewPreferenceHandler(svc *service.PreferenceService) *PreferenceHandler {
	return &PreferenceHandler{svc: svc}
}

// preferenceGate resolves the caller and decides whether it may touch
// namespace ("" for the routes that carry none). status == 0 means allowed.
func preferenceGate(c fiber.Ctx, namespace string) (userID uint, status int, code int) {
	clientID, _ := c.Locals("token_client_id").(string)
	if clientID != "" {
		scope, _ := c.Locals("user_scope").(string)
		if !service.ScopeHolds(scope, PreferencesScope) {
			return 0, fiber.StatusForbidden, errors.ErrPrefScopeRequired
		}
	}
	if namespace != "" {
		if !model.IsPreferenceNamespace(namespace) {
			return 0, fiber.StatusBadRequest, errors.ErrPrefNamespaceInvalid
		}
		if !service.PreferenceNamespaceAllowed(clientID, namespace) {
			return 0, fiber.StatusForbidden, errors.ErrPrefNamespaceDenied
		}
	}
	userID, _ = c.Locals("user_id").(uint)
	if userID == 0 {
		return 0, fiber.StatusUnauthorized, errors.ErrAuthUnauthorized
	}
	return userID, 0, 0
}

func rejectPref(c fiber.Ctx, status, code int) error {
	return response.Error(c, status, code, errors.GetMessage(code))
}

func (h *PreferenceHandler) ConfirmAdult(c fiber.Ctx) error {
	if _, status, code := preferenceGate(c, ""); status != 0 {
		return rejectPref(c, status, code)
	}
	userUUID, _ := c.Locals("user_uuid").(string)

	prefs, err := h.svc.ConfirmAdult(c.Context(), userUUID)
	if err != nil {
		return prefError(c, err)
	}
	return response.Success(c, dto.AdultConfirmationResponse{
		AdultConfirmedAt: formatUTC(prefs.AdultConfirmedAt),
	})
}

func (h *PreferenceHandler) SetNSFWDisplay(c fiber.Ctx) error {
	if _, status, code := preferenceGate(c, ""); status != 0 {
		return rejectPref(c, status, code)
	}

	var req dto.UpdateNSFWDisplayRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequest(c, errors.ErrPrefDisplayInvalid)
	}

	userUUID, _ := c.Locals("user_uuid").(string)
	prefs, err := h.svc.SetNSFWDisplay(c.Context(), userUUID, req.NSFWDisplay)
	if err != nil {
		return prefError(c, err)
	}

	resp := dto.NSFWDisplayResponse{NSFWDisplay: prefs.NSFWDisplay}
	if prefs.AdultConfirmedAt != nil {
		at := formatUTC(prefs.AdultConfirmedAt)
		resp.AdultConfirmedAt = &at
	}
	return response.Success(c, resp)
}

// List is the one route an OAuth token cannot reach at all. A namespace listing
// names every application the account has ever stored something from, which is
// not the caller's business even holding `preferences`; the account console
// needs it, and the account console is first-party.
func (h *PreferenceHandler) List(c fiber.Ctx) error {
	if clientID, _ := c.Locals("token_client_id").(string); clientID != "" {
		return rejectPref(c, fiber.StatusForbidden, errors.ErrPrefNamespaceDenied)
	}
	userID, status, code := preferenceGate(c, "")
	if status != 0 {
		return rejectPref(c, status, code)
	}

	rows, err := h.svc.List(c.Context(), userID)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	out := make([]dto.PreferenceSummaryResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.PreferenceSummaryResponse{
			Namespace: r.Namespace,
			Version:   r.Version,
			UpdatedAt: r.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			SizeBytes: r.SizeBytes,
		})
	}
	return response.Success(c, out)
}

func (h *PreferenceHandler) Get(c fiber.Ctx) error {
	namespace := c.Params("namespace")
	userID, status, code := preferenceGate(c, namespace)
	if status != 0 {
		return rejectPref(c, status, code)
	}

	doc, err := h.svc.Get(c.Context(), userID, namespace)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	c.Set(fiber.HeaderETag, etagOf(doc.Version))
	return response.Success(c, preferenceDocResponse(doc))
}

func (h *PreferenceHandler) Put(c fiber.Ctx) error {
	namespace := c.Params("namespace")
	userID, status, code := preferenceGate(c, namespace)
	if status != 0 {
		return rejectPref(c, status, code)
	}

	var req dto.PutPreferenceRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	expected, ok := parseIfMatch(c.Get(fiber.HeaderIfMatch))
	if !ok {
		return response.BadRequestMsg(c, errors.ErrValidationFailed,
			`If-Match must be a version number, e.g. If-Match: "3"`)
	}

	doc, err := h.svc.Put(c.Context(), userID, namespace, req.Doc, expected)
	if err != nil {
		return prefError(c, err)
	}
	c.Set(fiber.HeaderETag, etagOf(doc.Version))
	return response.Success(c, preferenceDocResponse(doc))
}

func (h *PreferenceHandler) Delete(c fiber.Ctx) error {
	namespace := c.Params("namespace")
	userID, status, code := preferenceGate(c, namespace)
	if status != 0 {
		return rejectPref(c, status, code)
	}

	if err := h.svc.Delete(c.Context(), userID, namespace); err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, nil)
}

func preferenceDocResponse(doc *service.PreferenceDoc) dto.PreferenceDocResponse {
	out := dto.PreferenceDocResponse{
		Namespace: doc.Namespace,
		Doc:       doc.Doc,
		Version:   doc.Version,
	}
	if doc.UpdatedAt != nil {
		at := doc.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
		out.UpdatedAt = &at
	}
	return out
}

func etagOf(version int) string {
	return fmt.Sprintf("%q", strconv.Itoa(version))
}

func parseIfMatch(header string) (*int, bool) {
	raw := strings.TrimSpace(header)
	if raw == "" {
		return nil, true
	}
	raw = strings.TrimPrefix(raw, "W/")
	raw = strings.Trim(raw, `"`)
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return nil, false
	}
	return &v, true
}

func prefError(c fiber.Ctx, err error) error {
	appErr, ok := err.(*errors.AppError)
	if !ok {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	switch appErr.Code {
	case errors.ErrPrefVersionConflict:
		return rejectPref(c, fiber.StatusPreconditionFailed, appErr.Code)
	case errors.ErrPrefDocTooLarge:
		return rejectPref(c, fiber.StatusRequestEntityTooLarge, appErr.Code)
	case errors.ErrAuthUserNotFound:
		return response.NotFound(c, appErr.Code)
	default:
		return response.BadRequest(c, appErr.Code)
	}
}

func formatUTC(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

func adultConfirmedAt(u *model.User) *string {
	if u.AdultConfirmedAt == nil {
		return nil
	}
	at := formatUTC(u.AdultConfirmedAt)
	return &at
}
