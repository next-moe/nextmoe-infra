package envconfig

import (
	"errors"
	"log/slog"
	"strconv"

	apperrors "api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	svc      *Service
	canWrite func(roles []string) bool
}

func NewHandler(svc *Service, canWrite func(roles []string) bool) *Handler {
	return &Handler{svc: svc, canWrite: canWrite}
}

func (h *Handler) Register(admin fiber.Router, view fiber.Handler, write fiber.Handler) {
	g := admin.Group("/settings/env", view)
	g.Get("", h.Overview)
	g.Get("/audit", h.Audit)
	g.Put("/:key", write, h.Set)
	g.Delete("/:key", write, h.Reset)
}

func (h *Handler) Overview(c fiber.Ctx) error {
	roles, _ := c.Locals("user_roles").([]string)
	ov, err := h.svc.Overview(c.Context(), h.canWrite(roles))
	if err != nil {
		slog.Error("envconfig: overview failed", "err", err)
		return response.InternalError(c, apperrors.ErrOperationFailed)
	}
	return response.Success(c, ov)
}

func (h *Handler) Audit(c fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit"))
	entries, err := h.svc.Audit(c.Context(), limit)
	if err != nil {
		slog.Error("envconfig: audit read failed", "err", err)
		return response.InternalError(c, apperrors.ErrOperationFailed)
	}
	return response.Success(c, entries)
}

type setRequest struct {
	Scope   string `json:"scope"`
	Value   string `json:"value"`
	Note    string `json:"note"`
	Version *int64 `json:"version"`
}

func (h *Handler) Set(c fiber.Ctx) error {
	var req setRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, apperrors.ErrBadRequest)
	}
	userID, _ := c.Locals("user_id").(uint)
	key := c.Params("key")
	if err := h.svc.Set(c.Context(), userID, req.Scope, key, req.Value, req.Note, req.Version); err != nil {
		return h.writeErr(c, err, key)
	}
	return response.Success(c, fiber.Map{"key": key, "scope": req.Scope})
}

func (h *Handler) Reset(c fiber.Ctx) error {
	userID, _ := c.Locals("user_id").(uint)
	key, scope := c.Params("key"), c.Query("scope")
	if err := h.svc.Reset(c.Context(), userID, scope, key, c.Query("note")); err != nil {
		return h.writeErr(c, err, key)
	}
	return response.Success(c, fiber.Map{"key": key, "scope": scope})
}

func (h *Handler) writeErr(c fiber.Ctx, err error, key string) error {
	switch {
	case errors.Is(err, ErrNoKEK):
		return response.Error(c, fiber.StatusServiceUnavailable, apperrors.ErrOperationFailed, "oauth 未配置 KUN_CONFIG_KEK,无法写入")
	case errors.Is(err, ErrNotStorable):
		return response.BadRequestMsg(c, apperrors.ErrInvalidParam, "该变量是启动接线或拓扑,只能在 compose 里设置")
	case errors.Is(err, ErrUnknownVar):
		return response.NotFoundMsg(c, apperrors.ErrNotFound, "没有任何服务读取这个变量")
	case errors.Is(err, ErrUnknownScope):
		return response.BadRequestMsg(c, apperrors.ErrInvalidParam, "未知的作用域")
	case errors.Is(err, ErrEmptyValue):
		return response.BadRequestMsg(c, apperrors.ErrInvalidParam, "值不能为空;要删除请撤销")
	case errors.Is(err, ErrValueTooLong), errors.Is(err, ErrNoteTooLong):
		return response.BadRequestMsg(c, apperrors.ErrInvalidParam, err.Error())
	case errors.Is(err, ErrVersionConflict):
		return response.Error(c, fiber.StatusConflict, apperrors.ErrOperationFailed, "该配置已被其他人修改,请刷新后重试")
	case errors.Is(err, ErrNoRow):
		return response.BadRequestMsg(c, apperrors.ErrOperationFailed, "该作用域没有存储值")
	default:
		slog.Error("envconfig: write failed", "key", key, "err", err)
		return response.InternalError(c, apperrors.ErrOperationFailed)
	}
}
