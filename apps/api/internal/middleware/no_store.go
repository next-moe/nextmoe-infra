package middleware

import "github.com/gofiber/fiber/v3"

func NoStore() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(fiber.HeaderCacheControl, "no-store")
		return c.Next()
	}
}
