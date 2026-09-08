package devapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	siteModel "api/internal/platform/site/model"

	"gorm.io/datatypes"
)

const (
	MaxAppsPerOwner     = 5
	MaxActiveKeysPerApp = 5
	maxAppNameLen       = 100
	maxAppDescLen       = 100
)

// Scopes a key owner may tick unilaterally. galgame:read left this list when the
// /v1/galgame face retired to a 410 tombstone (wave 146): no live route consumes
// it, so offering it minted a permission over nothing. news:read left it the
// other way round on 2026-08-25 — the grant machinery retired and /v1/news now
// takes any valid key, so there is nothing left to tick. store:read joined on
// 2026-08-26 as part of the same retirement: /v1/store still checks the scope
// on every request, and with the application queue gone, ticking it here is the
// only way anyone can hold it. moyu:read and sticker:read joined on 2026-09-08
// with the first two federated downstream faces (B-tier gateway termination,
// docs/developer-platform/08 §16.5) and left the same day, the news:read way:
// the first smoke call was 403'd by the scope its own owner had not ticked,
// and the ruling followed — a free read-only face takes any valid key, so
// there is nothing left to tick. No key was ever minted with either string.
var selfServiceScopes = []string{ScopeCatalogRead, ScopeStoreRead}

var (
	ErrAppLimitReached = errors.New("devapi: application limit reached")
	ErrKeyLimitReached = errors.New("devapi: active key limit reached")
	ErrScopeNotAllowed = errors.New("devapi: scope not permitted (want catalog:read or store:read)")
	ErrNameRequired    = errors.New("devapi: name is required")
	ErrNameTooLong     = errors.New("devapi: name too long (max 100)")
	ErrDescTooLong     = errors.New("devapi: description too long (max 100)")
)

type SelfServiceService struct {
	repo  *Repository
	admin *AdminService
	store Store
}

func NewSelfServiceService(repo *Repository, admin *AdminService, store Store) *SelfServiceService {
	return &SelfServiceService{repo: repo, admin: admin, store: store}
}

