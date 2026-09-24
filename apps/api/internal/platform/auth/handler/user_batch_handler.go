package handler

import (
	stderrors "errors"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf8"

	"api/internal/middleware"
	"api/internal/platform/auth/service"
	"api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

type UserBatchHandler struct {
	svc *service.UserBatchService
}

func NewUserBatchHandler(svc *service.UserBatchService) *UserBatchHandler {
	return &UserBatchHandler{svc: svc}
}

func (h *UserBatchHandler) Get(c fiber.Ctx) error {
	idsStr := c.Query("ids")
	ids, err := parseUintList(idsStr)
	if err != nil || len(ids) == 0 {
		return response.BadRequestMsg(c, errors.ErrInvalidParam, "ids is required (comma-separated unsigned integers)")
	}
	if len(ids) > 100 {
		return response.BadRequestMsg(c, errors.ErrInvalidParam, "ids: max 100 per request")
	}

	resp, err := h.svc.GetBriefs(c.Context(), ids, callerSite(c))
	if err != nil {
		slog.Error("users batch query", "ids_len", len(ids), "err", err)
		return response.InternalError(c, errors.ErrInternalServer)
	}
	return response.Success(c, resp)
}

func (h *UserBatchHandler) Deleted(c fiber.Ctx) error {
	limit := 100
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			return response.BadRequestMsg(c, errors.ErrInvalidParam, "limit: 1..500")
		}
		limit = n
	}
	resp, err := h.svc.ListDeleted(c.Context(), c.Query("cursor"), limit)
	if stderrors.Is(err, service.ErrBadDeletedCursor) {
		return response.BadRequestMsg(c, errors.ErrInvalidParam, "cursor: pass back next_cursor unchanged")
	}
	if err != nil {
		slog.Error("users deleted feed", "err", err)
		return response.InternalError(c, errors.ErrInternalServer)
	}
	return response.Success(c, resp)
}

func (h *UserBatchHandler) Search(c fiber.Ctx) error {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		return response.BadRequestMsg(c, errors.ErrInvalidParam, "q is required")
	}
	if utf8.RuneCountInString(q) > 50 {
		return response.BadRequestMsg(c, errors.ErrInvalidParam, "q: max 50 chars")
	}

	limit := 20
	if s := c.Query("limit"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 {
			return response.BadRequestMsg(c, errors.ErrInvalidParam, "limit must be a positive integer")
		}
		limit = v
	}
	if limit > 50 {
		limit = 50
	}

	resp, err := h.svc.SearchByName(c.Context(), q, limit, callerSite(c))
	if err != nil {
		slog.Error("users search", "q", q, "limit", limit, "err", err)
		return response.InternalError(c, errors.ErrInternalServer)
	}
	return response.Success(c, resp)
}

func parseUintList(s string) ([]uint, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]uint, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, uint(v))
	}
	return out, nil
}

func callerSite(c fiber.Ctx) uint {
	if client := middleware.OAuthClientFromCtx(c); client != nil && client.SiteID != nil {
		return *client.SiteID
	}
	return 0
}
