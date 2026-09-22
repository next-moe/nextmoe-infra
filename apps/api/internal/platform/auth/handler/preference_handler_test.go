package handler

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/auth/service"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"
	"api/pkg/errors"

	"github.com/gofiber/fiber/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type caller struct {
	uuid     string
	id       uint
	clientID string
	scope    string
}

type prefEnv struct {
	app     *fiber.App
	db      *gorm.DB
	user    *model.User
	oauth   *service.OAuthService
	current *caller
}

func newPrefEnv(t *testing.T) *prefEnv {
	t.Helper()

	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "open database: %v", err)
	}
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		dbtest.Skipf(t, "pgcrypto: %v", err)
	}
	if err := model.AddUserContentPreferenceColumns(db); err != nil {
		dbtest.Skipf(t, "content preference columns: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserPreference{}); err != nil {
		dbtest.Skipf(t, "migrate: %v", err)
	}
	if err := model.EnsureNSFWDisplayCheck(db); err != nil {
		t.Fatalf("nsfw_display check constraint: %v", err)
	}

	env := &prefEnv{db: db}

	tag := randomTail()
	user := &model.User{Name: "pref_" + tag, Email: "pref_" + tag + "@example.test"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		db.Where("user_id = ?", user.ID).Delete(&model.UserPreference{})
		db.Unscoped().Delete(&model.User{}, user.ID)
	})
	if err := db.First(user, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	env.user = user
	env.current = &caller{uuid: user.UUID, id: user.ID}

	userRepo := repository.NewUserRepository(db)
	prefSvc := service.NewPreferenceService(userRepo, repository.NewUserPreferenceRepository(db))
	h := NewPreferenceHandler(prefSvc)

	cfg := &config.Config{}
	env.oauth = service.NewOAuthService(userRepo, nil, nil, nil, nil, cfg)

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_uuid", env.current.uuid)
		c.Locals("user_id", env.current.id)
		c.Locals("token_client_id", env.current.clientID)
		c.Locals("user_scope", env.current.scope)
		return c.Next()
	})
	app.Post("/auth/me/adult-confirmation", h.ConfirmAdult)
	app.Put("/auth/me/nsfw", h.SetNSFWDisplay)
	app.Get("/auth/me/preferences", h.List)
	app.Get("/auth/me/preferences/:namespace", h.Get)
	app.Put("/auth/me/preferences/:namespace", h.Put)
	app.Delete("/auth/me/preferences/:namespace", h.Delete)
	env.app = app

	return env
}

func randomTail() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (e *prefEnv) as(c caller) { e.current = &c }

func (e *prefEnv) asSession() { e.as(caller{uuid: e.user.UUID, id: e.user.ID}) }

func (e *prefEnv) asClient(clientID, scope string) {
	e.as(caller{uuid: e.user.UUID, id: e.user.ID, clientID: clientID, scope: scope})
}

type reply struct {
	status  int
	header  http.Header
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *prefEnv) call(t *testing.T, method, path, body string, headers map[string]string) reply {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := e.app.Test(req, fiber.TestConfig{Timeout: 0})
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	out := reply{status: resp.StatusCode, header: resp.Header}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s %s body %q is not the house envelope: %v", method, path, raw, err)
		}
	}
	return out
}

type docPayload struct {
	Namespace string          `json:"namespace"`
	Doc       json.RawMessage `json:"doc"`
	Version   int             `json:"version"`
	UpdatedAt *string         `json:"updated_at"`
}

func (r reply) doc(t *testing.T) docPayload {
	t.Helper()
	var d docPayload
	if err := json.Unmarshal(r.Data, &d); err != nil {
		t.Fatalf("data %q is not a preference document: %v", r.Data, err)
	}
	return d
}

