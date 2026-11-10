package handler

import (
	stderrors "errors"
	"log/slog"

	"api/internal/platform/auth/model"
	"api/pkg/errors"
	"api/pkg/imageclient"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type AvatarUploadHandler struct {
	db        *gorm.DB
	imgClient *imageclient.Client
}

func NewAvatarUploadHandler(db *gorm.DB, imgClient *imageclient.Client) *AvatarUploadHandler {
	return &AvatarUploadHandler{db: db, imgClient: imgClient}
}

func (h *AvatarUploadHandler) Upload(c fiber.Ctx) error {
	uuid := c.Params("uuid")
	if uuid == "" {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	return h.uploadFor(c, uuid)
}

func (h *AvatarUploadHandler) UploadMine(c fiber.Ctx) error {
	uuid, _ := c.Locals("user_uuid").(string)
	if uuid == "" {
		return response.Unauthorized(c, errors.ErrAuthUnauthorized)
	}
	return h.uploadFor(c, uuid)
}

func (h *AvatarUploadHandler) UploadClientLogo(c fiber.Ctx) error {
	if h.imgClient == nil {
		return response.InternalErrorMsg(c, "image client not configured (KUN_IMAGE_CLIENT_ID/SECRET unset)")
	}

	fh, err := c.FormFile("file")
	if err != nil || fh == nil {
		return response.BadRequest(c, errors.ErrMissingParam)
	}
	src, err := fh.Open()
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	defer src.Close()

	result, err := h.imgClient.Upload(c.Context(), src, fh.Filename, "avatar")
	if err != nil {
		return uploadFailure(c, err, "upload client logo failed")
	}
	return response.Success(c, result)
}

func (h *AvatarUploadHandler) uploadFor(c fiber.Ctx, uuid string) error {
	if h.imgClient == nil {
		return response.InternalErrorMsg(c, "image client not configured (KUN_IMAGE_CLIENT_ID/SECRET unset)")
	}

	var existing model.User
	if err := h.db.WithContext(c.Context()).
		Select("id").
		Where("uuid = ?", uuid).
		First(&existing).Error; err != nil {
		return response.NotFound(c, errors.ErrAuthUserNotFound)
	}

	fh, err := c.FormFile("file")
	if err != nil || fh == nil {
		return response.BadRequest(c, errors.ErrMissingParam)
	}
	src, err := fh.Open()
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	defer src.Close()

	result, err := h.imgClient.Upload(c.Context(), src, fh.Filename, "avatar")
	if err != nil {
		return uploadFailure(c, err, "upload avatar failed", "uuid", uuid)
	}

	if err := h.db.WithContext(c.Context()).
		Model(&model.User{}).
		Where("uuid = ?", uuid).
		Update("avatar_image_hash", result.Hash).Error; err != nil {
		slog.Error("persist avatar hash failed", "uuid", uuid, "err", err)
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, result)
}

func uploadFailure(c fiber.Ctx, err error, msg string, attrs ...any) error {
	status, code := uploadErrorStatus(err)
	if status >= fiber.StatusInternalServerError {
		slog.Error(msg, append(attrs, "err", err)...)
	} else {
		slog.Warn(msg, append(attrs, "err", err)...)
	}
	message := errors.GetMessage(code)
	if code == errors.ErrImageMIMEDenied {
		message = "不支持该图片格式，请使用 JPG、PNG 或 WebP"
	}
	return response.Error(c, status, code, message)
}

func uploadErrorStatus(err error) (int, int) {
	switch {
	case stderrors.Is(err, imageclient.ErrMIMEDenied):
		return fiber.StatusBadRequest, errors.ErrImageMIMEDenied
	case stderrors.Is(err, imageclient.ErrDecodeFailed):
		return fiber.StatusBadRequest, errors.ErrImageDecodeFailed
	case stderrors.Is(err, imageclient.ErrModerationRejected):
		return fiber.StatusBadRequest, errors.ErrModerationRejected
	case stderrors.Is(err, imageclient.ErrFileTooLarge):
		return fiber.StatusRequestEntityTooLarge, errors.ErrImageFileTooLarge
	case stderrors.Is(err, imageclient.ErrQuotaExceeded):
		return fiber.StatusTooManyRequests, errors.ErrImageQuotaExceeded
	}
	return fiber.StatusInternalServerError, errors.ErrOperationFailed
}
