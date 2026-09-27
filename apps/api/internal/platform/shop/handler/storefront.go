package handler

import (
	"strconv"

	"api/internal/middleware"
	"api/internal/platform/shop/model"
	"api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

func storefrontSite(c fiber.Ctx) (uint, bool) {
	client := middleware.OAuthClientFromCtx(c)
	if client == nil || !client.MoemoepointAwarder || client.SiteID == nil {
		return 0, false
	}
	return *client.SiteID, true
}

func storefrontCustomer(c fiber.Ctx) (userID, site uint, deny func() error) {
	site, ok := storefrontSite(c)
	if !ok {
		return 0, 0, func() error { return response.Forbidden(c, errors.ErrShopNotStorefront) }
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, 0, func() error { return response.BadRequest(c, errors.ErrInvalidID) }
	}
	return uint(id), site, nil
}

func (h *Handler) Storefront(c fiber.Ctx) error {
	site, ok := storefrontSite(c)
	if !ok {
		return response.Forbidden(c, errors.ErrShopNotStorefront)
	}
	out, err := h.shop.Storefront(c.Context(), site)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

func (h *Handler) CustomerInventory(c fiber.Ctx) error {
	userID, _, deny := storefrontCustomer(c)
	if deny != nil {
		return deny()
	}
	inv, err := h.shop.Inventory(c.Context(), userID)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, inv)
}

func (h *Handler) CustomerPurchase(c fiber.Ctx) error {
	userID, site, deny := storefrontCustomer(c)
	if deny != nil {
		return deny()
	}
	var req purchaseRequest
	if err := c.Bind().JSON(&req); err != nil || req.OfferID == 0 {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	out, err := h.shop.Purchase(c.Context(), userID, req.OfferID, req.IdempotencyKey, site)
	if err != nil {
		return respondErr(c, err)
	}
	return response.Success(c, out)
}

func (h *Handler) CustomerEquip(c fiber.Ctx) error {
	userID, site, deny := storefrontCustomer(c)
	if deny != nil {
		return deny()
	}
	var req equipRequest
	if err := c.Bind().JSON(&req); err != nil || req.Slot == "" {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if req.SiteID != model.EverySite && req.SiteID != site {
		return response.Forbidden(c, errors.ErrForbidden)
	}
	return h.equip(c, userID, req)
}
