package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"api/internal/middleware"
	authModel "api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/ledger/ledgertest"
	"api/internal/platform/ledger/service"
	siteModel "api/internal/platform/site/model"
	"api/internal/testsupport/dbtest"
	"api/pkg/errors"

	"github.com/gofiber/fiber/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type env struct {
	app  *fiber.App
	db   *gorm.DB
	user uint
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "open database: %v", err)
	}
	if err := ledgertest.Migrate(db); err != nil {
		dbtest.Skipf(t, "migrate: %v", err)
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	u := &authModel.User{Name: "lh" + tag[len(tag)-10:], Email: "lh-" + tag + "@test.local"}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_ = ledgertest.Purge(db, u.ID)
		db.Exec(`DELETE FROM users WHERE id = ?`, u.ID)
	})

	l := service.New(db)
	h := New(l, repository.NewUserRepository(db))
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		if id := c.Get("X-Test-Client"); id != "" {
			c.Locals(middleware.LocalOAuthClient, &siteModel.OAuthClient{
				ID: id, MoemoepointAwarder: c.Get("X-Test-Awarder") == "1",
			})
		}
		return c.Next()
	})
	app.Post("/users/:id/moemoepoint", h.Adjust)
	app.Post("/users/:id/moemoepoint/charges", h.Charge)
	app.Post("/users/:id/moemoepoint/reversals", h.Reverse)
	app.Get("/users/:id/moemoepoint", h.GetBalance)
	app.Get("/users/:id/moemoepoint/log", h.GetLog)
	ledgertest.Fund(t, l, u.ID, 10)
	return &env{app: app, db: db, user: u.ID}
}

func (e *env) k(s string) string { return fmt.Sprintf("%d:%s", e.user, s) }

type reply struct {
	status int
	Code   int            `json:"code"`
	Data   map[string]any `json:"data"`
}

func (e *env) do(t *testing.T, method, path string, awarder bool, body any) reply {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, fmt.Sprintf(path, e.user), &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Client", "forum-handler-test")
	if awarder {
		req.Header.Set("X-Test-Awarder", "1")
	}
	resp, err := e.app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	var r reply
	_ = json.NewDecoder(resp.Body).Decode(&r)
	r.status = resp.StatusCode
	return r
}

func (r reply) want(t *testing.T, status, code int) reply {
	t.Helper()
	if r.status != status || r.Code != code {
		t.Fatalf("got HTTP %d code %d, want HTTP %d code %d", r.status, r.Code, status, code)
	}
	return r
}

func (r reply) balance() float64 {
	b, _ := r.Data["balance"].(float64)
	return b
}

func TestWritesNeedTheAwarderFlagAndReadsDoNot(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/users/%d/moemoepoint", "/users/%d/moemoepoint/charges", "/users/%d/moemoepoint/reversals"} {
		e.do(t, "POST", path, false, map[string]any{"delta": 1, "amount": 1, "reason": "liked",
			"idempotency_key": e.k("k")}).want(t, 403, errors.ErrMoemoepointNotAwarder)
	}
	if b := e.do(t, "GET", "/users/%d/moemoepoint", false, nil).want(t, 200, 0).balance(); b != 10 {
		t.Fatalf("balance %v, want 10", b)
	}
}

// The s2s award takes only the four award reasons. name_change used to slip
// through, because the old handler listed what to refuse instead of what to
// accept; a spend dressed as an award is refused for the same reason.
func TestAdjustAcceptsOnlyAwardReasons(t *testing.T) {
	e := newEnv(t)
	for _, reason := range []string{"name_change", "admin_grant", "register_gift", "spend", "reversal", "opening_balance"} {
		e.do(t, "POST", "/users/%d/moemoepoint", true, map[string]any{"delta": -1, "reason": reason,
			"idempotency_key": e.k("adj:" + reason)}).want(t, 400, errors.ErrMoemoepointInvalidReason)
	}
	r := e.do(t, "POST", "/users/%d/moemoepoint", true, map[string]any{"delta": 3, "reason": "liked",
		"idempotency_key": e.k("adj:liked")}).want(t, 200, 0)
	if r.balance() != 13 || r.Data["applied"] != true {
		t.Fatalf("award: %+v", r.Data)
	}
}

func TestChargeIsCheckedByTheServer(t *testing.T) {
	e := newEnv(t)
	e.do(t, "POST", "/users/%d/moemoepoint/charges", true, map[string]any{"amount": 11,
		"idempotency_key": e.k("c:too-much")}).want(t, 400, errors.ErrMoemoepointInsufficient)
	e.do(t, "POST", "/users/%d/moemoepoint/charges", true, map[string]any{"amount": 0,
		"idempotency_key": e.k("c:zero")}).want(t, 400, errors.ErrMoemoepointInvalidDelta)

	body := map[string]any{"amount": 4, "ref": "topic_upvote:1", "idempotency_key": e.k("c:ok")}
	if r := e.do(t, "POST", "/users/%d/moemoepoint/charges", true, body).want(t, 200, 0); r.balance() != 6 || r.Data["applied"] != true {
		t.Fatalf("charge: %+v", r.Data)
	}
	if r := e.do(t, "POST", "/users/%d/moemoepoint/charges", true, body).want(t, 200, 0); r.balance() != 6 || r.Data["applied"] != false {
		t.Fatalf("replayed charge: %+v", r.Data)
	}

	e.do(t, "POST", "/users/%d/moemoepoint/reversals", true, map[string]any{"idempotency_key": e.k("c:unknown")}).
		want(t, 404, errors.ErrMoemoepointTransferNotFound)
	if r := e.do(t, "POST", "/users/%d/moemoepoint/reversals", true, map[string]any{"idempotency_key": e.k("c:ok")}).
		want(t, 200, 0); r.balance() != 10 {
		t.Fatalf("reversal: %+v", r.Data)
	}
}

func TestS2SLogHidesNoteAndActor(t *testing.T) {
	e := newEnv(t)
	e.do(t, "POST", "/users/%d/moemoepoint/charges", true, map[string]any{"amount": 2,
		"note": "moderator-only text", "idempotency_key": e.k("log:c")}).want(t, 200, 0)
	var r struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	resp, err := e.app.Test(httptest.NewRequest("GET", fmt.Sprintf("/users/%d/moemoepoint/log", e.user), nil))
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	if len(r.Data.Items) != 2 {
		t.Fatalf("log items: %+v", r.Data.Items)
	}
	top := r.Data.Items[0]
	if _, leaked := top["note"]; leaked {
		t.Fatal("the s2s log carries note")
	}
	if _, leaked := top["actor_user_id"]; leaked {
		t.Fatal("the s2s log carries actor_user_id")
	}
	if top["reason"] != "spend" || top["balance_after"] != float64(8) {
		t.Fatalf("top row: %+v", top)
	}
}
