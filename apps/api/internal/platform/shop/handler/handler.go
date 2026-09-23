package handler

import (
	"context"
	"io"
	"log/slog"
	"strconv"

	authModel "api/internal/platform/auth/model"
	"api/internal/platform/shop/perm"
	"api/internal/platform/shop/service"
	"api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

type userFinder interface {
	FindByUUID(ctx context.Context, uuid string) (*authModel.User, error)
}

type Handler struct {
	shop  *service.Shop
	users userFinder
}

func New(shop *service.Shop, users userFinder) *Handler {
	return &Handler{shop: shop, users: users}
}

func (h *Handler) Catalog(c fiber.Ctx) error {
	offers, err := h.shop.Catalog(c.Context())
	if err != nil {
		return respondErr(c, err)
	}
	c.Set(fiber.HeaderCacheControl, "public, max-age=60")
	return response.Success(c, fiber.Map{"offers": offers})
}

// The shop spends a user's points, so an OAuth access token from some other
// application must not reach it; only the account center's own session may.
func firstPartyUser(c fiber.Ctx) (uint, bool) {
	if client, _ := c.Locals("token_client_id").(string); client != "" {
		return 0, false
	}
	id, _ := c.Locals("user_id").(uint)
	return id, id != 0
}

func (h *Handler) Inventory(c fiber.Ctx) error {
	userID, ok := firstPartyUser(c)
	if !ok {
		return response.Forbidden(c, errors.ErrShopFirstPartyOnly)
	}
	inv, err := h.shop.Inventory(c.Context(), userID)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, inv)
}

