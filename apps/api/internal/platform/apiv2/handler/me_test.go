package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/editspec"
	catsvc "api/internal/platform/catalog/service"
	"api/pkg/imageclient"

	"github.com/stretchr/testify/require"
)

func TestMePlaytimesRequireUserToken(t *testing.T) {
	app := testApp(t)
	status, ct, body := do(t, app, http.MethodGet, "/v2/me/playtimes")
	require.Equal(t, 401, status)
	require.Contains(t, ct, "application/problem+json")
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	require.Equal(t, problem.CodeMissingCredential, p.Code)

	req := httptest.NewRequest(http.MethodGet, "/v2/me/playtimes", nil)
	req.Header.Set("Authorization", "Bearer nm_live_notauser")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 401, resp.StatusCode)
}

func TestPlaytimeUnboundWithUser(t *testing.T) {
	ctx := contextWithUser(t.Context(), 7, "client-a")
	_, err := (*Catalog)(nil).ListPlaytimes(ctx, collect.Query{}, nil)
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("%v", err)
	}
	_, err = (&Catalog{}).GetPlaytime(ctx, 1)
	if p, ok = err.(*problem.Problem); !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("%v", err)
	}
}

func TestWriteOpsUnbound(t *testing.T) {
	ctx := contextWithUser(t.Context(), 7, "client-a")
	ctx = context.WithValue(ctx, ctxSite, "kungal")
	_, _, err := (*Catalog)(nil).CreateProposal(ctx, "catalog.work", "1", map[string]any{"catalog.work.display_name": "x"}, "")
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("proposal %v", err)
	}
	if err := requireIfMatch("", `"x"`); err == nil {
		t.Fatal("empty If-Match")
	}
	p, ok = requireIfMatch("", `"x"`).(*problem.Problem)
	if !ok || p.Code != problem.CodePreconditionRequired {
		t.Fatalf("if-match %v", err)
	}
}

func TestClaimsUnbound(t *testing.T) {
	ctx := contextWithUser(t.Context(), 7, "client-a")
	_, err := (*Catalog)(nil).ListMyClaims(ctx, collect.Query{}, myClaimFilter{})
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("%v", err)
	}
}

func TestModerationClaimsRequireSite(t *testing.T) {
	ctx := contextWithUser(t.Context(), 7, "client-a")
	cat := &Catalog{Claims: &catsvc.ClaimLifecycleService{}}
	_, err := cat.ListModerationClaims(ctx, collect.Query{}, "")
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeSiteNotBound {
		t.Fatalf("list %v", err)
	}
	_, err = cat.GetModerationClaim(ctx, 1)
	if p, ok = err.(*problem.Problem); !ok || p.Code != problem.CodeSiteNotBound {
		t.Fatalf("get %v", err)
	}
}

func TestCreateClaimRequiresWorkIDOrRefs(t *testing.T) {
	ctx := contextWithUser(t.Context(), 7, "client-a")
	ctx = context.WithValue(ctx, ctxSite, "kungal")
	cat := &Catalog{Claims: &catsvc.ClaimLifecycleService{}}
	_, err := cat.CreateClaim(ctx, "", "", "", nil, nil, catsvc.ReleaseDate{}, false)
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeValidationFailed {
		t.Fatalf("%v", err)
	}
	_, err = cat.CreateClaim(ctx, "", "", "", []repr.Ref{{Source: "vndb", ExternalID: "v1"}}, nil, catsvc.ReleaseDate{}, false)
	if p, ok = err.(*problem.Problem); !ok || p.Code != problem.CodeValidationFailed {
		t.Fatalf("refs without display_name %v", err)
	}
	_, err = cat.CreateClaim(ctx, "12", "", "", nil, map[string]any{editspec.FieldWorkOLang: "ja"}, catsvc.ReleaseDate{}, false)
	if p, ok = err.(*problem.Problem); !ok || p.Code != problem.CodeValidationFailed ||
		len(p.Errors) != 1 || p.Errors[0].Pointer != "/field_values" {
		t.Fatalf("work_id with fields %v", err)
	}
	_, err = cat.CreateClaim(ctx, "12", "", "", nil, nil, catsvc.ReleaseDate{Y: 2020}, false)
	if p, ok = err.(*problem.Problem); !ok || p.Code != problem.CodeValidationFailed ||
		len(p.Errors) != 1 || p.Errors[0].Pointer != "/released" {
		t.Fatalf("work_id with released %v", err)
	}
	_, err = cat.ListMyClaims(ctx, collect.Query{Cursor: "not-an-event-id"}, myClaimFilter{})
	if p, ok = err.(*problem.Problem); !ok || p.Code != problem.CodeInvalidCursor {
		t.Fatalf("cursor %v", err)
	}
}

func TestCoverVoteUnbound(t *testing.T) {
	ctx := contextWithUser(t.Context(), 7, "client-a")
	_, err := (*Catalog)(nil).ListCoverVotes(ctx)
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("%v", err)
	}
}

func TestEditImageSubCarriesTheTokenSite(t *testing.T) {
	ctx := contextWithUser(t.Context(), 7, "moyu-client")
	ctx = context.WithValue(ctx, ctxSite, "moyu")

	var gotSub string
	cat := &Catalog{Uploads: func(_ context.Context, _ io.Reader, _, _, sub string) (*imageclient.UploadResult, error) {
		gotSub = sub
		return &imageclient.UploadResult{Hash: "h", URL: "u"}, nil
	}}
	if _, err := cat.UploadEditImage(ctx, "cover", "c.png", strings.NewReader("x")); err != nil {
		t.Fatalf("upload: %v", err)
	}
	require.Equal(t, "moyu:7", gotSub)

	if _, err := (&Catalog{Uploads: cat.Uploads}).UploadEditImage(
		contextWithUser(t.Context(), 7, "no-site-client"), "cover", "c.png", strings.NewReader("x"),
	); err == nil {
		t.Fatal("a token whose client is not bound to a site must not upload")
	}
}

func contextWithUser(ctx context.Context, uid int64, client string) context.Context {
	ctx = context.WithValue(ctx, ctxUserID, uid)
	return context.WithValue(ctx, ctxClientID, client)
}
