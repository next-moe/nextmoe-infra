package devapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	siteModel "api/internal/platform/site/model"

	"gorm.io/datatypes"
)

const rotateGrace = 72 * time.Hour

var (
	ErrInvalidTier      = errors.New("devapi: invalid tier (want free|trusted|internal)")
	ErrKeyNotRevoked    = errors.New("devapi: a key must be revoked before it can be deleted")
	ErrKeyHasHistory    = errors.New("devapi: a key that has been used cannot be deleted, only revoked")
	ErrAppArchived      = errors.New("devapi: application is archived — re-enable it before minting a key")
	ErrAppNotArchived   = errors.New("devapi: an application must be archived before it can be deleted")
	ErrAppHasReferences = errors.New("devapi: application still has keys, usage, store links or logins, or can sign users in")
)

type AdminService struct {
	repo  *Repository
	store Store
}

func NewAdminService(repo *Repository, store Store) *AdminService {
	return &AdminService{repo: repo, store: store}
}

type AppConfig struct {
	OwnerUserID             *uint
	DevEnabled              *bool
	DevTier                 *string
	DevRatePerMin           *int
	DevQuotaDaily           *int
	StoreSettlementEligible *bool
}

type AppView struct {
	Client   *siteModel.OAuthClient
	KeyCount int64
}

func (s *AdminService) UpdateAppConfig(ctx context.Context, clientID string, cfg AppConfig) (*siteModel.OAuthClient, error) {
	fields := map[string]any{}
	if cfg.OwnerUserID != nil {
		fields["owner_user_id"] = *cfg.OwnerUserID
	}
	if cfg.DevEnabled != nil {
		fields["dev_enabled"] = *cfg.DevEnabled
		// The console's enable path is itself an approval: without this a row
		// enabled straight from the app list would stay 'pending' and its owner
		// would still be refused a key on a live application. It is also the
		// only un-archive there is — an application put back into service must
		// reappear in its owner's list, or they hold a live application they
		// cannot see.
		if *cfg.DevEnabled {
			fields["dev_review_status"] = AppReviewApproved
			fields["dev_review_note"] = ""
			fields["dev_archived_at"] = nil
		}
	}
	if cfg.DevTier != nil {
		if !validTier(*cfg.DevTier) {
			return nil, ErrInvalidTier
		}
		fields["dev_tier"] = *cfg.DevTier
	}
	if cfg.DevRatePerMin != nil {
		fields["dev_rate_per_min"] = *cfg.DevRatePerMin
	}
	if cfg.DevQuotaDaily != nil {
		fields["dev_quota_daily"] = *cfg.DevQuotaDaily
	}
	if cfg.StoreSettlementEligible != nil {
		fields["store_settlement_eligible"] = *cfg.StoreSettlementEligible
	}
	if err := s.repo.UpdateAppDevConfig(ctx, clientID, fields); err != nil {
		return nil, err
	}
	s.bustAppCredentials(ctx, clientID)
	return s.repo.GetApp(ctx, clientID)
}

func (s *AdminService) MovesSettlement(ctx context.Context, clientID string, cfg AppConfig) (bool, error) {
	if cfg.OwnerUserID == nil && cfg.StoreSettlementEligible == nil {
		return false, nil
	}
	cur, err := s.repo.GetApp(ctx, clientID)
	if err != nil {
		return false, err
	}
	var owner uint
	if cur.OwnerUserID != nil {
		owner = *cur.OwnerUserID
	}
	return (cfg.OwnerUserID != nil && *cfg.OwnerUserID != owner) ||
		(cfg.StoreSettlementEligible != nil && *cfg.StoreSettlementEligible != cur.StoreSettlementEligible), nil
}

func (s *AdminService) bustAppCredentials(ctx context.Context, clientID string) {
	keys, err := s.repo.ListKeysByClient(ctx, clientID)
	if err != nil {
		slog.Warn("devapi: app config changed but its resolve cache could not be busted",
			"client_id", clientID, "err", err)
		return
	}
	for i := range keys {
		if key, ok := credCacheKeyForStoredHash(keys[i].KeyHash); ok {
			_ = s.store.Del(ctx, key)
		}
	}
}