func TestPreferenceKVLifecycle(t *testing.T) {
	env := newPrefEnv(t)
	env.asSession()

	t.Run("a namespace that was never written reads as an empty doc at version 0", func(t *testing.T) {
		r := env.call(t, "GET", "/auth/me/preferences/never-written", "", nil)
		if r.status != http.StatusOK || r.Code != 0 {
			t.Fatalf("got %d code=%d, want 200 code=0", r.status, r.Code)
		}
		d := r.doc(t)
		if string(d.Doc) != "{}" || d.Version != 0 || d.UpdatedAt != nil {
			t.Fatalf("got %+v, want {} at version 0 with no updated_at", d)
		}
		if got := r.header.Get("ETag"); got != `"0"` {
			t.Fatalf("ETag = %q, want \"0\"", got)
		}
	})

	t.Run("the first write is version 1 and every write after it adds one", func(t *testing.T) {
		for want := 1; want <= 3; want++ {
			r := env.call(t, "PUT", "/auth/me/preferences/global",
				fmt.Sprintf(`{"doc":{"n":%d}}`, want), nil)
			if r.status != http.StatusOK {
				t.Fatalf("write %d: status %d code %d", want, r.status, r.Code)
			}
			if got := r.doc(t).Version; got != want {
				t.Fatalf("write %d landed at version %d", want, got)
			}
		}
		d := env.call(t, "GET", "/auth/me/preferences/global", "", nil).doc(t)
		if string(d.Doc) != `{"n":3}` || d.Version != 3 {
			t.Fatalf("read back %+v, want {\"n\":3} at version 3", d)
		}
	})

	t.Run("If-Match on the current version writes, on a stale one is 412", func(t *testing.T) {
		stale := env.call(t, "PUT", "/auth/me/preferences/global", `{"doc":{"n":99}}`,
			map[string]string{"If-Match": `"1"`})
		if stale.status != http.StatusPreconditionFailed || stale.Code != errors.ErrPrefVersionConflict {
			t.Fatalf("stale If-Match = %d code=%d, want 412 code=%d",
				stale.status, stale.Code, errors.ErrPrefVersionConflict)
		}
		unchanged := env.call(t, "GET", "/auth/me/preferences/global", "", nil).doc(t)
		if unchanged.Version != 3 || string(unchanged.Doc) != `{"n":3}` {
			t.Fatalf("a rejected write still changed the row: %+v", unchanged)
		}

		fresh := env.call(t, "PUT", "/auth/me/preferences/global", `{"doc":{"n":4}}`,
			map[string]string{"If-Match": `"3"`})
		if fresh.status != http.StatusOK || fresh.doc(t).Version != 4 {
			t.Fatalf("matching If-Match = %d, version %d, want 200 at version 4",
				fresh.status, fresh.doc(t).Version)
		}
	})

	t.Run("If-Match 0 claims the first write and loses to an existing row", func(t *testing.T) {
		first := env.call(t, "PUT", "/auth/me/preferences/claimed", `{"doc":{"a":1}}`,
			map[string]string{"If-Match": `"0"`})
		if first.status != http.StatusOK || first.doc(t).Version != 1 {
			t.Fatalf("If-Match 0 on an absent row = %d version %d, want 200 at 1",
				first.status, first.doc(t).Version)
		}
		again := env.call(t, "PUT", "/auth/me/preferences/claimed", `{"doc":{"a":2}}`,
			map[string]string{"If-Match": `"0"`})
		if again.status != http.StatusPreconditionFailed {
			t.Fatalf("If-Match 0 on an existing row = %d, want 412", again.status)
		}
	})

	t.Run("an If-Match that is not a version number is a 400", func(t *testing.T) {
		r := env.call(t, "PUT", "/auth/me/preferences/global", `{"doc":{}}`,
			map[string]string{"If-Match": "*"})
		if r.status != http.StatusBadRequest {
			t.Fatalf("If-Match: * = %d, want 400", r.status)
		}
	})

	t.Run("the list names every namespace with its version, size and time", func(t *testing.T) {
		r := env.call(t, "GET", "/auth/me/preferences", "", nil)
		var rows []struct {
			Namespace string `json:"namespace"`
			Version   int    `json:"version"`
			UpdatedAt string `json:"updated_at"`
			SizeBytes int64  `json:"size_bytes"`
		}
		if err := json.Unmarshal(r.Data, &rows); err != nil {
			t.Fatalf("list data %q: %v", r.Data, err)
		}
		found := map[string]int{}
		for _, row := range rows {
			found[row.Namespace] = row.Version
			if row.SizeBytes <= 0 || row.UpdatedAt == "" {
				t.Fatalf("row %+v is missing its size or time", row)
			}
		}
		if found["global"] != 4 || found["claimed"] != 1 {
			t.Fatalf("list = %v, want global at 4 and claimed at 1", found)
		}
		if _, ok := found["never-written"]; ok {
			t.Fatal("a namespace that was only read must not appear in the list")
		}
	})

	t.Run("delete removes the row and is idempotent", func(t *testing.T) {
		for range 2 {
			if r := env.call(t, "DELETE", "/auth/me/preferences/claimed", "", nil); r.status != http.StatusOK {
				t.Fatalf("delete = %d, want 200", r.status)
			}
		}
		if d := env.call(t, "GET", "/auth/me/preferences/claimed", "", nil).doc(t); d.Version != 0 {
			t.Fatalf("after delete the namespace reads at version %d, want 0", d.Version)
		}
	})

	t.Run("a doc that is not a JSON object is rejected", func(t *testing.T) {
		for _, body := range []string{`{"doc":null}`, `{"doc":[]}`, `{"doc":"x"}`, `{}`} {
			r := env.call(t, "PUT", "/auth/me/preferences/global", body, nil)
			if r.status != http.StatusBadRequest || r.Code != errors.ErrPrefDocInvalid {
				t.Fatalf("body %s = %d code=%d, want 400 code=%d",
					body, r.status, r.Code, errors.ErrPrefDocInvalid)
			}
		}
	})

	t.Run("a doc over 64 KB is rejected and nothing is stored", func(t *testing.T) {
		before := env.call(t, "GET", "/auth/me/preferences/global", "", nil).doc(t)
		oversize := fmt.Sprintf(`{"doc":{"k":%q}}`, strings.Repeat("x", model.PreferenceMaxDocBytes))
		r := env.call(t, "PUT", "/auth/me/preferences/global", oversize, nil)
		if r.status != http.StatusRequestEntityTooLarge || r.Code != errors.ErrPrefDocTooLarge {
			t.Fatalf("oversize doc = %d code=%d, want 413 code=%d",
				r.status, r.Code, errors.ErrPrefDocTooLarge)
		}
		after := env.call(t, "GET", "/auth/me/preferences/global", "", nil).doc(t)
		if after.Version != before.Version {
			t.Fatalf("a rejected oversize write bumped the version %d -> %d", before.Version, after.Version)
		}
	})

	t.Run("an invalid namespace is a 400 before anything is read", func(t *testing.T) {
		r := env.call(t, "GET", "/auth/me/preferences/NotLowercase", "", nil)
		if r.status != http.StatusBadRequest || r.Code != errors.ErrPrefNamespaceInvalid {
			t.Fatalf("got %d code=%d, want 400 code=%d",
				r.status, r.Code, errors.ErrPrefNamespaceInvalid)
		}
	})
}

