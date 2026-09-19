package devapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	authModel "api/internal/platform/auth/model"
	siteModel "api/internal/platform/site/model"
	storeModel "api/internal/platform/store/model"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// ArchiveApp deletes keys that never served a request, and then deletes the
// application because nothing is left pointing at it. Every fixture here that
// wants the archive arm rather than the delete arm has to give its key HISTORY,
// not merely mint it — the first version of this file minted and got the delete
// arm in six tests at once.
func giveKeyHistory(t *testing.T, repo *Repository, keyID uint) {
	t.Helper()
	if err := repo.TouchLastUsed(context.Background(), keyID, time.Now().UTC()); err != nil {
		t.Fatalf("touch last_used_at: %v", err)
	}
}

func loginRequest(want bool) *UserLoginRequest {
	if !want {
		return nil
	}
	return &UserLoginRequest{RedirectURIs: []string{"https://devapitest.example/callback"}}
}

func TestArchiveReleasesTheAppSlot(t *testing.T) {
	cleanupSelf(t)
	svc, _, repo, _ := newSelfService(t)
	ctx := context.Background()
	const owner = uint(1)

	ids := make([]string, 0, MaxAppsPerOwner)
	for i := 0; i < MaxAppsPerOwner; i++ {
		app, err := svc.CreateApp(ctx, owner, fmt.Sprintf("app %d", i), "", nil)
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		ids = append(ids, app.ID)
	}
	if _, err := svc.CreateApp(ctx, owner, "over the cap", "", nil); err != ErrAppLimitReached {
		t.Fatalf("app %d = %v, want ErrAppLimitReached", MaxAppsPerOwner+1, err)
	}

	// A used key is what keeps the archived application's row alive, so this is
	// the case where the slot has to be released by the dev_archived_at filter
	// rather than by the row disappearing.
	key, _, err := svc.MintKey(ctx, owner, ids[0], MintKeyInput{Name: "k"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	giveKeyHistory(t, repo, key.ID)
	if err := svc.ArchiveApp(ctx, owner, ids[0]); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := repo.GetApp(ctx, ids[0]); err != nil {
		t.Fatalf("an archived app with a key row must survive: %v", err)
	}

	n, err := repo.CountAppsByOwner(ctx, owner)
	if err != nil {
		t.Fatalf("count apps: %v", err)
	}
	if n != MaxAppsPerOwner-1 {
		t.Errorf("owner app count after archive = %d, want %d", n, MaxAppsPerOwner-1)
	}
	if _, err := svc.CreateApp(ctx, owner, "the slot came back", "", nil); err != nil {
		t.Fatalf("create after archive = %v, want the freed slot to be usable", err)
	}
}

func TestArchivePendingOrDeclinedReleasesTheSlot(t *testing.T) {
	cases := []struct {
		name       string
		wantStatus string
		decline    bool
	}{
		{"pending", AppReviewPending, false},
		{"declined", AppReviewDeclined, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanupSelf(t)
			svc, admin, repo, _ := newSelfService(t)
			ctx := context.Background()
			const owner, reviewer = uint(1), uint(9)

			approvalMode(t, admin)
			filed, err := svc.CreateApp(ctx, owner, "under review", "", nil)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if tc.decline {
				if _, err := admin.DeclineApp(ctx, filed.ID, reviewer, "not this time"); err != nil {
					t.Fatalf("decline: %v", err)
				}
			}
			current, err := repo.GetApp(ctx, filed.ID)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if current.DevReviewStatus != tc.wantStatus {
				t.Fatalf("status = %q, want %q", current.DevReviewStatus, tc.wantStatus)
			}
			if n, err := repo.CountAppsByOwner(ctx, owner); err != nil || n != 1 {
				t.Fatalf("count before archive = %d (%v), want 1 — an unapproved app holds a slot", n, err)
			}

			if err := svc.ArchiveApp(ctx, owner, filed.ID); err != nil {
				t.Fatalf("archive a %s app = %v, want it allowed", tc.name, err)
			}
			// MintKey refuses on an unapproved app, so a row that never left
			// review can never hold a reference: this is always the delete arm.
			if _, err := repo.GetApp(ctx, filed.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Errorf("archived %s app = %v, want the referenceless row deleted", tc.name, err)
			}
			n, err := repo.CountAppsByOwner(ctx, owner)
			if err != nil {
				t.Fatalf("count apps: %v", err)
			}
			if n != 0 {
				t.Errorf("owner app count after archive = %d, want 0", n)
			}
			if _, err := svc.CreateApp(ctx, owner, "file again", "", nil); err != nil {
				t.Fatalf("create after archiving a %s app = %v, want the slot back", tc.name, err)
			}
		})
	}
}

func TestArchiveDeletesAnApplicationWithNoHistory(t *testing.T) {
	cases := []struct {
		name     string
		login    bool
		mintKey  bool
		useKey   bool
		wantKept bool
	}{
		{"an application that never minted a key", false, false, false, false},
		{"an application whose only key never served a request", false, true, false, false},
		{"an application whose key served a request", false, true, true, true},
		// The one case where "referenceless" and "deletable" diverge: nothing
		// points at this row, but it could once have signed a user in, and the
		// rows that proves live in kun_catalog where this package cannot look.
		{"an application that offers user login", true, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanupSelf(t)
			svc, _, repo, _ := newSelfService(t)
			ctx := context.Background()
			const owner = uint(1)

			app, err := svc.CreateApp(ctx, owner, tc.name, "", loginRequest(tc.login))
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			var keyID uint
			if tc.mintKey {
				key, _, err := svc.MintKey(ctx, owner, app.ID, MintKeyInput{Name: "k"})
				if err != nil {
					t.Fatalf("mint: %v", err)
				}
				keyID = key.ID
				if tc.useKey {
					giveKeyHistory(t, repo, keyID)
				}
			}

			refs, err := repo.AppReferences(ctx, app.ID)
			if err != nil {
				t.Fatalf("references: %v", err)
			}
			if refs.LoginClient != tc.login {
				t.Fatalf("login_client = %v, want %v — CreateApp with a nil login must leave grants, redirect_uris and is_public empty",
					refs.LoginClient, tc.login)
			}

			if err := svc.ArchiveApp(ctx, owner, app.ID); err != nil {
				t.Fatalf("archive: %v", err)
			}

			reloaded, appErr := repo.GetApp(ctx, app.ID)
			if !tc.wantKept {
				if !errors.Is(appErr, gorm.ErrRecordNotFound) {
					t.Errorf("app row = %v, want it deleted (gorm.ErrRecordNotFound)", appErr)
				}
				if tc.mintKey {
					if _, err := repo.GetKey(ctx, keyID); !errors.Is(err, gorm.ErrRecordNotFound) {
						t.Errorf("key row = %v, want the unused key deleted with its app", err)
					}
				}
			} else {
				if appErr != nil {
					t.Fatalf("app row = %v, want it kept", appErr)
				}
				if reloaded.DevArchivedAt == nil || reloaded.DevEnabled {
					t.Errorf("kept row = archived_at %v / enabled %v, want stamped and disabled",
						reloaded.DevArchivedAt, reloaded.DevEnabled)
				}
				if tc.mintKey {
					if _, err := repo.GetKey(ctx, keyID); err != nil {
						t.Errorf("key row = %v, want the used key kept", err)
					}
				}
				if _, err := repo.GetAppByOwner(ctx, app.ID, owner); !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Errorf("owner-scoped get of the archived app = %v, want gorm.ErrRecordNotFound", err)
				}
			}

			mine, err := svc.ListApps(ctx, owner)
			if err != nil {
				t.Fatalf("owner list: %v", err)
			}
			if len(mine) != 0 {
				t.Errorf("owner list = %d apps, want the archived one gone either way", len(mine))
			}
			if n, err := repo.CountAppsByOwner(ctx, owner); err != nil || n != 0 {
				t.Errorf("owner app count = %d (%v), want 0 — the slot is released either way", n, err)
			}
		})
	}
}

