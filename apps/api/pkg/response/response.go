package response

import (
	"api/pkg/errors"

	"github.com/gofiber/fiber/v3"
)

type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func Success(c fiber.Ctx, data any) error {
	return c.JSON(Response{
		Code:    0,
		Message: "成功",
		Data:    data,
	})
}

func SuccessWithMessage(c fiber.Ctx, message string, data any) error {
	return c.JSON(Response{
		Code:    0,
		Message: message,
		Data:    data,
	})
}

func Error(c fiber.Ctx, status int, code int, message string) error {
	return c.Status(status).JSON(Response{
		Code:    code,
		Message: message,
	})
}

func ErrorCode(c fiber.Ctx, status int, code int) error {
	return c.Status(status).JSON(Response{
		Code:    code,
		Message: errors.GetMessage(code),
	})
}

func FromAppError(c fiber.Ctx, status int, err *errors.AppError) error {
	return c.Status(status).JSON(Response{
		Code:    err.Code,
		Message: err.Message,
	})
}

func BadRequest(c fiber.Ctx, code int) error {
	return ErrorCode(c, fiber.StatusBadRequest, code)
}

func BadRequestMsg(c fiber.Ctx, code int, message string) error {
	return Error(c, fiber.StatusBadRequest, code, message)
}

func Unauthorized(c fiber.Ctx, code int) error {
	return ErrorCode(c, fiber.StatusUnauthorized, code)
}

func Forbidden(c fiber.Ctx, code int) error {
	return ErrorCode(c, fiber.StatusForbidden, code)
}

func ForbiddenMsg(c fiber.Ctx, code int, message string) error {
	return Error(c, fiber.StatusForbidden, code, message)
}

func NotFound(c fiber.Ctx, code int) error {
	return ErrorCode(c, fiber.StatusNotFound, code)
}

func NotFoundMsg(c fiber.Ctx, code int, message string) error {
	return Error(c, fiber.StatusNotFound, code, message)
}

func TooManyRequests(c fiber.Ctx) error {
	return ErrorCode(c, fiber.StatusTooManyRequests, errors.ErrTooManyRequests)
}

func InternalError(c fiber.Ctx, code int) error {
	return ErrorCode(c, fiber.StatusInternalServerError, code)
}

func InternalErrorMsg(c fiber.Ctx, message string) error {
	return Error(c, fiber.StatusInternalServerError, errors.ErrInternalServer, message)
}