func TestPreferenceScopeAndNamespaceBinding(t *testing.T) {
	env := newPrefEnv(t)

	t.Run("an OAuth token without the preferences scope is refused", func(t *testing.T) {
		env.asClient("client-a", "openid profile email")
		for _, call := range [][3]string{
			{"GET", "/auth/me/preferences/client-a", ""},
			{"PUT", "/auth/me/preferences/client-a", `{"doc":{}}`},
			{"DELETE", "/auth/me/preferences/client-a", ""},
			{"POST", "/auth/me/adult-confirmation", ""},
			{"PUT", "/auth/me/nsfw", `{"nsfw_display":"hide"}`},
		} {
			r := env.call(t, call[0], call[1], call[2], nil)
			if r.status != http.StatusForbidden || r.Code != errors.ErrPrefScopeRequired {
				t.Fatalf("%s %s = %d code=%d, want 403 code=%d",
					call[0], call[1], r.status, r.Code, errors.ErrPrefScopeRequired)
			}
		}
	})

	t.Run("an OAuth token that negotiated no scope at all is refused too", func(t *testing.T) {
		env.asClient("client-a", "")
		r := env.call(t, "GET", "/auth/me/preferences/client-a", "", nil)
		if r.status != http.StatusForbidden || r.Code != errors.ErrPrefScopeRequired {
			t.Fatalf("empty-scope OAuth token = %d code=%d, want 403 code=%d",
				r.status, r.Code, errors.ErrPrefScopeRequired)
		}
	})

	t.Run("with the scope a client reaches its own namespace and global", func(t *testing.T) {
		env.asClient("client-a", "openid profile preferences")
		for _, ns := range []string{"client-a", "global"} {
			w := env.call(t, "PUT", "/auth/me/preferences/"+ns, `{"doc":{"from":"a"}}`, nil)
			if w.status != http.StatusOK {
				t.Fatalf("PUT %s = %d code=%d", ns, w.status, w.Code)
			}
			if r := env.call(t, "GET", "/auth/me/preferences/"+ns, "", nil); r.status != http.StatusOK {
				t.Fatalf("GET %s = %d code=%d", ns, r.status, r.Code)
			}
		}
	})

	t.Run("a client cannot touch another client's namespace", func(t *testing.T) {
		env.asClient("client-a", "openid preferences")
		for _, call := range [][3]string{
			{"GET", "/auth/me/preferences/client-b", ""},
			{"PUT", "/auth/me/preferences/client-b", `{"doc":{"stolen":true}}`},
			{"DELETE", "/auth/me/preferences/client-b", ""},
		} {
			r := env.call(t, call[0], call[1], call[2], nil)
			if r.status != http.StatusForbidden || r.Code != errors.ErrPrefNamespaceDenied {
				t.Fatalf("%s %s = %d code=%d, want 403 code=%d",
					call[0], call[1], r.status, r.Code, errors.ErrPrefNamespaceDenied)
			}
		}

		env.asSession()
		if d := env.call(t, "GET", "/auth/me/preferences/client-b", "", nil).doc(t); d.Version != 0 {
			t.Fatalf("the refused cross-client write landed anyway: %+v", d)
		}
	})

	t.Run("the namespace listing is first-party only", func(t *testing.T) {
		env.asClient("client-a", "openid preferences")
		r := env.call(t, "GET", "/auth/me/preferences", "", nil)
		if r.status != http.StatusForbidden || r.Code != errors.ErrPrefNamespaceDenied {
			t.Fatalf("OAuth list = %d code=%d, want 403 code=%d",
				r.status, r.Code, errors.ErrPrefNamespaceDenied)
		}

		env.asSession()
		if r := env.call(t, "GET", "/auth/me/preferences", "", nil); r.status != http.StatusOK {
			t.Fatalf("session list = %d code=%d, want 200", r.status, r.Code)
		}
	})

	t.Run("a session reaches any namespace, including another client's", func(t *testing.T) {
		env.asSession()
		if r := env.call(t, "PUT", "/auth/me/preferences/client-b", `{"doc":{"ok":true}}`, nil); r.status != http.StatusOK {
			t.Fatalf("session write to client-b = %d code=%d", r.status, r.Code)
		}
		if r := env.call(t, "DELETE", "/auth/me/preferences/client-b", "", nil); r.status != http.StatusOK {
			t.Fatalf("session delete of client-b = %d code=%d", r.status, r.Code)
		}
	})
}

