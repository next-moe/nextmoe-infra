package handler

import (
	"context"
	"strconv"

	"api/internal/middleware"
	authModel "api/internal/platform/auth/model"
	"api/internal/platform/ledger/model"
	"api/internal/platform/ledger/service"
	"api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

type userFinder interface {
	FindByUUID(ctx context.Context, uuid string) (*authModel.User, error)
}

type Handler struct {
	ledger *service.Ledger
	users  userFinder
}

func New(ledger *service.Ledger, users userFinder) *Handler {
	return &Handler{ledger: ledger, users: users}
}

type adjustRequest struct {
	Delta          int64  `json:"delta"`
	Reason         string `json:"reason"`
	Ref            string `json:"ref"`
	ActorUserID    uint   `json:"actor_user_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Note           string `json:"note"`
}

func isAwardReason(r string) bool {
	switch r {
	case model.ReasonContentApproved, model.ReasonContentRemoved,
		model.ReasonDailyCheckin, model.ReasonLiked:
		return true
	}
	return false
}

func (h *Handler) Adjust(c fiber.Ctx) error {
	userID, client, deny := s2sTarget(c)
	if deny != nil {
		return deny()
	}
	var req adjustRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if !isAwardReason(req.Reason) {
		return response.BadRequest(c, errors.ErrMoemoepointInvalidReason)
	}
	res, err := h.ledger.Award(c.Context(), service.Award{
		UserID:         userID,
		Delta:          req.Delta,
		Reason:         req.Reason,
		SourceApp:      client,
		Ref:            req.Ref,
		ActorUserID:    req.ActorUserID,
		IdempotencyKey: req.IdempotencyKey,
		Note:           req.Note,
	})
	return respondPosted(c, userID, res, err)
}

type chargeRequest struct {
	Amount         int64  `json:"amount"`
	Ref            string `json:"ref"`
	IdempotencyKey string `json:"idempotency_key"`
	Note           string `json:"note"`
}

func (h *Handler) Charge(c fiber.Ctx) error {
	userID, client, deny := s2sTarget(c)
	if deny != nil {
		return deny()
	}
	var req chargeRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	res, err := h.ledger.Charge(c.Context(), service.Charge{
		UserID:         userID,
		Amount:         req.Amount,
		Reason:         model.ReasonSpend,
		SourceApp:      client,
		Ref:            req.Ref,
		ActorUserID:    userID,
		IdempotencyKey: req.IdempotencyKey,
		Note:           req.Note,
	})
	return respondPosted(c, userID, res, err)
}

type reverseRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	Note           string `json:"note"`
}

func (h *Handler) Reverse(c fiber.Ctx) error {
	userID, client, deny := s2sTarget(c)
	if deny != nil {
		return deny()
	}
	var req reverseRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if req.IdempotencyKey == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}
	res, err := h.ledger.Reverse(c.Context(), service.Reversal{
		SourceApp:      client,
		IdempotencyKey: req.IdempotencyKey,
		UserID:         userID,
		Note:           req.Note,
	})
	return respondPosted(c, userID, res, err)
}

// s2sTarget authorises a write to the ledger: only a client on the awarder
// allow-list may move a user's moemoepoints, in either direction.
func s2sTarget(c fiber.Ctx) (uint, string, func() error) {
	userID, err := parseUintParam(c, "id")
	if err != nil {
		return 0, "", func() error { return response.BadRequest(c, errors.ErrInvalidID) }
	}
	client := middleware.OAuthClientFromCtx(c)
	if client == nil {
		return 0, "", func() error { return response.Unauthorized(c, errors.ErrAuthUnauthorized) }
	}
	if !client.MoemoepointAwarder {
		return 0, "", func() error { return response.Forbidden(c, errors.ErrMoemoepointNotAwarder) }
	}
	return userID, client.ID, nil
}

func (h *Handler) GetBalance(c fiber.Ctx) error {
	userID, err := parseUintParam(c, "id")
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	balance, err := h.ledger.UserBalance(c.Context(), userID)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, fiber.Map{"user_id": userID, "balance": balance})
}

func (h *Handler) GetLog(c fiber.Ctx) error {
	userID, err := parseUintParam(c, "id")
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	return h.respondLog(c, userID, false)
}

func (h *Handler) MyLog(c fiber.Ctx) error {
	userID, _ := c.Locals("user_id").(uint)
	if userID == 0 {
		return response.Unauthorized(c, errors.ErrAuthUnauthorized)
	}
	return h.respondLog(c, userID, false)
}

type adminAdjustRequest struct {
	Delta          int64  `json:"delta"`
	Note           string `json:"note"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (h *Handler) AdminAdjust(c fiber.Ctx) error {
	u, err := h.users.FindByUUID(c.Context(), c.Params("uuid"))
	if err != nil {
		return response.NotFound(c, errors.ErrAuthUserNotFound)
	}
	adminID, _ := c.Locals("user_id").(uint)

	var req adminAdjustRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	reason := model.ReasonAdminGrant
	if req.Delta < 0 {
		reason = model.ReasonAdminDeduct
	}
	res, err := h.ledger.Award(c.Context(), service.Award{
		UserID:         u.ID,
		Delta:          req.Delta,
		Reason:         reason,
		SourceApp:      "oauth",
		Ref:            "admin:" + strconv.FormatUint(uint64(adminID), 10),
		ActorUserID:    adminID,
		IdempotencyKey: req.IdempotencyKey,
		Note:           req.Note,
	})
	return respondPosted(c, u.ID, res, err)
}

func (h *Handler) AdminGetLog(c fiber.Ctx) error {
	u, err := h.users.FindByUUID(c.Context(), c.Params("uuid"))
	if err != nil {
		return response.NotFound(c, errors.ErrAuthUserNotFound)
	}
	return h.respondLog(c, u.ID, true)
}

func (h *Handler) respondLog(c fiber.Ctx, userID uint, full bool) error {
	limit, _ := strconv.Atoi(c.Query("limit"))
	beforeID, _ := strconv.ParseInt(c.Query("before_id"), 10, 64)
	rows, hasMore, err := h.ledger.UserHistory(c.Context(), userID, limit, beforeID, c.Query("reason"))
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	apps := make([]string, len(rows))
	for i, r := range rows {
		apps[i] = r.SourceApp
	}
	names := h.ledger.SourceNames(c.Context(), apps)
	items := make([]fiber.Map, len(rows))
	for i, r := range rows {
		m := fiber.Map{
			"id": r.ID, "delta": r.Delta, "balance_after": r.BalanceAfter, "reason": r.Reason,
			"source_app": r.SourceApp, "source_name": names[r.SourceApp],
			"ref": r.Ref, "created_at": r.CreatedAt,
		}
		if full {
			m["note"] = r.Note
			m["actor_user_id"] = r.ActorUserID
		}
		items[i] = m
	}
	return response.Success(c, fiber.Map{"items": items, "has_more": hasMore})
}

func respondPosted(c fiber.Ctx, userID uint, res *service.Posted, err error) error {
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, fiber.Map{
		"user_id": userID, "balance": res.UserBalance(userID), "applied": res.Applied,
	})
}

func parseUintParam(c fiber.Ctx, name string) (uint, error) {
	n, err := strconv.ParseUint(c.Params(name), 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(n), nil
}

func respondErr(c fiber.Ctx, err error) error {
	if appErr, ok := err.(*errors.AppError); ok {
		switch appErr.Code {
		case errors.ErrAuthUserNotFound, errors.ErrMoemoepointTransferNotFound:
			return response.NotFound(c, appErr.Code)
		case errors.ErrMoemoepointInvalidDelta, errors.ErrMoemoepointInvalidReason,
			errors.ErrMoemoepointIdemConflict, errors.ErrMoemoepointInsufficient,
			errors.ErrMoemoepointNotReversible, errors.ErrMissingParam:
			return response.BadRequest(c, appErr.Code)
		}
	}
	return response.InternalError(c, errors.ErrOperationFailed)
}
