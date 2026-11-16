package envconfig

import (
	"context"
	"errors"
	"unicode/utf8"

	"api/pkg/config"
	"api/pkg/secretbox"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNoKEK           = errors.New("envconfig: " + config.KEKEnv + " is not set on this service")
	ErrNotStorable     = errors.New("envconfig: this variable cannot live in the config store")
	ErrUnknownVar      = errors.New("envconfig: no service reads this variable")
	ErrUnknownScope    = errors.New("envconfig: unknown scope")
	ErrEmptyValue      = errors.New("envconfig: value is empty")
	ErrValueTooLong    = errors.New("envconfig: value exceeds 8192 bytes")
	ErrNoteTooLong     = errors.New("envconfig: note exceeds 512 characters")
	ErrVersionConflict = errors.New("envconfig: version conflict")
	ErrNoRow           = errors.New("envconfig: no stored value")
)

const maxValueBytes = 8192

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

func (s *Store) Snapshots(ctx context.Context) ([]BootSnapshot, error) {
	var out []BootSnapshot
	err := s.db.WithContext(ctx).Order("service").Find(&out).Error
	return out, err
}

type SecretRow struct {
	ConfigSecret
	UpdatedByName string
}

func (s *Store) Secrets(ctx context.Context) ([]SecretRow, error) {
	var out []SecretRow
	err := s.db.WithContext(ctx).Model(&ConfigSecret{}).
		Select("config_secrets.*, users.name AS updated_by_name").
		Joins("LEFT JOIN users ON users.id = config_secrets.updated_by_user_id").
		Order("config_secrets.key, config_secrets.scope").Scan(&out).Error
	return out, err
}

type AuditRow struct {
	ConfigSecretAuditLog
	ActorName string
}

func (s *Store) Audit(ctx context.Context, limit int) ([]AuditRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []AuditRow
	err := s.db.WithContext(ctx).Model(&ConfigSecretAuditLog{}).
		Select("config_secret_audit_logs.*, users.name AS actor_name").
		Joins("LEFT JOIN users ON users.id = config_secret_audit_logs.actor_user_id").
		Order("config_secret_audit_logs.id DESC").Limit(limit).Scan(&out).Error
	return out, err
}

type PutInput struct {
	ActorID       uint
	Action        string
	Scope         string
	Key           string
	Value         string
	Note          string
	ExpectVersion *int64
}

func (s *Store) Put(ctx context.Context, kek []byte, in PutInput) (*ConfigSecret, error) {
	if len(kek) == 0 {
		return nil, ErrNoKEK
	}
	if !config.Storable(in.Key) {
		return nil, ErrNotStorable
	}
	if in.Value == "" {
		return nil, ErrEmptyValue
	}
	if len(in.Value) > maxValueBytes {
		return nil, ErrValueTooLong
	}
	if utf8.RuneCountInString(in.Note) > 512 {
		return nil, ErrNoteTooLong
	}
	sealed, err := secretbox.Seal(kek, in.Value)
	if err != nil {
		return nil, err
	}
	secret := config.IsSecret(in.Key)
	var saved ConfigSecret
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cur ConfigSecret
		found := true
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scope = ? AND key = ?", in.Scope, in.Key).First(&cur).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			found = false
		}
		if in.ExpectVersion != nil && (!found || cur.Version != *in.ExpectVersion) {
			return ErrVersionConflict
		}
		audit := ConfigSecretAuditLog{
			ActorUserID: in.ActorID, Action: in.Action, Scope: in.Scope, Key: in.Key,
			NewFingerprint: secretbox.Fingerprint(kek, in.Value), Note: in.Note,
		}
		if found {
			audit.OldFingerprint = cur.Fingerprint
			if !secret {
				audit.OldValue, _ = secretbox.Open(kek, cur.Cipher)
			}
		}
		if !secret {
			audit.NewValue = in.Value
		}
		saved = cur
		saved.Scope, saved.Key, saved.Cipher = in.Scope, in.Key, sealed
		saved.Fingerprint = audit.NewFingerprint
		saved.Hint = ""
		if secret {
			saved.Hint = secretbox.Hint(in.Value)
		}
		saved.Version = cur.Version + 1
		saved.UpdatedByUserID, saved.Note = in.ActorID, in.Note
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		return tx.Create(&audit).Error
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

func (s *Store) Delete(ctx context.Context, kek []byte, actorID uint, scope, key, note string) error {
	if utf8.RuneCountInString(note) > 512 {
		return ErrNoteTooLong
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cur ConfigSecret
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scope = ? AND key = ?", scope, key).First(&cur).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNoRow
			}
			return err
		}
		audit := ConfigSecretAuditLog{
			ActorUserID: actorID, Action: ActionReset, Scope: scope, Key: key,
			OldFingerprint: cur.Fingerprint, Note: note,
		}
		if !config.IsSecret(key) && len(kek) > 0 {
			audit.OldValue, _ = secretbox.Open(kek, cur.Cipher)
		}
		if err := tx.Delete(&cur).Error; err != nil {
			return err
		}
		return tx.Create(&audit).Error
	})
}