func TestDeleteKeyGuard(t *testing.T) {
	cleanupSelf(t)
	svc, _, repo, _ := newSelfService(t)
	ctx := context.Background()
	const owner = uint(1)

	app, err := svc.CreateApp(ctx, owner, "key delete", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	revoke := func(t *testing.T, keyID uint) {
		t.Helper()
		if _, err := svc.RevokeKey(ctx, owner, app.ID, keyID); err != nil {
			t.Fatalf("revoke: %v", err)
		}
	}
	cases := []struct {
		name string
		prep func(t *testing.T, keyID uint)
		want error
	}{
		{"an active key", func(*testing.T, uint) {}, ErrKeyNotRevoked},
		{"a revoked key with last_used_at", func(t *testing.T, keyID uint) {
			revoke(t, keyID)
			if err := repo.TouchLastUsed(ctx, keyID, time.Now().UTC()); err != nil {
				t.Fatalf("touch last_used_at: %v", err)
			}
			if n, err := repo.CountUsageByKey(ctx, keyID); err != nil || n != 0 {
				t.Fatalf("usage rows = %d (%v), want 0 so only the timestamp can refuse", n, err)
			}
		}, ErrKeyHasHistory},
		{"a revoked key with metered usage", func(t *testing.T, keyID uint) {
			revoke(t, keyID)
			rec := NewUsageRecorder(repo, newMemStore())
			rec.Record(&Credential{KeyID: keyID, ClientID: app.ID}, "v2", "/v2/catalog/works", 200)
			if err := rec.Flush(ctx); err != nil {
				t.Fatalf("flush usage: %v", err)
			}
			stored, err := repo.GetKey(ctx, keyID)
			if err != nil {
				t.Fatalf("reload key: %v", err)
			}
			if stored.LastUsedAt != nil {
				t.Fatalf("last_used_at = %v, want NULL so only the usage row can refuse", stored.LastUsedAt)
			}
			if n, err := repo.CountUsageByKey(ctx, keyID); err != nil || n != 1 {
				t.Fatalf("usage rows = %d (%v), want 1", n, err)
			}
		}, ErrKeyHasHistory},
		{"a revoked key that never served a request", revoke, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, plaintext, err := svc.MintKey(ctx, owner, app.ID, MintKeyInput{Name: tc.name})
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			tc.prep(t, key.ID)

			found, err := svc.DeleteKey(ctx, owner, app.ID, key.ID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("delete %s = %v, want %v", tc.name, err, tc.want)
			}
			if !found {
				t.Fatalf("delete reported the owner's own key as not found")
			}
			if tc.want != nil {
				if _, err := repo.GetKey(ctx, key.ID); err != nil {
					t.Errorf("a refused delete must leave the key row in place, got %v", err)
				}
				return
			}
			if _, err := repo.GetKey(ctx, key.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Errorf("deleted key row = %v, want gorm.ErrRecordNotFound", err)
			}
			if c, _ := repo.ResolveByHash(ctx, HashKey(plaintext), time.Now()); c != nil {
				t.Errorf("a deleted key must not resolve")
			}
		})
	}
}

