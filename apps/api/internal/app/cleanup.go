package app

import (
	"context"
	"log/slog"
	"time"

	authrepo "api/internal/platform/auth/repository"
)

func StartCleanup(ctx context.Context, sessionRepo *authrepo.SessionRepository, authCodeRepo *authrepo.AuthorizationCodeRepository, activityRepo *authrepo.ActivityRepository) {
	ticker := time.NewTicker(1 * time.Hour)

	runCleanup(ctx, sessionRepo, authCodeRepo, activityRepo)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("cleanup goroutine stopped")
				return
			case <-ticker.C:
				runCleanup(ctx, sessionRepo, authCodeRepo, activityRepo)
			}
		}
	}()
}

func runCleanup(ctx context.Context, sessionRepo *authrepo.SessionRepository, authCodeRepo *authrepo.AuthorizationCodeRepository, activityRepo *authrepo.ActivityRepository) {
	if err := sessionRepo.DeleteExpired(ctx); err != nil {
		slog.Error("failed to cleanup expired sessions", "error", err)
	}
	if err := authCodeRepo.DeleteExpired(ctx); err != nil {
		slog.Error("failed to cleanup expired authorization codes", "error", err)
	}
	if err := activityRepo.Prune(ctx); err != nil {
		slog.Error("failed to prune old user activity hours", "error", err)
	}
	slog.Debug("expired data cleanup completed")
}