func TestNSFWDisplayNeedsNoAttestation(t *testing.T) {
	env := newPrefEnv(t)
	env.asSession()

	// Accounts are adult by construction since 2026-09-23, so the hook has
	// already stamped this row. Clear it: the state the retired 18008 guard
	// keyed on is still constructible, and the endpoint must no longer care.
	if err := env.db.Model(&model.User{}).Where("id = ?", env.user.ID).
		Update("adult_confirmed_at", nil).Error; err != nil {
		t.Fatalf("clear the attestation: %v", err)
	}

	t.Run("every display value is accepted on a row with no attestation", func(t *testing.T) {
		for _, v := range []string{model.NSFWDisplayBlur, model.NSFWDisplayShow, model.NSFWDisplayHide} {
			r := env.call(t, "PUT", "/auth/me/nsfw", fmt.Sprintf(`{"nsfw_display":%q}`, v), nil)
			if r.status != http.StatusOK {
				t.Fatalf("%q = %d code=%d, want 200", v, r.status, r.Code)
			}
			var stored model.User
			if err := env.db.First(&stored, env.user.ID).Error; err != nil {
				t.Fatalf("reload: %v", err)
			}
			if stored.NSFWDisplay != v {
				t.Fatalf("stored nsfw_display = %q, want %q", stored.NSFWDisplay, v)
			}
		}
	})

	t.Run("an unknown display value is rejected without touching the row", func(t *testing.T) {
		r := env.call(t, "PUT", "/auth/me/nsfw", `{"nsfw_display":"reveal"}`, nil)
		if r.status != http.StatusBadRequest || r.Code != errors.ErrPrefDisplayInvalid {
			t.Fatalf("got %d code=%d, want 400 code=%d", r.status, r.Code, errors.ErrPrefDisplayInvalid)
		}
	})

	// Vestigial but alive: downstream sites that shipped before the retirement
	// still call it, and they must keep getting a timestamp back.
	t.Run("confirmation is idempotent and never moves the timestamp", func(t *testing.T) {
		first := env.call(t, "POST", "/auth/me/adult-confirmation", "", nil)
		if first.status != http.StatusOK {
			t.Fatalf("first confirmation = %d code=%d", first.status, first.Code)
		}
		var firstAt struct {
			AdultConfirmedAt string `json:"adult_confirmed_at"`
		}
		if err := json.Unmarshal(first.Data, &firstAt); err != nil || firstAt.AdultConfirmedAt == "" {
			t.Fatalf("first confirmation data %q: %v", first.Data, err)
		}

		second := env.call(t, "POST", "/auth/me/adult-confirmation", "", nil)
		var secondAt struct {
			AdultConfirmedAt string `json:"adult_confirmed_at"`
		}
		if err := json.Unmarshal(second.Data, &secondAt); err != nil {
			t.Fatalf("second confirmation data %q: %v", second.Data, err)
		}
		if second.status != http.StatusOK || secondAt.AdultConfirmedAt != firstAt.AdultConfirmedAt {
			t.Fatalf("second confirmation = %d at %q, want 200 at the first %q",
				second.status, secondAt.AdultConfirmedAt, firstAt.AdultConfirmedAt)
		}
	})

	t.Run("the CHECK constraint refuses a fourth value outside the API", func(t *testing.T) {
		err := env.db.Exec(`UPDATE users SET nsfw_display = 'reveal' WHERE id = ?`, env.user.ID).Error
		if err == nil {
			t.Fatal("the database accepted a display value the API does not define")
		}
	})
}

