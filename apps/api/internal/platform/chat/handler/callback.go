package handler

import (
	"encoding/json"
	"log/slog"
	"time"

	"api/internal/platform/chat/service"
	"api/pkg/trustclient"

	"github.com/gofiber/fiber/v3"
)

func TrustCallback(secret string, svc *service.Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		body := c.Body()
		if !trustclient.VerifyCallback(secret, c.Get("X-Trust-Timestamp"), c.Get("X-Trust-Signature"), body, time.Now()) {
			return c.SendStatus(fiber.StatusUnauthorized)
		}
		var cb trustclient.Callback
		if err := json.Unmarshal(body, &cb); err != nil {
			return c.SendStatus(fiber.StatusBadRequest)
		}
		result, err := svc.ApplyDisposition(c.Context(), cb)
		if err != nil {
			slog.Error("chat trust callback", "err", err, "disposition_id", cb.DispositionID, "subject_id", cb.SubjectID)
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		if result == service.DispositionUnsupported {
			slog.Warn("chat trust callback not applied, handle it by hand",
				"subject_kind", cb.SubjectKind, "action", cb.Action, "disposition_id", cb.DispositionID, "subject_id", cb.SubjectID)
		}
		return c.JSON(fiber.Map{"ok": true})
	}
}
