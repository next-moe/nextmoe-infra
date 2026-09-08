package handler

import (
	"slices"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/service"
	sitePerm "api/internal/platform/site/perm"
	"api/pkg/errors"
	"api/pkg/response"
	"api/pkg/utils"

	"github.com/gofiber/fiber/v3"
)

var manageableRoles = []string{"user", "creator", "moderator", "admin"}

func callerCanManageRole(c fiber.Ctx, role string) bool {
	if !slices.Contains(manageableRoles, role) {
		return false
	}
	roles := callerRoles(c)
	if role == "admin" || role == "user" {
		return sitePerm.Resolver.Can(roles, sitePerm.RolesGrantAdmin)
	}
	return sitePerm.Resolver.Can(roles, sitePerm.RolesGrantBasic)
}

func callerRoles(c fiber.Ctx) []string {
	roles, _ := c.Locals("user_roles").([]string)
	return roles
}

type AdminHandler struct {
	adminService *service.AdminService
}

func NewAdminHandler(adminService *service.AdminService) *AdminHandler {
	return &AdminHandler{adminService: adminService}
}

func (h *AdminHandler) ListUsers(c fiber.Ctx) error {
	var req dto.UserListRequest
	if err := c.Bind().Query(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	result, err := h.adminService.ListUsers(c.Context(), &req, sitePerm.Resolver.Can(callerRoles(c), sitePerm.UsersPIIView))
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, result)
}

func (h *AdminHandler) RegistrationStats(c fiber.Ctx) error {
	var req dto.RegistrationStatsRequest
	if err := c.Bind().Query(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}
	days := req.Days
	if days == 0 {
		days = 30
	}

	result, err := h.adminService.RegistrationStats(c.Context(), days)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, result)
}

func (h *AdminHandler) HourlyRegistrationStats(c fiber.Ctx) error {
	var req dto.HourlyStatsRequest
	if err := c.Bind().Query(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	result, err := h.adminService.HourlyRegistrations(c.Context(), req.Date)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, result)
}

func (h *AdminHandler) GetUser(c fiber.Ctx) error {
	uuid := c.Params("uuid")
	if uuid == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}

	user, err := h.adminService.GetUser(c.Context(), uuid, sitePerm.Resolver.Can(callerRoles(c), sitePerm.UsersPIIView))
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			if appErr.Code == errors.ErrForbidden {
				return response.Forbidden(c, appErr.Code)
			}
			return response.NotFound(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, user)
}

func (h *AdminHandler) AssignRole(c fiber.Ctx) error {
	uuid := c.Params("uuid")
	if uuid == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}
	if callerUUID, _ := c.Locals("user_uuid").(string); callerUUID == uuid {
		return response.ForbiddenMsg(c, errors.ErrForbidden, "不能修改自己的角色")
	}

	var req dto.AssignRoleRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}
	if !callerCanManageRole(c, req.Role) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, "无权赋予该角色")
	}

	roles, err := h.adminService.AssignRole(c.Context(), uuid, req.Role)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.NotFound(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, fiber.Map{"roles": roles})
}

func (h *AdminHandler) RevokeRole(c fiber.Ctx) error {
	uuid := c.Params("uuid")
	role := c.Params("role")
	if uuid == "" || role == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}
	if callerUUID, _ := c.Locals("user_uuid").(string); callerUUID == uuid {
		return response.ForbiddenMsg(c, errors.ErrForbidden, "不能修改自己的角色")
	}
	if !callerCanManageRole(c, role) {
		return response.ForbiddenMsg(c, errors.ErrForbidden, "无权移除该角色")
	}

	roles, err := h.adminService.RevokeRole(c.Context(), uuid, role)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.NotFound(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, fiber.Map{"roles": roles})
}

func (h *AdminHandler) UpdateUser(c fiber.Ctx) error {
	uuid := c.Params("uuid")
	if uuid == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}

	var req dto.UpdateUserRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	roles := callerRoles(c)
	user, err := h.adminService.UpdateUser(c.Context(), uuid, &req, service.UserUpdateActor{
		CanSeePII:       sitePerm.Resolver.Can(roles, sitePerm.UsersPIIView),
		CanManageAdmins: sitePerm.Resolver.Can(roles, sitePerm.RolesGrantAdmin),
	})
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			if appErr.Code == errors.ErrForbidden {
				return response.ForbiddenMsg(c, appErr.Code, appErr.Message)
			}
			if appErr.Code == errors.ErrAuthUserNotFound {
				return response.NotFound(c, appErr.Code)
			}
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, user)
}

func (h *AdminHandler) BanUser(c fiber.Ctx) error {
	uuid := c.Params("uuid")
	if uuid == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}

	if err := h.adminService.BanUser(c.Context(), uuid); err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			if appErr.Code == errors.ErrForbidden {
				return response.Forbidden(c, appErr.Code)
			}
			return response.NotFound(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.SuccessWithMessage(c, "用户已封禁", nil)
}

func (h *AdminHandler) UnbanUser(c fiber.Ctx) error {
	uuid := c.Params("uuid")
	if uuid == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}

	if err := h.adminService.UnbanUser(c.Context(), uuid); err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			if appErr.Code == errors.ErrForbidden {
				return response.Forbidden(c, appErr.Code)
			}
			return response.NotFound(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.SuccessWithMessage(c, "用户已解封", nil)
}

func (h *AdminHandler) AnonymizeUser(c fiber.Ctx) error {
	uuid := c.Params("uuid")
	if uuid == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}

	if err := h.adminService.AnonymizeUser(c.Context(), uuid); err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			if appErr.Code == errors.ErrForbidden {
				return response.Forbidden(c, appErr.Code)
			}
			return response.NotFound(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.SuccessWithMessage(c, "用户已注销并匿名化", nil)
}

func (h *AdminHandler) DeleteUserSessions(c fiber.Ctx) error {
	uuid := c.Params("uuid")
	if uuid == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}

	if err := h.adminService.DeleteUserSessions(c.Context(), uuid); err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			if appErr.Code == errors.ErrForbidden {
				return response.Forbidden(c, appErr.Code)
			}
			return response.NotFound(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.SuccessWithMessage(c, "用户会话已清除", nil)
}
