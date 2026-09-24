package service

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"api/internal/platform/auth/model"
	"api/internal/platform/settings/keys"
	"api/pkg/errors"
)

const AccountDeletionGrace = 7 * 24 * time.Hour

type codeCache interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte, expiration time.Duration) error
	Delete(key string) error
}

func (s *AuthService) codeStore() codeCache {
	if s.codes != nil {
		return s.codes
	}
	if s.cache != nil {
		return s.cache
	}
	return nil
}

func deletionCodeKey(userUUID string) string { return "account_delete:" + userUUID }

func (s *AuthService) deletableUser(ctx context.Context, userUUID string) (*model.User, error) {
	user, err := s.userRepo.FindByUUIDWithRoles(ctx, userUUID)
	if err != nil || user.IsAnonymized() {
		return nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	if adminProtected(user) {
		return nil, errors.NewWithCode(errors.ErrAuthDeletionProtected)
	}
	return user, nil
}

func (s *AuthService) SendDeletionCode(ctx context.Context, userUUID string) error {
	user, err := s.deletableUser(ctx, userUUID)
	if err != nil {
		return err
	}
	store := s.codeStore()
	if store == nil {
		return fmt.Errorf("cache not configured")
	}
	cooldownKey := "account_delete_cooldown:" + userUUID
	if cd, _ := store.Get(cooldownKey); cd != nil {
		return errors.NewWithCode(errors.ErrAuthCodeTooFrequent)
	}
	code, err := generateNumericCode(6)
	if err != nil {
		return err
	}
	ttl := verificationCodeTTL()
	if err := store.Set(deletionCodeKey(userUUID), []byte(code), ttl); err != nil {
		return err
	}
	cooldown := time.Duration(keys.AuthVerificationResendCooldownSeconds.Get()) * time.Second
	if err := store.Set(cooldownKey, []byte("1"), cooldown); err != nil {
		return err
	}
	if s.mailer != nil {
		if err := s.mailer.SendAccountDeletionCodeEmail(user.Email, user.Name, code, int(ttl.Minutes())); err != nil {
			return fmt.Errorf("failed to send email: %w", err)
		}
	}
	return nil
}

func (s *AuthService) RequestDeletion(ctx context.Context, userUUID, code string) (time.Time, error) {
	store := s.codeStore()
	if store == nil {
		return time.Time{}, fmt.Errorf("cache not configured")
	}
	raw, err := store.Get(deletionCodeKey(userUUID))
	if err != nil || raw == nil {
		return time.Time{}, errors.NewWithCode(errors.ErrAuthCodeExpired)
	}
	if subtle.ConstantTimeCompare(raw, []byte(code)) != 1 {
		s.countFailedDeletionCode(store, userUUID)
		return time.Time{}, errors.NewWithCode(errors.ErrAuthCodeInvalid)
	}
	user, err := s.deletableUser(ctx, userUUID)
	if err != nil {
		return time.Time{}, err
	}
	_ = store.Delete(deletionCodeKey(userUUID))
	if user.DeletionDueAt != nil {
		return *user.DeletionDueAt, nil
	}
	now := time.Now()
	due := now.Add(AccountDeletionGrace)
	if err := s.userRepo.ScheduleDeletion(ctx, user.ID, now, due); err != nil {
		return time.Time{}, err
	}
	if s.mailer != nil {
		if err := s.mailer.SendAccountDeletionScheduledEmail(user.Email, user.Name, due); err != nil {
			slog.Warn("account deletion: scheduled notice not sent", "user_id", user.ID, "err", err)
		}
	}
	return due, nil
}

const deletionCodeMaxFailures = 5

func (s *AuthService) countFailedDeletionCode(store codeCache, userUUID string) {
	key := "account_delete_failures:" + userUUID
	n := 1
	if raw, _ := store.Get(key); raw != nil {
		if v, err := strconv.Atoi(string(raw)); err == nil {
			n = v + 1
		}
	}
	if n >= deletionCodeMaxFailures {
		_ = store.Delete(deletionCodeKey(userUUID))
		_ = store.Delete(key)
		return
	}
	_ = store.Set(key, []byte(strconv.Itoa(n)), verificationCodeTTL())
}

func (s *AuthService) CancelDeletion(ctx context.Context, userUUID string) error {
	user, err := s.userRepo.FindByUUID(ctx, userUUID)
	if err != nil || user.IsAnonymized() {
		return errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	return s.userRepo.CancelDeletion(ctx, user.ID)
}

func (s *AdminService) ExecuteDueDeletions(ctx context.Context, now time.Time) (int, error) {
	users, err := s.userRepo.FindDueForDeletion(ctx, now, 100)
	if err != nil {
		return 0, err
	}
	done := 0
	var failed []uint
	for i := range users {
		u := &users[i]
		if adminProtected(u) {
			slog.Warn("account deletion: due account holds an admin role; left pending", "user_id", u.ID)
			continue
		}
		erased, err := s.userRepo.EraseAccount(ctx, u.ID, now, time.Now())
		if err != nil {
			slog.Error("account deletion: erase failed; retried next run", "user_id", u.ID, "err", err)
			failed = append(failed, u.ID)
			continue
		}
		if !erased {
			continue
		}
		done++
		if u.AvatarImageHash != nil {
			s.releaseAvatar(ctx, u.ID, *u.AvatarImageHash)
		}
	}
	if len(failed) > 0 {
		return done, fmt.Errorf("account deletion: %d of %d due account(s) failed: %v", len(failed), len(users), failed)
	}
	return done, nil
}
