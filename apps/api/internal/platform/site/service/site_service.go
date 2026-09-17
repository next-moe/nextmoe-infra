package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"api/internal/platform/devapi"
	"api/internal/platform/site/model"
	"api/internal/platform/site/repository"
)

type SiteService struct {
	siteRepo        *repository.SiteRepository
	oauthClientRepo *repository.OAuthClientRepository
	devRepo         *devapi.Repository
}

// devRepo is required rather than optional so that no wiring can produce a
// SiteService whose DeleteOAuthClient silently drops the guard again.
func NewSiteService(siteRepo *repository.SiteRepository, oauthClientRepo *repository.OAuthClientRepository, devRepo *devapi.Repository) *SiteService {
	return &SiteService{
		siteRepo:        siteRepo,
		oauthClientRepo: oauthClientRepo,
		devRepo:         devRepo,
	}
}

func (s *SiteService) GetByID(ctx context.Context, id uint) (*model.Site, error) {
	return s.siteRepo.FindByID(ctx, id)
}

func (s *SiteService) GetByDomain(ctx context.Context, domain string) (*model.Site, error) {
	return s.siteRepo.FindByDomain(ctx, domain)
}

func (s *SiteService) Create(ctx context.Context, site *model.Site) error {
	return s.siteRepo.Create(ctx, site)
}

func (s *SiteService) Update(ctx context.Context, site *model.Site) error {
	return s.siteRepo.Update(ctx, site)
}

func (s *SiteService) Delete(ctx context.Context, id uint) error {
	return s.siteRepo.Delete(ctx, id)
}

func (s *SiteService) List(ctx context.Context) ([]model.Site, error) {
	return s.siteRepo.List(ctx)
}

func (s *SiteService) ListByCreator(ctx context.Context, userID uint) ([]model.Site, error) {
	return s.siteRepo.ListByCreator(ctx, userID)
}

func (s *SiteService) DomainExists(ctx context.Context, domain string) bool {
	site, err := s.siteRepo.FindByDomain(ctx, domain)
	return err == nil && site != nil
}

func (s *SiteService) ListOAuthClients(ctx context.Context) ([]model.OAuthClient, error) {
	return s.oauthClientRepo.FindAll(ctx)
}

func (s *SiteService) ListOAuthClientsByCreator(ctx context.Context, userID uint) ([]model.OAuthClient, error) {
	return s.oauthClientRepo.FindAllByCreator(ctx, userID)
}

func (s *SiteService) GetOAuthClientsBySiteID(ctx context.Context, siteID uint) ([]model.OAuthClient, error) {
	return s.oauthClientRepo.FindBySiteID(ctx, siteID)
}

func (s *SiteService) GetOAuthClientsBySiteIDAndCreator(ctx context.Context, siteID, userID uint) ([]model.OAuthClient, error) {
	return s.oauthClientRepo.FindBySiteIDAndCreator(ctx, siteID, userID)
}

func (s *SiteService) CreateOAuthClient(ctx context.Context, siteID uint, name string, redirectURIs, grants, allowedScopes []string, isPublic, autoConsent bool, refreshTokenTTLSeconds *int, listed bool, logoURL, tagline string, displayOrder int, createdBy *uint) (*model.OAuthClient, string, error) {
	clientID, err := generateRandomHex(16)
	if err != nil {
		return nil, "", err
	}

	secret, err := generateRandomHex(32)
	if err != nil {
		return nil, "", err
	}

	urisJSON, _ := json.Marshal(redirectURIs)
	grantsJSON, _ := json.Marshal(grants)

	client := &model.OAuthClient{
		ID:              clientID,
		SiteID:          &siteID,
		Name:            name,
		Secret:          model.HashOAuthClientSecret(secret),
		RedirectURIs:    urisJSON,
		Grants:          grantsJSON,
		IsPublic:        isPublic,
		AutoConsent:     autoConsent,
		CreatedByUserID: createdBy,
		Listed:          listed,
		LogoURL:         logoURL,
		Tagline:         tagline,
		DisplayOrder:    displayOrder,
	}
	if allowedScopes != nil {
		scopesJSON, _ := json.Marshal(allowedScopes)
		client.AllowedScopes = scopesJSON
	}
	if refreshTokenTTLSeconds != nil {
		client.RefreshTokenTTLSeconds = *refreshTokenTTLSeconds
	}

	if err := s.oauthClientRepo.Create(ctx, client); err != nil {
		return nil, "", err
	}

	return client, secret, nil
}

