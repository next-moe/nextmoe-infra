package handler

import (
	"encoding/json"
	"log/slog"
	"time"

	"api/internal/platform/auth/service"
	"api/pkg/errors"
	"api/pkg/response"
	"api/pkg/trustclient"

	"github.com/gofiber/fiber/v3"
)

func ProfileTrustCallback(secret string, enforcer *service.ProfileEnforcer) fiber.Handler {
	return func(c fiber.Ctx) error {
		body := c.Body()
		if !trustclient.VerifyCallback(secret, c.Get("X-Trust-Timestamp"), c.Get("X-Trust-Signature"), body, time.Now()) {
			return response.Unauthorized(c, errors.ErrAuthUnauthorized)
		}
		var cb trustclient.Callback
		if err := json.Unmarshal(body, &cb); err != nil {
			return response.BadRequest(c, errors.ErrValidationFailed)
		}
		result, err := enforcer.Handle(c.Context(), cb)
		if err != nil {
			slog.Error("profile trust callback enforce", "err", err, "disposition_id", cb.DispositionID, "subject_id", cb.SubjectID)
			return response.InternalError(c, errors.ErrInternalServer)
		}
		switch result {
		case service.ProfileEnforced:
			slog.Info("profile reset by trust disposition", "disposition_id", cb.DispositionID, "user_id", cb.SubjectID, "action", cb.Action)
		case service.ProfileEnforceUnsupported:
			slog.Info("profile trust callback: not a user_profile action, ignored",
				"kind", cb.SubjectKind, "action", cb.Action, "disposition_id", cb.DispositionID)
		}
		return response.Success(c, fiber.Map{"ok": true})
	}
}
