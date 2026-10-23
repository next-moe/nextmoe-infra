package handler

import (
	"context"
	stderrors "errors"
	"strconv"
	"strings"

	"api/internal/middleware"
	"api/internal/platform/news/dto"
	"api/internal/platform/news/model"
	"api/internal/platform/news/perm"
	"api/internal/platform/news/service"
	siteModel "api/internal/platform/site/model"
	"api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

const (
	AdminPrefix = "/api/v1/admin/news"

	msgBadStatus     = "status must be a comma-separated subset of: 0,1,2,3"
	msgBadAction     = "action must be one of: publish, reject, withdraw, repend"
	msgBadTransition = "that action is not legal from the item's current status"
	msgNoActor       = "the acting moderator could not be identified"
)

type OAuthClientLookup interface {
	FindByClientID(ctx context.Context, clientID string) (*siteModel.OAuthClient, error)
}

type AdminHandler struct {
	svc *service.AdminService
}

func NewAdminHandler(svc *service.AdminService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// AdminGate guards the whole prefix: a first-party client, then news.review.
// The third-party refusal is spelled out here rather than imported from the
// catalog gate because a platform domain must not depend on another domain's
// handler package; the rule itself is one field (an owned client is a
// third-party app) and is asserted in this package's tests.
func AdminGate(clients OAuthClientLookup) fiber.Handler {
	review := middleware.RequirePermission(perm.Resolver, perm.Review)
	return func(c fiber.Ctx) error {
		if clientID, _ := c.Locals("token_client_id").(string); clientID != "" {
			client, err := clients.FindByClientID(c.Context(), clientID)
			if err != nil || client == nil {
				return response.ForbiddenMsg(c, errors.ErrForbidden,
					"the access token's client is not registered")
			}
			if IsThirdPartyClient(client) {
				return response.ForbiddenMsg(c, errors.ErrForbidden,
					"a third-party application is not a moderation surface; the news admin face needs a first-party client")
			}
		}
		return review(c)
	}
}

// IsThirdPartyClient: an owned client belongs to a developer, not to us.
func IsThirdPartyClient(client *siteModel.OAuthClient) bool {
	return client != nil && client.OwnerUserID != nil
}

func (h *AdminHandler) offline(c fiber.Ctx) bool {
	if h.svc != nil {
		return false
	}
	_ = response.Error(c, fiber.StatusServiceUnavailable, errors.ErrInternalServer, msgOffline)
	return true
}

func (h *AdminHandler) Queue(c fiber.Ctx) error {
	if h.offline(c) {
		return nil
	}
	f := service.QueueFilter{
		Lanes:    parseCSV(c.Query("lane")),
		Sources:  parseCSV(c.Query("source")),
		Ungraded: c.Query("ungraded") == "true",
		Degraded: c.Query("degraded") == "true",
	}
	if !lanesKnown(f.Lanes) {
		return response.BadRequestMsg(c, errors.ErrInvalidParam, msgBadLane)
	}
	statuses, ok := parseStatuses(c.Query("status"))
	if !ok {
		return response.BadRequestMsg(c, errors.ErrInvalidParam, msgBadStatus)
	}
	f.Statuses = statuses

	limit, ok := parseAdminLimit(c.Query("limit"))
	if !ok {
		return response.BadRequestMsg(c, errors.ErrInvalidParam, msgBadLimit)
	}
	offset, ok := parseOffset(c.Query("offset"))
	if !ok {
		return response.BadRequestMsg(c, errors.ErrInvalidParam, msgBadLimit)
	}

	data, err := h.svc.Queue(c.Context(), f, offset, limit)
	if err != nil {
		return response.InternalError(c, errors.ErrInternalServer)
	}
	return response.Success(c, data)
}

func (h *AdminHandler) Detail(c fiber.Ctx) error {
	if h.offline(c) {
		return nil
	}
	id, ok := parseID(c.Params("id"))
	if !ok {
		return response.NotFound(c, errors.ErrNotFound)
	}
	data, err := h.svc.Item(c.Context(), id)
	if err != nil {
		if stderrors.Is(err, service.ErrNotFound) {
			return response.NotFound(c, errors.ErrNotFound)
		}
		return response.InternalError(c, errors.ErrInternalServer)
	}
	return response.Success(c, data)
}

func (h *AdminHandler) Decide(c fiber.Ctx) error {
	if h.offline(c) {
		return nil
	}
	id, ok := parseID(c.Params("id"))
	if !ok {
		return response.NotFound(c, errors.ErrNotFound)
	}
	var body dto.AdminDecisionRequest
	if err := c.Bind().JSON(&body); err != nil {
		return response.BadRequestMsg(c, errors.ErrInvalidParam, msgBadAction)
	}
	// An unattributable decision is not a decision: the whole point of the audit
	// line is that someone stood behind publishing a partner's words under our
	// name.
	actor, ok := c.Locals("user_id").(uint)
	if !ok || actor == 0 {
		return response.Error(c, fiber.StatusForbidden, errors.ErrForbidden, msgNoActor)
	}

	data, err := h.svc.Decide(c.Context(), id, int64(actor),
		strings.TrimSpace(body.Action), strings.TrimSpace(body.Reason))
	switch {
	case err == nil:
		return response.Success(c, data)
	case stderrors.Is(err, service.ErrNotFound):
		return response.NotFound(c, errors.ErrNotFound)
	case stderrors.Is(err, service.ErrUnknownAction):
		return response.BadRequestMsg(c, errors.ErrInvalidParam, msgBadAction)
	case stderrors.Is(err, service.ErrIllegalTransition):
		return response.Error(c, fiber.StatusConflict, errors.ErrInvalidParam, msgBadTransition)
	default:
		return response.InternalError(c, errors.ErrInternalServer)
	}
}

func (h *AdminHandler) Stats(c fiber.Ctx) error {
	if h.offline(c) {
		return nil
	}
	data, err := h.svc.Stats(c.Context())
	if err != nil {
		return response.InternalError(c, errors.ErrInternalServer)
	}
	return response.Success(c, data)
}

func parseID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0
}

// parseStatuses rejects an unknown status for the same reason lanesKnown rejects
// an unknown lane: filtering on one silently returns an empty queue, and an
// empty moderation queue is exactly the answer nobody double-checks.
func parseStatuses(raw string) ([]int16, bool) {
	parts := parseCSV(raw)
	if len(parts) == 0 {
		return nil, true
	}
	out := make([]int16, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		s := int16(n)
		switch s {
		case model.StatusPending, model.StatusPublished, model.StatusRejected, model.StatusWithdrawn:
			out = append(out, s)
		default:
			return nil, false
		}
	}
	return out, true
}

func parseAdminLimit(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 50, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 200 {
		return 0, false
	}
	return n, true
}

func parseOffset(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