func (s *AdminService) ListApps(ctx context.Context, filter string) ([]AppView, error) {
	apps, err := s.repo.ListAppsByFilter(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]AppView, len(apps))
	for i := range apps {
		n, err := s.repo.CountKeysByClient(ctx, apps[i].ID)
		if err != nil {
			return nil, err
		}
		out[i] = AppView{Client: &apps[i], KeyCount: n}
	}
	return out, nil
}

type MintKeyInput struct {
	Name   string
	Test   bool
	Scopes []string
}

func (s *AdminService) MintKey(ctx context.Context, clientID string, in MintKeyInput, createdBy uint) (*DeveloperAPIKey, string, error) {
	app, err := s.repo.GetApp(ctx, clientID)
	if err != nil {
		return nil, "", err
	}
	// The self-service face cannot reach an archived application at all
	// (GetAppByOwner filters them), so this is the only door left open. A key
	// minted here could never resolve — archiving clears dev_enabled — but it
	// would add a row that makes the application undeletable for good.
	if app.DevArchivedAt != nil {
		return nil, "", ErrAppArchived
	}
	scopes := in.Scopes
	if len(scopes) == 0 {
		scopes = []string{ScopeCatalogRead}
	}
	key, plaintext, err := s.buildKey(clientID, in.Name, in.Test, scopes, createdBy)
	if err != nil {
		return nil, "", err
	}
	if err := s.repo.CreateKey(ctx, key); err != nil {
		return nil, "", err
	}
	slog.Info("devapi key minted", "client_id", clientID, "key_id", key.ID, "prefix", key.KeyPrefix, "last4", key.Last4, "by", createdBy)
	return key, plaintext, nil
}

func (s *AdminService) ListKeys(ctx context.Context, clientID string) ([]DeveloperAPIKey, error) {
	return s.repo.ListKeysByClient(ctx, clientID)
}

func (s *AdminService) ListAllKeys(ctx context.Context, f KeyListFilter) ([]AdminKeyRow, int64, error) {
	return s.repo.ListAllKeys(ctx, f, time.Now())
}

func (s *AdminService) RotateKey(ctx context.Context, keyID, createdBy uint) (*DeveloperAPIKey, string, error) {
	old, err := s.repo.GetKey(ctx, keyID)
	if err != nil {
		return nil, "", err
	}
	var scopes []string
	if len(old.Scopes) > 0 {
		_ = json.Unmarshal(old.Scopes, &scopes)
	}
	test := strings.HasPrefix(old.KeyPrefix, TestPrefix) || strings.HasPrefix(old.KeyPrefix, V2TestPrefix)
	newKey, plaintext, err := s.buildKey(old.ClientID, old.Name, test, scopes, createdBy)
	if err != nil {
		return nil, "", err
	}
	if err := s.repo.CreateKey(ctx, newKey); err != nil {
		return nil, "", err
	}
	if err := s.repo.SetKeyExpiry(ctx, old.ID, time.Now().Add(rotateGrace)); err != nil {
		return nil, "", err
	}
	slog.Info("devapi key rotated", "client_id", old.ClientID, "old_key_id", old.ID, "new_key_id", newKey.ID, "by", createdBy)
	return newKey, plaintext, nil
}

func (s *AdminService) RevokeKey(ctx context.Context, keyID uint) error {
	key, err := s.repo.GetKey(ctx, keyID)
	if err != nil {
		return err
	}
	if err := s.repo.RevokeKey(ctx, key.ID, time.Now()); err != nil {
		return err
	}
	if cacheKey, ok := credCacheKeyForStoredHash(key.KeyHash); ok {
		_ = s.store.Del(ctx, cacheKey)
	}
	slog.Info("devapi key revoked", "client_id", key.ClientID, "key_id", key.ID)
	return nil
}

