package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"api/internal/platform/chat/migrate"
	"api/internal/platform/chat/realtime"
	"api/internal/platform/chat/service"
	"api/internal/testsupport/dbtest"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

const suiteLockKey = 0x63686174

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("chat/handler")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("chat/handler", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	conn, err := sqlDB.Conn(context.Background())
	if err == nil {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_lock($1)", suiteLockKey)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("chat/handler", "chat migration failed: %v", err)
	}
	testDB = db
	code := m.Run()
	if conn != nil {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", suiteLockKey)
		_ = conn.Close()
	}
	os.Exit(code)
}

func clean(t *testing.T) {
	t.Helper()
	if err := testDB.Exec(`TRUNCATE chat_report, chat_update, chat_hidden_message, chat_reaction, chat_message,
		chat_member, chat_conversation, chat_user RESTART IDENTITY CASCADE`).Error; err != nil {
		t.Fatal(err)
	}
}

type users map[int64]service.Profile

func (u users) Profiles(_ context.Context, ids []int64) (map[int64]service.Profile, error) {
	out := map[int64]service.Profile{}
	for _, id := range ids {
		if p, ok := u[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

type everyoneFollows struct{}

func (everyoneFollows) BlockedEitherWay(context.Context, int64, int64) (bool, error) {
	return false, nil
}
func (everyoneFollows) Follows(context.Context, int64, int64) (bool, error) { return true, nil }
func (everyoneFollows) TrustLevel(context.Context, int64) (int16, error)    { return 1, nil }

// Tokens are "<scope>:<uid>", e.g. "write:1".
func identify(_ context.Context, raw string) (Identity, error) {
	scope, uid, ok := strings.Cut(raw, ":")
	if !ok || uid == "" {
		return Identity{}, errors.New("bad token")
	}
	var id int64
	for _, c := range uid {
		id = id*10 + int64(c-'0')
	}
	var scopes []string
	switch scope {
	case "write":
		scopes = []string{"openid", ScopeWrite}
	case "read":
		scopes = []string{"openid", ScopeRead}
	case "none":
		scopes = []string{"openid"}
	}
	return Identity{UID: id, ClientID: "letmoe-web", Scopes: scopes}, nil
}

func newApp(t *testing.T) *fiber.App {
	t.Helper()
	clean(t)
	svc := service.New(testDB, service.Options{
		Users:         users{1: {ID: 1, Name: "one", CreatedAt: time.Unix(0, 0)}, 2: {ID: 2, Name: "two", CreatedAt: time.Unix(0, 0)}},
		Relationships: everyoneFollows{},
	})
	app := fiber.New()
	Setup(app, Options{
		Chat: svc, Identify: identify,
		Tokens: realtime.NewTokenIssuer("secret", time.Minute), RealtimeURL: "wss://realtime.example/connection/websocket",
		Client: func(context.Context, string) (ClientInfo, error) {
			return ClientInfo{Site: "letmoe", Hosts: []string{"letmoe.example"}}, nil
		},
	})
	return app
}

func call(t *testing.T, app *fiber.App, method, path, token, body string) (int, map[string]any, http.Header) {
	t.Helper()
	var r io.Reader = http.NoBody
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, resp.Header
}

func TestAuthentication(t *testing.T) {
	app := newApp(t)
	for name, c := range map[string]struct {
		token  string
		method string
		want   int
		code   string
	}{
		"no token":          {"", http.MethodGet, 401, "MISSING_CREDENTIAL"},
		"application key":   {"nmk_live_x", http.MethodGet, 401, "INVALID_CREDENTIAL"},
		"garbage":           {"garbage", http.MethodGet, 401, "INVALID_CREDENTIAL"},
		"no chat scope":     {"none:1", http.MethodGet, 403, "SCOPE_REQUIRED"},
		"read cannot write": {"read:1", http.MethodPatch, 403, "SCOPE_REQUIRED"},
		"read can read":     {"read:1", http.MethodGet, 200, ""},
		"write can read":    {"write:1", http.MethodGet, 200, ""},
		"write can write":   {"write:1", http.MethodPatch, 200, ""},
	} {
		body := ""
		if c.method == http.MethodPatch {
			body = `{"accept_requests": false}`
		}
		status, out, _ := call(t, app, c.method, "/v2/chat/settings", c.token, body)
		if status != c.want {
			t.Errorf("%s: status %d, want %d (%v)", name, status, c.want, out)
			continue
		}
		if c.code != "" && out["code"] != c.code {
			t.Errorf("%s: code %v, want %s", name, out["code"], c.code)
		}
		if c.code != "" && !strings.HasPrefix(out["request_id"].(string), "req_") {
			t.Errorf("%s: problem lacks a request id: %v", name, out)
		}
	}
	status, _, _ := call(t, app, http.MethodGet, SpecPath, "", "")
	if status == http.StatusUnauthorized {
		t.Fatal("the spec needs no token")
	}
}

func TestConversationFlowThroughRoutes(t *testing.T) {
	app := newApp(t)
	status, conv, _ := call(t, app, http.MethodPut, "/v2/chat/direct/2", "write:1", "")
	if status != 200 || conv["object"] != "conversation" || conv["peer_id"] != "2" {
		t.Fatalf("open direct: %d %v", status, conv)
	}
	id := conv["id"].(string)

	status, msg, _ := call(t, app, http.MethodPost, "/v2/chat/conversations/"+id+"/messages", "write:1",
		`{"client_message_id":"0b4a5c52-7c4e-4a3e-9c1c-2c3f4b5a6d7e","text":"**hi** @two","entities":[{"type":"mention","offset":7,"length":4,"user_id":"2"}],"context":{"kind":"topic","id":"7","title":"A topic","url":"https://letmoe.example/topic/7"}}`)
	if status != http.StatusCreated {
		t.Fatalf("send: %d %v", status, msg)
	}
	if msg["object"] != "message" || msg["seq"] != float64(1) || msg["sender_id"] != "1" || msg["client_message_id"] == nil {
		t.Fatalf("message: %v", msg)
	}
	ents := msg["entities"].([]any)
	if len(ents) != 1 || ents[0].(map[string]any)["user_id"] != "2" {
		t.Fatalf("mention user_id must come back as a string: %v", ents)
	}
	if ctxCard := msg["context"].(map[string]any); ctxCard["site"] != "letmoe" {
		t.Fatalf("context site comes from the token's client: %v", ctxCard)
	}
	if us := msg["users"].([]any); len(us) == 0 {
		t.Fatal("users come with the message")
	}

	status, page, _ := call(t, app, http.MethodGet, "/v2/chat/conversations/"+id+"/messages", "read:2", "")
	if status != 200 || page["object"] != "list" {
		t.Fatalf("list: %d %v", status, page)
	}
	items := page["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["client_message_id"] != nil {
		t.Fatalf("the recipient never sees the sender's client_message_id: %v", items)
	}

	status, list, _ := call(t, app, http.MethodGet, "/v2/chat/conversations", "read:2", "")
	if status != 200 || len(list["items"].([]any)) != 1 {
		t.Fatalf("conversations: %d %v", status, list)
	}
	status, st, _ := call(t, app, http.MethodGet, "/v2/chat/state", "read:2", "")
	if status != 200 || st["unread_message_count"] != float64(1) || st["last_update_seq"] != float64(1) {
		t.Fatalf("state: %d %v", status, st)
	}
	status, ups, _ := call(t, app, http.MethodGet, "/v2/chat/updates?after=0", "read:2", "")
	if status != 200 || len(ups["updates"].([]any)) != 1 || len(ups["messages"].([]any)) != 1 {
		t.Fatalf("updates: %d %v", status, ups)
	}
	status, read, _ := call(t, app, http.MethodPost, "/v2/chat/conversations/"+id+"/read", "write:2", `{"max_seq":1}`)
	if status != 200 || read["unread_count"] != float64(0) {
		t.Fatalf("read: %d %v", status, read)
	}
	msgID := msg["id"].(string)
	status, rs, _ := call(t, app, http.MethodPut, "/v2/chat/messages/"+msgID+"/reaction", "write:2", `{"reaction":"heart"}`)
	if status != 200 || len(rs["reactions"].([]any)) != 1 {
		t.Fatalf("react: %d %v", status, rs)
	}
	status, _, _ = call(t, app, http.MethodPut, "/v2/chat/conversations/"+id+"/pins/1", "write:2", "")
	if status != http.StatusNoContent {
		t.Fatalf("pin: %d", status)
	}
	status, tok, _ := call(t, app, http.MethodPost, "/v2/chat/realtime-token", "write:1", "")
	if status != 200 || tok["url"] == "" || tok["token"] == "" {
		t.Fatalf("realtime token: %d %v", status, tok)
	}
}

func TestProblemsMapCleanly(t *testing.T) {
	app := newApp(t)
	_, conv, _ := call(t, app, http.MethodPut, "/v2/chat/direct/2", "write:1", "")
	id := conv["id"].(string)

	status, out, _ := call(t, app, http.MethodGet, "/v2/chat/conversations/999", "read:1", "")
	if status != 404 || out["code"] != "NOT_FOUND" {
		t.Fatalf("unknown conversation: %d %v", status, out)
	}
	status, out, _ = call(t, app, http.MethodGet, "/v2/chat/conversations/"+id, "read:3", "")
	if status != 404 {
		t.Fatalf("someone else's conversation reads as not found: %d %v", status, out)
	}
	status, out, _ = call(t, app, http.MethodPost, "/v2/chat/conversations/"+id+"/messages", "write:1", `{"text":"   "}`)
	if status != 422 || out["code"] != "VALIDATION_FAILED" || len(out["errors"].([]any)) != 1 {
		t.Fatalf("empty message: %d %v", status, out)
	}
	status, out, _ = call(t, app, http.MethodPut, "/v2/chat/direct/1", "write:1", "")
	if status != 422 {
		t.Fatalf("messaging yourself: %d %v", status, out)
	}
	status, out, _ = call(t, app, http.MethodPost, "/v2/chat/conversations/"+id+"/messages", "write:1", `{"text":"x","reply_to_seq":0}`)
	if status != 422 {
		t.Fatalf("schema validation: %d %v", status, out)
	}
	if out["code"] != "VALIDATION_FAILED" {
		t.Fatalf("huma validation errors are problems too: %v", out)
	}
}

func TestProblemOfRateLimitCarriesRetryAfter(t *testing.T) {
	err := fail(context.Background(), "x", &service.RateLimitError{RetryAfter: 1500 * time.Millisecond})
	hp, ok := err.(interface{ GetHeaders() http.Header })
	if !ok || hp.GetHeaders().Get("Retry-After") != "2" {
		t.Fatalf("Retry-After: %#v", err)
	}
}

func TestRealtimeTokenClaims(t *testing.T) {
	iss := realtime.NewTokenIssuer("secret", time.Minute)
	now := time.Unix(1_800_000_000, 0)
	raw, exp, err := iss.Issue(42, now)
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(now.Add(time.Minute)) {
		t.Fatalf("exp %v", exp)
	}
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return []byte("secret"), nil },
		jwt.WithTimeFunc(func() time.Time { return now })); err != nil {
		t.Fatal(err)
	}
	chans, _ := claims["channels"].([]any)
	if claims["sub"] != "42" || len(chans) != 1 || chans[0] != "user:42" {
		t.Fatalf("claims: %v", claims)
	}
	if realtime.NewTokenIssuer("", time.Minute) != nil {
		t.Fatal("no secret, no issuer")
	}
}

func TestDeletedAccountsCannotWriteAndLookupsFailClosed(t *testing.T) {
	clean(t)
	svc := service.New(testDB, service.Options{Users: users{1: {ID: 1, CreatedAt: time.Unix(0, 0)}}})
	app := fiber.New()
	Setup(app, Options{Chat: svc, Identify: identify,
		AccountActive: func(_ context.Context, uid int64) (bool, error) { return uid != 9, nil },
		Client: func(_ context.Context, id string) (ClientInfo, error) {
			return ClientInfo{}, errors.New("database down")
		}})
	status, out, _ := call(t, app, http.MethodPatch, "/v2/chat/settings", "write:9", `{"accept_requests":false}`)
	if status != http.StatusUnauthorized || out["code"] != "INVALID_CREDENTIAL" {
		t.Fatalf("a deleted account's token must not write: %d %v", status, out)
	}
	status, out, _ = call(t, app, http.MethodGet, "/v2/chat/settings", "read:1", "")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("an unreadable client must fail closed: %d %v", status, out)
	}
}