func (s *SelfServiceService) CreateApp(ctx context.Context, ownerUserID uint, name, description string, login *UserLoginRequest) (*siteModel.OAuthClient, error) {
	mode, err := s.repo.PolicyMode(ctx, CapabilityAppCreate)
	if err != nil {
		return nil, err
	}
	if mode == PolicyDisabled {
		return nil, ErrCapabilityDisabled
	}
	if err := validateAppMeta(name, description, true); err != nil {
		return nil, err
	}
	if err := validateAppName(name); err != nil {
		return nil, err
	}
	redirectURIs, grants, userScopes := "[]", "[]", ""
	isPublic := false
	if login != nil {
		scopes, err := validateUserLogin(*login)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(login.RedirectURIs)
		if err != nil {
			return nil, err
		}
		redirectURIs = string(encoded)
		grants = `["authorization_code","refresh_token"]`
		userScopes = strings.Join(scopes, " ")
		isPublic = true
	}
	n, err := s.repo.CountAppsByOwner(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	if n >= MaxAppsPerOwner {
		return nil, ErrAppLimitReached
	}

	clientID, err := generateHex(16)
	if err != nil {
		return nil, err
	}
	secret, err := generateHex(32)
	if err != nil {
		return nil, err
	}

	owner := ownerUserID
	reviewStatus, enabled := AppReviewApproved, true
	if mode == PolicyApproval {
		reviewStatus, enabled = AppReviewPending, false
	}
	app := &siteModel.OAuthClient{
		ID:              clientID,
		Name:            name,
		Secret:          siteModel.HashOAuthClientSecret(secret),
		RedirectURIs:    datatypes.JSON([]byte(redirectURIs)),
		Grants:          datatypes.JSON([]byte(grants)),
		IsPublic:        isPublic,
		AllowedScopes:   datatypes.JSON(appAllowedScopes(userScopes)),
		Tagline:         description,
		OwnerUserID:     &owner,
		DevEnabled:      enabled,
		DevTier:         TierFree,
		DevRatePerMin:   0,
		DevQuotaDaily:   0,
		DevReviewStatus: reviewStatus,
	}
	if err := s.repo.CreateApp(ctx, app); err != nil {
		return nil, err
	}
	return app, nil
}

func (s *SelfServiceService) ListApps(ctx context.Context, ownerUserID uint) ([]AppView, error) {
	apps, err := s.repo.ListAppsByOwner(ctx, ownerUserID)
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

func (s *SelfServiceService) GetApp(ctx context.Context, ownerUserID uint, clientID string) (*AppView, error) {
	app, err := s.repo.GetAppByOwner(ctx, clientID, ownerUserID)
	if err != nil {
		return nil, err
	}
	n, err := s.repo.CountKeysByClient(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	return &AppView{Client: app, KeyCount: n}, nil
}

func (s *SelfServiceService) UpdateApp(ctx context.Context, ownerUserID uint, clientID string, name, description *string, login *UserLoginRequest) (*siteModel.OAuthClient, error) {
	app, err := s.repo.GetAppByOwner(ctx, clientID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if err := s.requireCapability(ctx, CapabilityAppManage); err != nil {
		return nil, err
	}
	if err := validateAppMetaPtr(name, description); err != nil {
		return nil, err
	}
	if name != nil {
		if err := validateAppName(*name); err != nil {
			return nil, err
		}
	}
	fields := map[string]any{}
	if login != nil {
		scopes, err := validateUserLogin(*login)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(login.RedirectURIs)
		if err != nil {
			return nil, err
		}
		fields["redirect_uris"] = datatypes.JSON(encoded)
		fields["grants"] = datatypes.JSON([]byte(`["authorization_code","refresh_token"]`))
		fields["allowed_scopes"] = datatypes.JSON(appAllowedScopes(strings.Join(scopes, " ")))
		fields["is_public"] = true
	}
	if name != nil {
		fields["name"] = *name
	}
	if description != nil {
		fields["tagline"] = *description
	}
	if err := s.repo.UpdateAppFields(ctx, app.ID, fields); err != nil {
		return nil, err
	}
	return s.repo.GetApp(ctx, app.ID)
}

// ArchiveApp is the portal's "delete application". It revokes every live key,
// takes the application out of service and — the part a plain dev_enabled=false
// never did — releases the owner's slot and drops the row out of their list.
// An application nothing ever used is deleted outright, keys and all; one that
// served requests keeps its row, because its client_id still anchors sessions,
// authorization codes, short links and metered history. Either way the owner
// cannot undo it.
//
// A pending or declined application may be archived, which the deactivation
// this replaces refused. That refusal existed so "waiting for review" could not
// silently become "gone" — but because CountAppsByOwner has never filtered on
// status, it also meant a declined application held one of five slots with no
// self-service way to free it, and five declines locked the account out of the
// platform for good.
func (s *SelfServiceService) ArchiveApp(ctx context.Context, ownerUserID uint, clientID string) error {
	app, err := s.repo.GetAppByOwner(ctx, clientID, ownerUserID)
	if err != nil {
		return err
	}
	if err := s.requireCapability(ctx, CapabilityAppManage); err != nil {
		return err
	}
	keys, err := s.repo.ListKeysByClient(ctx, app.ID)
	if err != nil {
		return err
	}
	for i := range keys {
		if keys[i].RevokedAt != nil {
			continue
		}
		if err := s.admin.RevokeKey(ctx, keys[i].ID); err != nil {
			return err
		}
	}
	// Keys that never served a request go with the application. Re-read first:
	// the revocations above happened after the list was taken, and DeleteKey
	// judges on the row's own RevokedAt. ErrKeyHasHistory is the answer "this
	// one stays", not a failure — anything a usage row still points at survives
	// and takes the application's row with it.
	//
	// Without this an owner strands themselves: an archived application is
	// hidden from GetAppByOwner, so they can never reach its keys again, and
	// "create, mint, never call, delete" would leave an archived row plus an
	// orphan key that only an operator could clear.
	keys, err = s.repo.ListKeysByClient(ctx, app.ID)
	if err != nil {
		return err
	}
	for i := range keys {
		if err := s.admin.DeleteKey(ctx, &keys[i]); err != nil && !errors.Is(err, ErrKeyHasHistory) {
			return err
		}
	}

	// A shell nothing ever referenced is deleted outright rather than archived.
	// Not a convenience: archiving releases the owner's slot, so without this
	// "create, archive, repeat" is an unbounded row-writing loop that the old
	// five-slot-forever behaviour made impossible. The guard is the same one
	// AdminService.DeleteApp uses, so the row that disappears here is exactly
	// the row an operator would have been allowed to delete anyway.
	refs, err := s.repo.AppReferences(ctx, app.ID)
	if err != nil {
		return err
	}
	if refs.Empty() {
		return s.repo.DeleteApp(ctx, app.ID)
	}
	// Settlement eligibility goes with it: the short links keep counting clicks
	// forever, and a share of a fixed pool must not keep accruing to an
	// application its owner has taken out of service.
	return s.repo.UpdateAppFields(ctx, app.ID, map[string]any{
		"dev_enabled":               false,
		"dev_archived_at":           time.Now().UTC(),
		"store_settlement_eligible": false,
	})
}

func (s *SelfServiceService) MintKey(ctx context.Context, ownerUserID uint, clientID string, in MintKeyInput) (*DeveloperAPIKey, string, error) {
	app, err := s.repo.GetAppByOwner(ctx, clientID, ownerUserID)
	if err != nil {
		return nil, "", err
	}
	if err := s.requireCapability(ctx, CapabilityKeyMint); err != nil {
		return nil, "", err
	}
	if appAwaitsReview(app.DevReviewStatus) {
		return nil, "", ErrAppNotApproved
	}
	if in.Name == "" {
		return nil, "", ErrNameRequired
	}
	if len(in.Name) > maxAppNameLen {
		return nil, "", ErrNameTooLong
	}
	if err := checkMintScopes(in.Scopes); err != nil {
		return nil, "", err
	}
	active, err := s.repo.CountActiveKeysByClient(ctx, clientID, time.Now())
	if err != nil {
		return nil, "", err
	}
	if active >= MaxActiveKeysPerApp {
		return nil, "", ErrKeyLimitReached
	}
	return s.admin.MintKey(ctx, clientID, in, ownerUserID)
}

func (s *SelfServiceService) ListKeys(ctx context.Context, ownerUserID uint, clientID string) ([]DeveloperAPIKey, error) {
	if _, err := s.repo.GetAppByOwner(ctx, clientID, ownerUserID); err != nil {
		return nil, err
	}
	return s.admin.ListKeys(ctx, clientID)
}

func (s *SelfServiceService) RotateKey(ctx context.Context, ownerUserID uint, clientID string, keyID uint) (*DeveloperAPIKey, string, error) {
	if _, err := s.repo.GetAppByOwner(ctx, clientID, ownerUserID); err != nil {
		return nil, "", err
	}
	if err := s.requireCapability(ctx, CapabilityKeyMint); err != nil {
		return nil, "", err
	}
	key, err := s.admin.GetKeyForClient(ctx, clientID, keyID)
	if err != nil {
		return nil, "", err
	}
	if key == nil {
		return nil, "", nil
	}
	return s.admin.RotateKey(ctx, keyID, ownerUserID)
}

func (s *SelfServiceService) RevokeKey(ctx context.Context, ownerUserID uint, clientID string, keyID uint) (found bool, err error) {
	if _, err := s.repo.GetAppByOwner(ctx, clientID, ownerUserID); err != nil {
		return false, err
	}
	key, err := s.admin.GetKeyForClient(ctx, clientID, keyID)
	if err != nil {
		return false, err
	}
	if key == nil {
		return false, nil
	}
	return true, s.admin.RevokeKey(ctx, keyID)
}

func (s *SelfServiceService) DeleteKey(ctx context.Context, ownerUserID uint, clientID string, keyID uint) (found bool, err error) {
	if _, err := s.repo.GetAppByOwner(ctx, clientID, ownerUserID); err != nil {
		return false, err
	}
	key, err := s.admin.GetKeyForClient(ctx, clientID, keyID)
	if err != nil {
		return false, err
	}
	if key == nil {
		return false, nil
	}
	return true, s.admin.DeleteKey(ctx, key)
}

func checkMintScopes(scopes []string) error {
	for _, sc := range scopes {
		if !slices.Contains(selfServiceScopes, sc) {
			return ErrScopeNotAllowed
		}
	}
	return nil
}

func validateAppMeta(name, description string, requireName bool) error {
	if requireName && name == "" {
		return ErrNameRequired
	}
	if len(name) > maxAppNameLen {
		return ErrNameTooLong
	}
	if len(description) > maxAppDescLen {
		return ErrDescTooLong
	}
	return nil
}

func validateAppMetaPtr(name, description *string) error {
	if name != nil {
		if *name == "" {
			return ErrNameRequired
		}
		if len(*name) > maxAppNameLen {
			return ErrNameTooLong
		}
	}
	if description != nil && len(*description) > maxAppDescLen {
		return ErrDescTooLong
	}
	return nil
}

func generateHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
