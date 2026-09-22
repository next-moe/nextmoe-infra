package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"time"

	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/pkg/errors"
)

type PreferenceService struct {
	userRepo *repository.UserRepository
	prefRepo *repository.UserPreferenceRepository
}

func NewPreferenceService(userRepo *repository.UserRepository, prefRepo *repository.UserPreferenceRepository) *PreferenceService {
	return &PreferenceService{userRepo: userRepo, prefRepo: prefRepo}
}

type ContentPreferences struct {
	AdultConfirmedAt *time.Time
	NSFWDisplay      string
}

func (s *PreferenceService) ConfirmAdult(ctx context.Context, userUUID string) (*ContentPreferences, error) {
	if err := s.userRepo.ConfirmAdult(ctx, userUUID); err != nil {
		return nil, err
	}
	return s.readContent(ctx, userUUID)
}

func (s *PreferenceService) SetNSFWDisplay(ctx context.Context, userUUID, display string) (*ContentPreferences, error) {
	if !model.IsNSFWDisplay(display) {
		return nil, errors.NewWithCode(errors.ErrPrefDisplayInvalid)
	}
	current, err := s.readContent(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	if err := s.userRepo.UpdateNSFWDisplay(ctx, userUUID, display); err != nil {
		return nil, err
	}
	current.NSFWDisplay = display
	return current, nil
}

func (s *PreferenceService) readContent(ctx context.Context, userUUID string) (*ContentPreferences, error) {
	user, err := s.userRepo.FindByUUID(ctx, userUUID)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	return &ContentPreferences{
		AdultConfirmedAt: user.AdultConfirmedAt,
		NSFWDisplay:      user.NSFWDisplay,
	}, nil
}

func (s *PreferenceService) List(ctx context.Context, userID uint) ([]repository.PreferenceSummary, error) {
	return s.prefRepo.List(ctx, userID)
}

type PreferenceDoc struct {
	Namespace string
	Doc       json.RawMessage
	Version   int
	UpdatedAt *time.Time
}

// Get answers for a namespace that was never written with an empty document at
// version 0 rather than 404: a client that has to tell "nothing stored yet"
// apart from "the request failed" would otherwise have to read a status code to
// learn its own starting state, and version 0 is exactly what it passes back as
// If-Match to claim the first write.
func (s *PreferenceService) Get(ctx context.Context, userID uint, namespace string) (*PreferenceDoc, error) {
	pref, err := s.prefRepo.Get(ctx, userID, namespace)
	if err != nil {
		return nil, err
	}
	if pref == nil {
		return &PreferenceDoc{Namespace: namespace, Doc: json.RawMessage(`{}`), Version: 0}, nil
	}
	return &PreferenceDoc{
		Namespace: namespace,
		Doc:       json.RawMessage(pref.Doc),
		Version:   pref.Version,
		UpdatedAt: &pref.UpdatedAt,
	}, nil
}

func (s *PreferenceService) Put(ctx context.Context, userID uint, namespace string, doc []byte, expected *int) (*PreferenceDoc, error) {
	compact, err := validatePreferenceDoc(doc)
	if err != nil {
		return nil, err
	}

	var written *repository.PreferenceWrite
	if expected != nil {
		written, err = s.prefRepo.PutIfVersion(ctx, userID, namespace, compact, *expected)
		if stderrors.Is(err, repository.ErrPreferenceVersionMismatch) {
			return nil, errors.NewWithCode(errors.ErrPrefVersionConflict)
		}
	} else {
		written, err = s.prefRepo.Put(ctx, userID, namespace, compact)
	}
	if err != nil {
		return nil, err
	}

	return &PreferenceDoc{
		Namespace: namespace,
		Doc:       json.RawMessage(compact),
		Version:   written.Version,
		UpdatedAt: &written.UpdatedAt,
	}, nil
}

func (s *PreferenceService) Delete(ctx context.Context, userID uint, namespace string) error {
	return s.prefRepo.Delete(ctx, userID, namespace)
}

func validatePreferenceDoc(doc []byte) ([]byte, error) {
	if len(doc) == 0 {
		return nil, errors.NewWithCode(errors.ErrPrefDocInvalid)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(doc, &obj); err != nil || obj == nil {
		return nil, errors.NewWithCode(errors.ErrPrefDocInvalid)
	}
	compact, err := json.Marshal(obj)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrPrefDocInvalid)
	}
	if len(compact) > model.PreferenceMaxDocBytes {
		return nil, errors.NewWithCode(errors.ErrPrefDocTooLarge)
	}
	return compact, nil
}

// PreferenceNamespaceAllowed is the whole binding rule. A first-party session
// carries no client_id and reaches every namespace the account console has to
// list and delete; an OAuth token reaches its own client_id and the shared
// `global` namespace, and nothing else.
func PreferenceNamespaceAllowed(clientID, namespace string) bool {
	if clientID == "" {
		return true
	}
	return namespace == clientID || namespace == model.PreferenceGlobalNamespace
}