func (s *SiteService) UpdateOAuthClient(ctx context.Context, clientID string, name *string, redirectURIs, grants, allowedScopes []string, autoConsent *bool, refreshTokenTTLSeconds *int, listed *bool, logoURL, tagline *string, displayOrder *int) (*model.OAuthClient, error) {
	client, err := s.oauthClientRepo.FindByClientID(ctx, clientID)
	if err != nil {
		return nil, err
	}

	if name != nil {
		client.Name = *name
	}
	if redirectURIs != nil {
		urisJSON, _ := json.Marshal(redirectURIs)
		client.RedirectURIs = urisJSON
	}
	if grants != nil {
		grantsJSON, _ := json.Marshal(grants)
		client.Grants = grantsJSON
	}
	if allowedScopes != nil {
		scopesJSON, _ := json.Marshal(allowedScopes)
		client.AllowedScopes = scopesJSON
	}
	if autoConsent != nil {
		client.AutoConsent = *autoConsent
	}
	if refreshTokenTTLSeconds != nil {
		client.RefreshTokenTTLSeconds = *refreshTokenTTLSeconds
	}
	if listed != nil {
		client.Listed = *listed
	}
	if logoURL != nil {
		client.LogoURL = *logoURL
	}
	if tagline != nil {
		client.Tagline = *tagline
	}
	if displayOrder != nil {
		client.DisplayOrder = *displayOrder
	}

	if err := s.oauthClientRepo.Update(ctx, client); err != nil {
		return nil, err
	}
	return client, nil
}

func (s *SiteService) GetOAuthClient(ctx context.Context, clientID string) (*model.OAuthClient, error) {
	return s.oauthClientRepo.FindByClientID(ctx, clientID)
}

type StorageConfig struct {
	ArtifactEnabled         bool
	ArtifactSiteKey         string
	ArtifactCDNBase         string
	ArtifactAllowedMime     []string
	ArtifactMaxFileSize     int64
	ArtifactQuotaDaily      int
	ArtifactQuotaBytesDaily int64
	ImageEnabled            bool
	ImageSiteKey            string
	ImageCDNBase            string
	ImageAllowedPresets     []string
	ImageMaxFileSize        int64
	ImageQuotaDaily         int
	ImageQuotaBytesDaily    int64
}

func (s *SiteService) UpdateOAuthClientStorage(ctx context.Context, clientID string, cfg StorageConfig) (*model.OAuthClient, error) {
	client, err := s.oauthClientRepo.FindByClientID(ctx, clientID)
	if err != nil {
		return nil, err
	}
	client.ArtifactEnabled = cfg.ArtifactEnabled
	client.ArtifactSiteKey = cfg.ArtifactSiteKey
	client.ArtifactCDNBase = cfg.ArtifactCDNBase
	client.ArtifactAllowedMime = marshalStringList(cfg.ArtifactAllowedMime)
	client.ArtifactMaxFileSize = cfg.ArtifactMaxFileSize
	client.ArtifactQuotaDaily = cfg.ArtifactQuotaDaily
	client.ArtifactQuotaBytesDaily = cfg.ArtifactQuotaBytesDaily
	client.ImageEnabled = cfg.ImageEnabled
	client.ImageSiteKey = cfg.ImageSiteKey
	client.ImageCDNBase = cfg.ImageCDNBase
	client.ImageAllowedPresets = marshalStringList(cfg.ImageAllowedPresets)
	client.ImageMaxFileSize = cfg.ImageMaxFileSize
	client.ImageQuotaDaily = cfg.ImageQuotaDaily
	client.ImageQuotaBytesDaily = cfg.ImageQuotaBytesDaily
	if err := s.oauthClientRepo.Update(ctx, client); err != nil {
		return nil, err
	}
	return client, nil
}

func marshalStringList(list []string) []byte {
	if list == nil {
		list = []string{}
	}
	b, _ := json.Marshal(list)
	return b
}

func (s *SiteService) DeleteOAuthClient(ctx context.Context, clientID string) error {
	client, err := s.oauthClientRepo.FindByClientID(ctx, clientID)
	if err != nil {
		return err
	}
	if err := s.devRepo.EnsureDeletable(ctx, client); err != nil {
		return err
	}
	return s.oauthClientRepo.Delete(ctx, clientID)
}

func generateRandomHex(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