func TestUserInfoContentClaimsRideProfileScope(t *testing.T) {
	env := newPrefEnv(t)

	if err := env.db.Model(&model.User{}).Where("id = ?", env.user.ID).
		Updates(map[string]any{"adult_confirmed_at": gorm.Expr("now()"), "nsfw_display": model.NSFWDisplayShow}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("profile scope carries both claims", func(t *testing.T) {
		info, err := env.oauth.GetUserInfo(t.Context(), env.user.UUID, "openid profile", 0)
		if err != nil {
			t.Fatalf("userinfo: %v", err)
		}
		if info.AdultConfirmed == nil || !*info.AdultConfirmed {
			t.Fatalf("adult_confirmed = %v, want true", info.AdultConfirmed)
		}
		if info.NSFWDisplay != model.NSFWDisplayShow {
			t.Fatalf("nsfw_display = %q, want %q", info.NSFWDisplay, model.NSFWDisplayShow)
		}
	})

	t.Run("without profile scope the keys are absent, not false", func(t *testing.T) {
		info, err := env.oauth.GetUserInfo(t.Context(), env.user.UUID, "openid email", 0)
		if err != nil {
			t.Fatalf("userinfo: %v", err)
		}
		raw, err := json.Marshal(info)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(raw, &keys); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for _, k := range []string{"adult_confirmed", "nsfw_display"} {
			if _, ok := keys[k]; ok {
				t.Fatalf("%q leaked into a userinfo response without the profile scope: %s", k, raw)
			}
		}
	})

	// No account reaches this state any more — the attestation was retired on
	// 2026-09-23 and the column is stamped on create — but the claim's shape
	// is what downstream sites branch on, so a null must still serialize as a
	// present false rather than vanish.
	t.Run("a null attestation reports adult_confirmed false, not a missing key", func(t *testing.T) {
		if err := env.db.Model(&model.User{}).Where("id = ?", env.user.ID).
			Update("adult_confirmed_at", nil).Error; err != nil {
			t.Fatalf("reset: %v", err)
		}
		info, err := env.oauth.GetUserInfo(t.Context(), env.user.UUID, "openid profile", 0)
		if err != nil {
			t.Fatalf("userinfo: %v", err)
		}
		if info.AdultConfirmed == nil || *info.AdultConfirmed {
			t.Fatalf("adult_confirmed = %v, want a present false", info.AdultConfirmed)
		}
		if got := model.EffectiveNSFWDisplay(nil, info.NSFWDisplay); got != model.NSFWDisplayHide {
			t.Fatalf("the documented effective rule gives %q for an unconfirmed account", got)
		}
	})
}
