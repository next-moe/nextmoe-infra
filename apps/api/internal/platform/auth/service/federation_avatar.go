package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"api/internal/platform/auth/model"
	"api/pkg/imageclient"
)

const (
	upstreamAvatarMaxBytes = 8 << 20
	upstreamAvatarBudget   = 8 * time.Second
)

var upstreamAvatarHTTP = &http.Client{
	CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to %s", req.URL.Scheme)
		}
		return nil
	},
}

func (s *FederationService) WithImageClient(c *imageclient.Client) *FederationService {
	s.imgClient = c
	return s
}

// adoptUpstreamAvatar copies the provider's picture into our own image service
// so the account has one from its first page view. Every failure is swallowed:
// an account is worth more than a picture, and the upstream URL is a third
// party we do not control.
//
// A user who already has an avatar keeps it — this runs on every federated
// login, not just registration, which is the only way the accounts created
// before it existed ever get one.
func (s *FederationService) adoptUpstreamAvatar(ctx context.Context, user *model.User, provider, rawURL string) {
	if s.imgClient == nil || user == nil || rawURL == "" {
		return
	}
	if (user.AvatarImageHash != nil && *user.AvatarImageHash != "") || strings.TrimSpace(user.Avatar) != "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, upstreamAvatarBudget)
	defer cancel()

	body, ext, err := fetchUpstreamAvatar(ctx, rawURL)
	if err != nil {
		slog.Warn("federation avatar fetch failed", "provider", provider, "user_id", user.ID, "err", err)
		return
	}
	defer body.Close()

	result, err := s.imgClient.Upload(ctx, body, provider+"-avatar"+ext, "avatar")
	if err != nil {
		slog.Warn("federation avatar upload failed", "provider", provider, "user_id", user.ID, "err", err)
		return
	}

	if err := s.userRepo.UpdateProfile(ctx, user.UUID, map[string]any{
		"avatar_image_hash": result.Hash,
	}); err != nil {
		slog.Warn("federation avatar persist failed", "provider", provider, "user_id", user.ID, "err", err)
		return
	}
	user.AvatarImageHash = &result.Hash
	slog.Info("federation avatar adopted", "provider", provider, "user_id", user.ID, "hash", result.Hash)
}

func fetchUpstreamAvatar(ctx context.Context, rawURL string) (io.ReadCloser, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", err
	}
	if u.Scheme != "https" {
		return nil, "", fmt.Errorf("avatar url is not https: %s", u.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", "NextMoe-Account/1.0")

	resp, err := upstreamAvatarHTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, "", fmt.Errorf("avatar fetch status %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		resp.Body.Close()
		return nil, "", fmt.Errorf("avatar content-type %q is not an image", ct)
	}

	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, upstreamAvatarMaxBytes), resp.Body}, avatarExt(ct), nil
}

func avatarExt(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/avif":
		return ".avif"
	default:
		return ".jpg"
	}
}