func TestDeleteAppGuard(t *testing.T) {
	cleanupSelf(t)
	svc, admin, repo, _ := newSelfService(t)
	ctx := context.Background()
	const owner = uint(1)

	site := siteModel.Site{Name: "first party", Domain: "devapitest-lifecycle.example"}
	if err := testDB.Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	user := authModel.User{Name: "devapitest_life", Email: "devapitest-lifecycle@example.invalid"}
	if err := testDB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		testDB.Exec(`DELETE FROM sessions WHERE user_id = ?`, user.ID)
		testDB.Unscoped().Delete(&authModel.User{}, user.ID)
		testDB.Exec(`DELETE FROM store_purchase_links WHERE alias LIKE 'devapitest-%'`)
		testDB.Exec(`DELETE FROM oauth_clients WHERE site_id = ?`, site.ID)
		testDB.Delete(&siteModel.Site{}, site.ID)
	})

	archive := func(t *testing.T, clientID string) {
		t.Helper()
		if _, err := admin.ArchiveApp(ctx, clientID); err != nil {
			t.Fatalf("archive: %v", err)
		}
	}
	cases := []struct {
		name  string
		login bool
		prep  func(t *testing.T, clientID string)
		// wantRefs pins which reference the refusal came from. Without it every
		// arm would pass on any non-empty AppReferences, so an arm whose fixture
		// silently failed to land would still be green off a neighbour's row.
		wantRefs AppReferences
		want     error
	}{
		{"a live application", false, func(*testing.T, string) {}, AppReferences{}, ErrAppNotArchived},
		{"an archived application still holding a key", false, func(t *testing.T, clientID string) {
			if _, _, err := svc.MintKey(ctx, owner, clientID, MintKeyInput{Name: "k"}); err != nil {
				t.Fatalf("mint: %v", err)
			}
			archive(t, clientID)
		}, AppReferences{Keys: 1}, ErrAppHasReferences},
		{"an archived application with a login session", false, func(t *testing.T, clientID string) {
			session := authModel.Session{
				UserID:       user.ID,
				ClientID:     clientID,
				SessionToken: "devapitest-session-" + clientID,
				RefreshToken: "devapitest-refresh-" + clientID,
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			if err := testDB.Create(&session).Error; err != nil {
				t.Fatalf("create session: %v", err)
			}
			archive(t, clientID)
		}, AppReferences{Logins: 1}, ErrAppHasReferences},
		{"an archived application with a store link", false, func(t *testing.T, clientID string) {
			link := storeModel.PurchaseLink{
				ClientID:  clientID,
				ProductID: "RJ01000000",
				Alias:     "devapitest-" + clientID,
				ShortURL:  "https://s.example.invalid/devapitest-" + clientID,
			}
			if err := testDB.Create(&link).Error; err != nil {
				t.Fatalf("create purchase link: %v", err)
			}
			archive(t, clientID)
		}, AppReferences{StoreLinks: 1}, ErrAppHasReferences},
		{"an archived first-party login client", false, func(t *testing.T, clientID string) {
			if err := repo.UpdateAppFields(ctx, clientID, map[string]any{"site_id": site.ID}); err != nil {
				t.Fatalf("bind to a site: %v", err)
			}
			archive(t, clientID)
		}, AppReferences{BoundSite: true}, ErrAppHasReferences},
		{"an archived application that offers user login", true, func(t *testing.T, clientID string) {
			archive(t, clientID)
		}, AppReferences{LoginClient: true}, ErrAppHasReferences},
		{"an archived shell nothing points at", false, func(t *testing.T, clientID string) {
			archive(t, clientID)
		}, AppReferences{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, err := svc.CreateApp(ctx, owner, tc.name, "", loginRequest(tc.login))
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			tc.prep(t, app.ID)

			refs, err := repo.AppReferences(ctx, app.ID)
			if err != nil {
				t.Fatalf("references: %v", err)
			}
			if refs != tc.wantRefs {
				t.Fatalf("references for %s = %+v, want %+v", tc.name, refs, tc.wantRefs)
			}
			if err := admin.DeleteApp(ctx, app.ID); !errors.Is(err, tc.want) {
				t.Fatalf("delete %s = %v, want %v", tc.name, err, tc.want)
			}
			_, getErr := repo.GetApp(ctx, app.ID)
			if tc.want != nil {
				if getErr != nil {
					t.Errorf("a refused delete must leave the row in place, got %v", getErr)
				}
				return
			}
			if !errors.Is(getErr, gorm.ErrRecordNotFound) {
				t.Errorf("deleted app row = %v, want gorm.ErrRecordNotFound", getErr)
			}
		})
	}
}

