package handler

import (
	"context"
	"strconv"
	"time"

	"api/internal/platform/store/service"
	"api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

// OwnerApps hands the portal panel the applications one signed-in owner holds.
// It is a function rather than a devapi dependency so the store domain never
// reads the developer-platform tables itself.
type OwnerApps func(ctx context.Context, ownerUserID uint) ([]service.OwnerApp, error)

type DevHandler struct {
	svc  *service.Service
	apps OwnerApps
}

func NewDevHandler(svc *service.Service, apps OwnerApps) *DevHandler {
	return &DevHandler{svc: svc, apps: apps}
}

func (h *DevHandler) Register(r fiber.Router) {
	r.Get("/store/usage", h.Usage)
	r.Get("/store/coupons", h.Coupons)
	r.Post("/store/coupons/:id/delivered", h.SetDelivered)
}

// owner answers the request itself when it returns false.
func owner(c fiber.Ctx) (uint, bool, error) {
	ownerID, ok := c.Locals("user_id").(uint)
	if !ok || ownerID == 0 {
		return 0, false, response.Unauthorized(c, errors.ErrAuthUnauthorized)
	}
	return ownerID, true, nil
}

// ownerApps answers the request itself when it returns false.
func (h *DevHandler) ownerApps(c fiber.Ctx) ([]service.OwnerApp, bool, error) {
	ownerID, ok, err := owner(c)
	if !ok {
		return nil, false, err
	}
	apps, err := h.apps(c.Context(), ownerID)
	if err != nil {
		return nil, false, response.InternalError(c, errors.ErrOperationFailed)
	}
	return apps, true, nil
}

func (h *DevHandler) Usage(c fiber.Ctx) error {
	apps, ok, err := h.ownerApps(c)
	if !ok {
		return err
	}
	days, _ := strconv.Atoi(c.Query("days"))
	summary, err := h.svc.OwnerUsage(c.Context(), apps, days)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, summary)
}

func (h *DevHandler) Coupons(c fiber.Ctx) error {
	ownerID, ok, err := owner(c)
	if !ok {
		return err
	}
	out, err := h.svc.OwnerCoupons(c.Context(), ownerID)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, out)
}

type deliveredRequest struct {
	Delivered bool `json:"delivered"`
}

func (h *DevHandler) SetDelivered(c fiber.Ctx) error {
	ownerID, ok, err := owner(c)
	if !ok {
		return err
	}
	id, ok := batchID(c)
	if !ok {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	var req deliveredRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := h.svc.SetCouponDelivered(c.Context(), ownerID, id, req.Delivered, time.Now()); err != nil {
		return couponError(c, err)
	}
	return response.Success(c, nil)
}
