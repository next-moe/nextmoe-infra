package handler

import (
	"encoding/json"
	"log/slog"
	"time"

	"api/internal/platform/community/service"
	"api/pkg/errors"
	"api/pkg/response"
	"api/pkg/trustclient"

	"github.com/gofiber/fiber/v3"
)

func TrustCallback(secret string, svc *service.CallbackService) fiber.Handler {
	return func(c fiber.Ctx) error {
		body := c.Body()
		if !trustclient.VerifyCallback(secret, c.Get("X-Trust-Timestamp"), c.Get("X-Trust-Signature"), body, time.Now()) {
			return response.Unauthorized(c, errors.ErrAuthUnauthorized)
		}
		var cb service.TrustCallback
		if err := json.Unmarshal(body, &cb); err != nil {
			return response.BadRequest(c, errors.ErrValidationFailed)
		}
		result, err := svc.Handle(c.Context(), cb)
		if err != nil {
			slog.Error("trust callback enforce", "err", err, "disposition_id", cb.DispositionID, "subject_id", cb.SubjectID)
			return response.InternalError(c, errors.ErrInternalServer)
		}
		if result == service.CallbackUnsupported {
			slog.Info("trust callback: action unsupported for community_post, handle manually",
				"action", cb.Action, "disposition_id", cb.DispositionID, "subject_id", cb.SubjectID)
		}
		return response.Success(c, fiber.Map{"ok": true})
	}
}