// DeleteKey removes the row, which no other path in this package does. The two
// conditions are the same for an operator as for an owner, because they are not
// about permission: a live credential must be revoked first (deleting it is a
// revocation that leaves no record of having happened), and a key that has ever
// served a request stays, because developer_api_usage rows carry its key_id
// with no foreign key behind it and deleting the key turns metered history into
// rows pointing at nothing. last_used_at is the durable half of that test —
// usage rows age out after DeveloperUsageRetentionDays, the timestamp does not.
func (s *AdminService) DeleteKey(ctx context.Context, key *DeveloperAPIKey) error {
	if key.RevokedAt == nil {
		return ErrKeyNotRevoked
	}
	if key.LastUsedAt != nil {
		return ErrKeyHasHistory
	}
	used, err := s.repo.CountUsageByKey(ctx, key.ID)
	if err != nil {
		return err
	}
	if used > 0 {
		return ErrKeyHasHistory
	}
	if err := s.repo.DeleteKey(ctx, key.ID); err != nil {
		return err
	}
	if cacheKey, ok := credCacheKeyForStoredHash(key.KeyHash); ok {
		_ = s.store.Del(ctx, cacheKey)
	}
	slog.Info("devapi key deleted", "client_id", key.ClientID, "key_id", key.ID)
	return nil
}

// ArchiveApp is the operator's copy of the portal's delete, and the
// precondition for DeleteApp: nothing is removed until an application has been
// taken out of service deliberately.
//
// It deliberately does NOT delete a referenceless application the way
// SelfServiceService.ArchiveApp does, and the asymmetry is load-bearing rather
// than an oversight: this is the only way to produce an archived clean shell,
// which is the only input DeleteApp's success branch accepts. Unifying the two
// archives makes that branch unreachable.
func (s *AdminService) ArchiveApp(ctx context.Context, clientID string) (*siteModel.OAuthClient, error) {
	app, err := s.repo.GetApp(ctx, clientID)
	if err != nil {
		return nil, err
	}
	keys, err := s.repo.ListKeysByClient(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].RevokedAt != nil {
			continue
		}
		if err := s.RevokeKey(ctx, keys[i].ID); err != nil {
			return nil, err
		}
	}
	if err := s.repo.UpdateAppFields(ctx, app.ID, map[string]any{
		"dev_enabled":               false,
		"dev_archived_at":           time.Now().UTC(),
		"store_settlement_eligible": false,
	}); err != nil {
		return nil, err
	}
	slog.Info("devapi application archived", "client_id", app.ID, "owner", app.OwnerUserID)
	return s.repo.GetApp(ctx, app.ID)
}

// DeleteApp drops the oauth_clients row itself, and only for a never-used shell.
// oauth_clients is dual-purpose — the developer application table AND the OAuth
// login client table — and none of the columns pointing at it is a foreign key,
// so Postgres would accept any DELETE and leave live sessions authenticating
// against a client that no longer exists, short links counting clicks the
// settlement denominator can no longer attribute, and metered calls charged to
// a gap. Archived-first is part of the guard rather than a courtesy: a login
// client whose sessions have all expired presents exactly like an unused shell,
// and only a deliberate archive distinguishes them.
//
// The rule itself lives in Repository.EnsureDeletable because this is not the
// only door into it.
func (s *AdminService) DeleteApp(ctx context.Context, clientID string) error {
	app, err := s.repo.GetApp(ctx, clientID)
	if err != nil {
		return err
	}
	if err := s.repo.EnsureDeletable(ctx, app); err != nil {
		return err
	}
	if err := s.repo.DeleteApp(ctx, clientID); err != nil {
		return err
	}
	slog.Info("devapi application deleted", "client_id", clientID, "owner", app.OwnerUserID)
	return nil
}

func (s *AdminService) GetKeyForClient(ctx context.Context, clientID string, keyID uint) (*DeveloperAPIKey, error) {
	key, err := s.repo.GetKey(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if key.ClientID != clientID {
		return nil, nil
	}
	return key, nil
}

func (s *AdminService) buildKey(clientID, name string, test bool, scopes []string, createdBy uint) (*DeveloperAPIKey, string, error) {
	plaintext, err := GenerateV2Key(!test)
	if err != nil {
		return nil, "", err
	}
	kp, last4 := KeyMetadata(plaintext)
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return nil, "", err
	}
	return &DeveloperAPIKey{
		ClientID:        clientID,
		Name:            name,
		KeyHash:         HashKey(plaintext),
		KeyPrefix:       kp,
		Last4:           last4,
		Scopes:          datatypes.JSON(scopesJSON),
		CreatedByUserID: createdBy,
	}, plaintext, nil
}

func validTier(t string) bool {
	return t == TierFree || t == TierTrusted || t == TierInternal
}
