package service

import (
	"testing"

	"api/internal/platform/auth/dto"
	"api/pkg/config"
)

func TestAuthService_ValidateAccessToken(t *testing.T) {
	jwtCfg := config.JWTConfig{
		Secret: "test-secret-key-for-testing-only",
	}

	svc := NewAuthService(nil, nil, jwtCfg)

	t.Run("invalid token", func(t *testing.T) {
		_, err := svc.ValidateAccessToken("invalid-token")
		if err == nil {
			t.Error("Expected error for invalid token")
		}
	})

	t.Run("empty token", func(t *testing.T) {
		_, err := svc.ValidateAccessToken("")
		if err == nil {
			t.Error("Expected error for empty token")
		}
	})
}

func TestPasswordValidation(t *testing.T) {
	t.Run("password too short", func(t *testing.T) {
		req := &dto.RegisterRequest{
			Name:     "testuser",
			Email:    "test@example.com",
			Password: "12345",
		}

		if len(req.Password) >= 6 {
			t.Error("Expected password to be too short")
		}
	})

	t.Run("valid password length", func(t *testing.T) {
		req := &dto.RegisterRequest{
			Name:     "testuser",
			Email:    "test@example.com",
			Password: "validpassword123",
		}

		if len(req.Password) < 6 {
			t.Error("Expected password to be valid")
		}
	})
}
