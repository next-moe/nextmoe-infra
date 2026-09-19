package handler

import (
	"context"
	"errors"
	"strconv"
	"time"

	"api/internal/platform/store/service"
	apperr "api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

// AdminApps resolves the sites behind a set of client IDs. devapi owns those
// rows, so the operator console passes a function rather than the store
// domain reading the developer-platform tables.
type AdminApps func(ctx context.Context, clientIDs []string) ([]service.AdminApp, error)

type AdminHandler struct {
	svc  *service.Service
	apps AdminApps
	// canManageCoupons reports whether the signed-in operator may see and
	// change coupon batches; the usage view says so, so the console can hide
	// what the coupon routes would refuse.
	canManageCoupons func(c fiber.Ctx) bool
}

func NewAdminHandler(svc *service.Service, apps AdminApps, canManageCoupons func(c fiber.Ctx) bool) *AdminHandler {
	return &AdminHandler{svc: svc, apps: apps, canManageCoupons: canManageCoupons}
}

// Register mounts the click view on r and gates every coupon route with
// couponGate: the codes are value, so only the platform owner handles them.
func (h *AdminHandler) Register(r fiber.Router, couponGate fiber.Handler) {
	r.Get("/store/usage", h.Usage)
	r.Get("/store/coupon-batches", couponGate, h.ListBatches)
	r.Post("/store/coupon-batches", couponGate, h.CreateBatch)
	r.Get("/store/coupon-batches/:id", couponGate, h.BatchDetail)
	r.Post("/store/coupon-batches/:id/publish", couponGate, h.PublishBatch)
	r.Delete("/store/coupon-batches/:id", couponGate, h.DeleteBatch)
}

type adminUsageView struct {
	*service.AdminUsage
	CanManageCoupons bool `json:"can_manage_coupons"`
}

func (h *AdminHandler) Usage(c fiber.Ctx) error {
	from, to, err := service.ResolveAdminRange(time.Now(), c.Query("from"), c.Query("to"))
	if err != nil {
		return response.BadRequestMsg(c, apperr.ErrInvalidParam,
			"from/to 要是 YYYY-MM-DD 的 JST 日期，起不晚于止，跨度不超过 366 天")
	}
	apps, err := h.storeApps(c.Context())
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	usage, err := h.svc.AdminUsage(c.Context(), apps, from, to)
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, adminUsageView{AdminUsage: usage, CanManageCoupons: h.canManageCoupons(c)})
}

func (h *AdminHandler) storeApps(ctx context.Context) ([]service.AdminApp, error) {
	ids, err := h.svc.StoreClientIDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []service.AdminApp{}, nil
	}
	return h.apps(ctx, ids)
}

func (h *AdminHandler) ListBatches(c fiber.Ctx) error {
	batches, err := h.svc.ListCouponBatches(c.Context())
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	return response.Success(c, batches)
}

func (h *AdminHandler) CreateBatch(c fiber.Ctx) error {
	var in service.CreateBatchInput
	if err := c.Bind().JSON(&in); err != nil {
		return response.BadRequest(c, apperr.ErrBadRequest)
	}
	actor, _ := c.Locals("user_id").(uint)
	batch, err := h.svc.CreateCouponBatch(c.Context(), actor, in)
	if err != nil {
		return couponError(c, err)
	}
	return response.Success(c, batch)
}

func (h *AdminHandler) BatchDetail(c fiber.Ctx) error {
	id, ok := batchID(c)
	if !ok {
		return response.BadRequest(c, apperr.ErrInvalidID)
	}
	apps, err := h.storeApps(c.Context())
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	detail, err := h.svc.CouponBatchDetail(c.Context(), id, apps)
	if err != nil {
		return couponError(c, err)
	}
	return response.Success(c, detail)
}

type publishRequest struct {
	Grants []service.GrantInput `json:"grants"`
}

func (h *AdminHandler) PublishBatch(c fiber.Ctx) error {
	id, ok := batchID(c)
	if !ok {
		return response.BadRequest(c, apperr.ErrInvalidID)
	}
	var req publishRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, apperr.ErrBadRequest)
	}
	apps, err := h.storeApps(c.Context())
	if err != nil {
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
	if err := h.svc.PublishCouponBatch(c.Context(), id, apps, req.Grants, time.Now()); err != nil {
		return couponError(c, err)
	}
	return response.Success(c, nil)
}

func (h *AdminHandler) DeleteBatch(c fiber.Ctx) error {
	id, ok := batchID(c)
	if !ok {
		return response.BadRequest(c, apperr.ErrInvalidID)
	}
	if err := h.svc.DeleteCouponBatch(c.Context(), id); err != nil {
		return couponError(c, err)
	}
	return response.Success(c, nil)
}

func batchID(c fiber.Ctx) (int64, bool) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	return id, err == nil && id > 0
}

func couponError(c fiber.Ctx, err error) error {
	var input *service.InputError
	switch {
	case errors.As(err, &input):
		return response.BadRequestMsg(c, apperr.ErrValidationFailed, input.Msg)
	case errors.Is(err, service.ErrBatchNotFound), errors.Is(err, service.ErrCouponNotFound):
		return response.NotFound(c, apperr.ErrNotFound)
	case errors.Is(err, service.ErrBatchNotDraft):
		return response.Error(c, fiber.StatusConflict, apperr.ErrOperationFailed, "这批券已经发布，不能再改")
	default:
		return response.InternalError(c, apperr.ErrOperationFailed)
	}
}
