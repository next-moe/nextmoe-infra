package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/protocol"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	"api/pkg/imageclient"

	"github.com/stretchr/testify/require"
)

type fakeNewsImages struct {
	known     map[string]bool
	uploads   int
	pings     int
	preset    string
	sub       string
	filename  string
	uploadErr error
	pingErr   error
}

func (f *fakeNewsImages) UploadWithSub(_ context.Context, r io.Reader, filename, preset, sub string) (*imageclient.UploadResult, error) {
	f.uploads++
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	b, _ := io.ReadAll(r)
	if len(b) == 0 {
		return nil, errors.New("the upload reader was empty")
	}
	f.preset, f.sub, f.filename = preset, sub, filename
	hash := strings.Repeat("b", 64)
	if f.known == nil {
		f.known = map[string]bool{}
	}
	f.known[hash] = true
	return &imageclient.UploadResult{
		Hash: hash, URL: "https://img.example.dev/image/" + hash + ".webp",
		Width: 1280, Height: 720, Thumbhash: "3PcNNYSFeXh", SizeBytes: 9001,
	}, nil
}

func (f *fakeNewsImages) ReferencePing(_ context.Context, hashes []string) (*imageclient.ReferencePingResult, error) {
	f.pings++
	if f.pingErr != nil {
		return nil, f.pingErr
	}
	res := &imageclient.ReferencePingResult{NotFound: []string{}}
	for _, h := range hashes {
		if f.known[h] {
			res.Updated++
		} else {
			res.NotFound = append(res.NotFound, h)
		}
	}
	return res, nil
}

func problemOf(t *testing.T, err error) *problem.Problem {
	t.Helper()
	p, ok := err.(*problem.Problem)
	require.True(t, ok, "%v", err)
	return p
}

func TestNewsImageUploadStoresUnderTheNewsPreset(t *testing.T) {
	ctx := contextWithUser(t.Context(), 7, "client-a")

	_, err := (&Catalog{}).UploadNewsImage(ctx, "b.png", strings.NewReader("bytes"))
	require.Equal(t, problem.CodeServiceUnavailable, problemOf(t, err).Code)

	store := &fakeNewsImages{}
	cat := &Catalog{NewsImages: store}
	rec, err := cat.UploadNewsImage(ctx, "b.png", strings.NewReader("bytes"))
	require.NoError(t, err)
	require.Equal(t, "news_banner", store.preset)
	require.Equal(t, "7", store.sub)
	require.Equal(t, "b.png", store.filename)
	require.Equal(t, "news_image", rec.Object)
	require.Equal(t, strings.Repeat("b", 64), rec.Hash)
	require.Equal(t, 1280, *rec.Width)
	require.Equal(t, 720, *rec.Height)
	require.Equal(t, int64(9001), rec.SizeBytes)

	_, err = cat.UploadNewsImage(t.Context(), "b.png", strings.NewReader("bytes"))
	require.Equal(t, problem.CodeUserIdentityRequired, problemOf(t, err).Code)
}

func TestNewsImageUploadIsCappedPerAccount(t *testing.T) {
	settings.Override(t, keys.CatalogNewsImageUploadsPerDay, int64(2))
	store := &fakeNewsImages{}
	cat := &Catalog{NewsImages: store, Counters: protocol.NewMemory()}
	alice := contextWithUser(t.Context(), 7, "client-a")
	bob := contextWithUser(t.Context(), 8, "client-a")

	for range 2 {
		_, err := cat.UploadNewsImage(alice, "b.png", strings.NewReader("bytes"))
		require.NoError(t, err)
	}
	_, err := cat.UploadNewsImage(alice, "b.png", strings.NewReader("bytes"))
	require.Equal(t, problem.CodeQuotaExceeded, problemOf(t, err).Code)
	require.Equal(t, 2, store.uploads, "a refused upload must not reach the image service")

	_, err = cat.UploadNewsImage(bob, "b.png", strings.NewReader("bytes"))
	require.NoError(t, err, "the cap is per account, not shared")

	settings.Override(t, keys.CatalogNewsImageUploadsPerDay, int64(0))
	_, err = cat.UploadNewsImage(contextWithUser(t.Context(), 9, "client-a"), "b.png", strings.NewReader("bytes"))
	require.Equal(t, problem.CodeQuotaExceeded, problemOf(t, err).Code, "0 closes the upload")
}

func TestImageUploadErrNamesTheFile(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   string
		reason string
	}{
		{fmt.Errorf("%w: image/gif", imageclient.ErrMIMEDenied), problem.CodeValidationFailed, problem.ReasonNotAllowedValue},
		{fmt.Errorf("%w: truncated", imageclient.ErrDecodeFailed), problem.CodeValidationFailed, problem.ReasonInvalidFormat},
		{fmt.Errorf("%w: nsfw", imageclient.ErrModerationRejected), problem.CodeValidationFailed, problem.ReasonNotAllowedValue},
		{fmt.Errorf("%w: daily", imageclient.ErrQuotaExceeded), problem.CodeQuotaExceeded, ""},
		{errors.New("connection refused"), problem.CodeServiceUnavailable, ""},
	} {
		p := problemOf(t, imageUploadErr(tc.err))
		require.Equal(t, tc.code, p.Code, tc.err)
		if tc.reason == "" {
			require.Empty(t, p.Errors, tc.err)
			continue
		}
		require.Len(t, p.Errors, 1, tc.err)
		require.Equal(t, "/file", p.Errors[0].Pointer, tc.err)
		require.Equal(t, tc.reason, p.Errors[0].Reason, tc.err)
	}
}

func TestNewsBannerMustBeHeldUnderTheNewsSite(t *testing.T) {
	ctx := t.Context()
	held := strings.Repeat("a", 64)
	foreign := strings.Repeat("c", 64)
	store := &fakeNewsImages{known: map[string]bool{held: true}}
	cat := &Catalog{NewsImages: store}

	errs, err := cat.newsBannerErrors(ctx, "/banner_hash", "")
	require.NoError(t, err)
	require.Empty(t, errs)
	errs, err = cat.newsBannerErrors(ctx, "/banner_hash", "zz")
	require.NoError(t, err)
	require.Len(t, errs, 1)
	require.Equal(t, problem.ReasonInvalidFormat, errs[0].Reason)
	require.Zero(t, store.pings, "an empty or malformed hash is settled without asking the image service")

	errs, err = cat.newsBannerErrors(ctx, "/banner_hash", held)
	require.NoError(t, err)
	require.Empty(t, errs)

	errs, err = cat.newsBannerErrors(ctx, "/banner_hash", foreign)
	require.NoError(t, err)
	require.Len(t, errs, 1)
	require.Equal(t, "/banner_hash", errs[0].Pointer)
	require.Equal(t, problem.ReasonUnknownReference, errs[0].Reason)

	store.pingErr = errors.New("connection refused")
	_, err = cat.newsBannerErrors(ctx, "/banner_hash", held)
	require.Equal(t, problem.CodeServiceUnavailable, problemOf(t, err).Code,
		"an unanswered check must not pass for a verified banner")

	_, err = (&Catalog{}).newsBannerErrors(ctx, "/banner_hash", held)
	require.Equal(t, problem.CodeServiceUnavailable, problemOf(t, err).Code)
	errs, err = (&Catalog{}).newsBannerErrors(ctx, "/banner_hash", "")
	require.NoError(t, err, "a submission without a banner needs no image leg")
	require.Empty(t, errs)
}
