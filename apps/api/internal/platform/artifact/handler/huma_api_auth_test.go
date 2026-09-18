package handler

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"

	artMW "api/internal/platform/artifact/middleware"
	"api/internal/platform/artifact/service"
	siteModel "api/internal/platform/site/model"
	"api/pkg/errors"
)

func callerCtx(method, sub string) context.Context {
	ctx := context.WithValue(context.Background(), ctxKeySite, "kungal")
	ctx = context.WithValue(ctx, ctxKeyClient, &siteModel.OAuthClient{ID: "c"})
	ctx = context.WithValue(ctx, ctxKeyUserSub, sub)
	return context.WithValue(ctx, ctxKeyMethod, method)
}

func TestUserTokenIsRefusedOnEverySiteWideOperation(t *testing.T) {
	s := &HumaServer{svc: service.New(nil, nil, nil)}
	ctx := callerCtx(artMW.MethodJWT, "alice")
	one := &uuidInput{UUID: "00000000-0000-0000-0000-000000000000"}

	ops := map[string]func() error{
		"list":     func() error { _, err := s.list(ctx, &listInput{}); return err },
		"get":      func() error { _, err := s.get(ctx, one); return err },
		"download": func() error { _, err := s.download(ctx, one); return err },
		"delete":   func() error { _, err := s.delete(ctx, one); return err },
	}
	for name, call := range ops {
		t.Run(name, func(t *testing.T) {
			var he *houseError
			if err := call(); !stderrors.As(err, &he) || he.status != http.StatusForbidden || he.Code != errors.ErrArtifactForbidden {
				t.Fatalf("%s with a user token = %v, want 403 / %d", name, err, errors.ErrArtifactForbidden)
			}
		})
	}
}

func TestSiteWideSiteAdmitsOnlyTheServerCredential(t *testing.T) {
	if site, err := siteWideSite(callerCtx(artMW.MethodBasic, "")); err != nil || site != "kungal" {
		t.Fatalf("basic caller: site=%q err=%v, want kungal/nil", site, err)
	}
	if _, err := siteWideSite(callerCtx(artMW.MethodJWT, "alice")); err == nil {
		t.Fatal("user-token caller was admitted site-wide")
	}
}

func TestUploadOwnerComesFromTheTokenOnly(t *testing.T) {
	if got := uploadOwner(callerCtx(artMW.MethodJWT, "alice")); got != "alice" {
		t.Fatalf("user-token caller owner = %q, want alice", got)
	}
	if got := uploadOwner(callerCtx(artMW.MethodBasic, "")); got != "" {
		t.Fatalf("server caller owner = %q, want unrestricted", got)
	}
}