func TestEnableUnarchivesApp(t *testing.T) {
	cleanupSelf(t)
	svc, admin, repo, _ := newSelfService(t)
	ctx := context.Background()
	const owner = uint(1)

	app, err := svc.CreateApp(ctx, owner, "back in service", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	key, _, err := svc.MintKey(ctx, owner, app.ID, MintKeyInput{Name: "k"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	giveKeyHistory(t, repo, key.ID)
	if err := svc.ArchiveApp(ctx, owner, app.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if n, err := repo.CountAppsByOwner(ctx, owner); err != nil || n != 0 {
		t.Fatalf("count while archived = %d (%v), want 0", n, err)
	}

	enabled := true
	restored, err := admin.UpdateAppConfig(ctx, app.ID, AppConfig{DevEnabled: &enabled})
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if restored.DevArchivedAt != nil {
		t.Errorf("dev_archived_at = %v after enable, want NULL", restored.DevArchivedAt)
	}
	if !restored.DevEnabled || restored.DevReviewStatus != AppReviewApproved {
		t.Errorf("restored row = enabled %v / %q, want enabled + approved",
			restored.DevEnabled, restored.DevReviewStatus)
	}

	mine, err := svc.ListApps(ctx, owner)
	if err != nil {
		t.Fatalf("owner list: %v", err)
	}
	if len(mine) != 1 || mine[0].Client.ID != app.ID {
		t.Fatalf("owner list = %d apps, want the un-archived one back", len(mine))
	}
	if n, err := repo.CountAppsByOwner(ctx, owner); err != nil || n != 1 {
		t.Errorf("count after enable = %d (%v), want 1 — an un-archived app takes its slot back", n, err)
	}
}

func TestStoreSettlementEligibleRoundTrip(t *testing.T) {
	cleanupSelf(t)
	svc, admin, repo, _ := newSelfService(t)
	ctx := context.Background()
	const owner = uint(1)

	app, err := svc.CreateApp(ctx, owner, "settlement", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	stored, err := repo.GetApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("reload new: %v", err)
	}
	if !app.StoreSettlementEligible || !stored.StoreSettlementEligible {
		t.Fatalf("a new application joins the settlement roster (returned %v, stored %v)",
			app.StoreSettlementEligible, stored.StoreSettlementEligible)
	}
	key, _, err := svc.MintKey(ctx, owner, app.ID, MintKeyInput{Name: "k", Scopes: []string{ScopeStoreRead}})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	giveKeyHistory(t, repo, key.ID)

	for _, want := range []bool{false, true} {
		flag := want
		updated, err := admin.UpdateAppConfig(ctx, app.ID, AppConfig{StoreSettlementEligible: &flag})
		if err != nil {
			t.Fatalf("set store_settlement_eligible=%v: %v", want, err)
		}
		if updated.StoreSettlementEligible != want {
			t.Errorf("UpdateAppConfig returned store_settlement_eligible=%v, want %v",
				updated.StoreSettlementEligible, want)
		}
		reloaded, err := repo.GetApp(ctx, app.ID)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if reloaded.StoreSettlementEligible != want {
			t.Errorf("stored store_settlement_eligible=%v, want %v", reloaded.StoreSettlementEligible, want)
		}
	}

	if err := svc.ArchiveApp(ctx, owner, app.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	archived, err := repo.GetApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("reload after archive: %v", err)
	}
	if archived.StoreSettlementEligible {
		t.Errorf("archiving must take the application off the settlement roster")
	}
}

func TestAdminMintKeyRefusesAnArchivedApp(t *testing.T) {
	cleanupSelf(t)
	svc, admin, repo, _ := newSelfService(t)
	ctx := context.Background()
	const owner, operator = uint(1), uint(9)

	app, err := svc.CreateApp(ctx, owner, "archived by the operator", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := admin.MintKey(ctx, app.ID, MintKeyInput{Name: "while live"}, operator); err != nil {
		t.Fatalf("mint on a live app = %v, want it allowed", err)
	}
	if _, err := admin.ArchiveApp(ctx, app.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	if _, _, err := admin.MintKey(ctx, app.ID, MintKeyInput{Name: "after archive"}, operator); !errors.Is(err, ErrAppArchived) {
		t.Fatalf("mint on an archived app = %v, want ErrAppArchived", err)
	}
	keys, err := repo.ListKeysByClient(ctx, app.ID)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("key rows = %d, want 1 — a refused mint must not write the row that makes the app undeletable", len(keys))
	}

	fiberApp := fiber.New()
	noop := func(c fiber.Ctx) error { return c.Next() }
	NewAdminHandler(admin).Register(fiberApp.Group("/admin/devapi", fakeAuth(operator)), noop)
	req := httptest.NewRequest("POST", "/admin/devapi/apps/"+app.ID+"/keys", bytes.NewBufferString(`{"name":"over http"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := fiberApp.Test(req)
	if err != nil {
		t.Fatalf("http mint: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != fiber.StatusConflict {
		t.Errorf("console mint on an archived app = %d, want 409", resp.StatusCode)
	}

	enabled := true
	if _, err := admin.UpdateAppConfig(ctx, app.ID, AppConfig{DevEnabled: &enabled}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if _, _, err := admin.MintKey(ctx, app.ID, MintKeyInput{Name: "after re-enable"}, operator); err != nil {
		t.Fatalf("mint after re-enabling = %v, want the door open again", err)
	}
}