type purchaseRequest struct {
	OfferID        int64  `json:"offer_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (h *Handler) Purchase(c fiber.Ctx) error {
	userID, ok := firstPartyUser(c)
	if !ok {
		return response.Forbidden(c, errors.ErrShopFirstPartyOnly)
	}
	var req purchaseRequest
	if err := c.Bind().JSON(&req); err != nil || req.OfferID == 0 {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	out, err := h.shop.Purchase(c.Context(), userID, req.OfferID, req.IdempotencyKey)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

type equipRequest struct {
	Slot   string `json:"slot"`
	SiteID uint   `json:"site_id"`
	ItemID *int64 `json:"item_id"`
}

func (h *Handler) Equip(c fiber.Ctx) error {
	userID, ok := firstPartyUser(c)
	if !ok {
		return response.Forbidden(c, errors.ErrShopFirstPartyOnly)
	}
	var req equipRequest
	if err := c.Bind().JSON(&req); err != nil || req.Slot == "" {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := h.shop.Equip(c.Context(), userID, req.Slot, req.SiteID, req.ItemID); err != nil {
		return respondErr(c, err)
	}
	cosmetics, err := h.shop.CosmeticsFor(c.Context(), []uint{userID}, req.SiteID)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, fiber.Map{"cosmetics": cosmetics[userID]})
}

func (h *Handler) UploadAsset(c fiber.Ctx) error {
	fh, err := c.FormFile("file")
	if err != nil || fh == nil {
		return response.BadRequest(c, errors.ErrMissingParam)
	}
	f, err := fh.Open()
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 3<<20))
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	by, _ := c.Locals("user_id").(uint)
	a, err := h.shop.UploadAsset(c.Context(), data, by)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, a)
}

func (h *Handler) ListAssets(c fiber.Ctx) error {
	assets, err := h.shop.ListAssets(c.Context())
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, fiber.Map{"assets": assets})
}

func (h *Handler) ListItems(c fiber.Ctx) error {
	items, err := h.shop.ListItems(c.Context(), c.Query("status"))
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, fiber.Map{"items": items})
}

func (h *Handler) CreateItem(c fiber.Ctx) error {
	var in service.ItemInput
	if err := c.Bind().JSON(&in); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	by, _ := c.Locals("user_id").(uint)
	out, err := h.shop.CreateItem(c.Context(), in, by)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

func (h *Handler) UpdateItem(c fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	var in service.ItemInput
	if err := c.Bind().JSON(&in); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	out, err := h.shop.UpdateItem(c.Context(), id, in)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

func (h *Handler) TransitionItem(c fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	action := service.ItemAction(c.Params("action"))
	switch action {
	case service.ItemSubmit, service.ItemReject, service.ItemPublish, service.ItemRetire, service.ItemRelist:
	default:
		return response.BadRequest(c, errors.ErrShopInvalidTransition)
	}
	if action.NeedsPublisher() && !canPublish(c) {
		return response.Forbidden(c, errors.ErrForbidden)
	}
	out, err := h.shop.TransitionItem(c.Context(), id, action)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

func (h *Handler) DeleteItem(c fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	if err := h.shop.DeleteItem(c.Context(), id); err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, nil)
}

func (h *Handler) ListOffers(c fiber.Ctx) error {
	offers, err := h.shop.ListOffers(c.Context(), c.Query("status"))
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, fiber.Map{"offers": offers})
}

func (h *Handler) CreateOffer(c fiber.Ctx) error {
	var in service.OfferInput
	if err := c.Bind().JSON(&in); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	by, _ := c.Locals("user_id").(uint)
	out, err := h.shop.CreateOffer(c.Context(), in, by)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

func (h *Handler) UpdateOffer(c fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	var in service.OfferInput
	if err := c.Bind().JSON(&in); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	out, err := h.shop.UpdateOffer(c.Context(), id, in)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

func (h *Handler) TransitionOffer(c fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	action := service.OfferAction(c.Params("action"))
	if action != service.OfferActivate && action != service.OfferRetire {
		return response.BadRequest(c, errors.ErrShopInvalidTransition)
	}
	if !canPublish(c) {
		return response.Forbidden(c, errors.ErrForbidden)
	}
	out, err := h.shop.TransitionOffer(c.Context(), id, action)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

func canPublish(c fiber.Ctx) bool {
	roles, _ := c.Locals("user_roles").([]string)
	return perm.Resolver.Can(roles, perm.Publish)
}

func (h *Handler) userByUUID(c fiber.Ctx) (*authModel.User, error) {
	u, err := h.users.FindByUUID(c.Context(), c.Params("uuid"))
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	return u, nil
}

func (h *Handler) UserShop(c fiber.Ctx) error {
	u, err := h.userByUUID(c)
	if err != nil {
		return respondErr(c, err)
	}
	out, err := h.shop.UserShop(c.Context(), u.ID)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

type grantRequest struct {
	ItemID       int64  `json:"item_id"`
	DurationDays int    `json:"duration_days"`
	Note         string `json:"note"`
}

func (h *Handler) Grant(c fiber.Ctx) error {
	u, err := h.userByUUID(c)
	if err != nil {
		return respondErr(c, err)
	}
	var req grantRequest
	if err := c.Bind().JSON(&req); err != nil || req.ItemID == 0 {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	by, _ := c.Locals("user_id").(uint)
	if err := h.shop.Grant(c.Context(), u.ID, req.ItemID, req.DurationDays, req.Note, by); err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, nil)
}

func (h *Handler) Revoke(c fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	if err := h.shop.Revoke(c.Context(), id); err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, nil)
}

type refundRequest struct {
	Note string `json:"note"`
}

func (h *Handler) Refund(c fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	var req refundRequest
	_ = c.Bind().JSON(&req)
	by, _ := c.Locals("user_id").(uint)
	out, err := h.shop.Refund(c.Context(), id, by, req.Note)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

func respondErr(c fiber.Ctx, err error) error {
	if appErr, ok := err.(*errors.AppError); ok {
		switch appErr.Code {
		case errors.ErrShopItemNotFound, errors.ErrShopOrderNotFound, errors.ErrAuthUserNotFound:
			return response.FromAppError(c, fiber.StatusNotFound, appErr)
		case errors.ErrShopStorageUnavailable:
			return response.FromAppError(c, fiber.StatusServiceUnavailable, appErr)
		case errors.ErrShopOfferUnavailable, errors.ErrShopAlreadyOwned, errors.ErrShopLimitReached,
			errors.ErrShopSoldOut, errors.ErrShopNotOwned, errors.ErrShopInvalidAsset, errors.ErrShopInvalidItem,
			errors.ErrShopInvalidOffer, errors.ErrShopInvalidTransition, errors.ErrShopIdemConflict,
			errors.ErrShopPriceBelowMinimum, errors.ErrMoemoepointInsufficient, errors.ErrMissingParam:
			return response.FromAppError(c, fiber.StatusBadRequest, appErr)
		}
	}
	slog.Error("shop request failed", "path", c.Path(), "err", err)
	return response.InternalError(c, errors.ErrOperationFailed)
}
