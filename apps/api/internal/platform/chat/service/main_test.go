package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/migrate"
	"api/internal/platform/chat/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

// Distinct from every other suite's advisory key: the key space is shared by
// the whole Postgres instance.
const chatSuiteLockKey = 0x63686174

func acquireSuiteLock(db *sql.DB) func() {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return func() {}
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", chatSuiteLockKey); err != nil {
		_ = conn.Close()
		return func() {}
	}
	return func() {
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", chatSuiteLockKey)
		_ = conn.Close()
	}
}

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("chat/service")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("chat/service", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := acquireSuiteLock(sqlDB)
	if err := migrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("chat/service", "chat migration failed: %v", err)
	}
	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func cleanTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"chat_import_message", "chat_report", "chat_update", "chat_hidden_message", "chat_reaction", "chat_message",
		"chat_member", "chat_conversation", "chat_user", "account_purge_cursor",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

type fakeUsers struct {
	mu sync.Mutex
	m  map[int64]Profile
}

func (f *fakeUsers) Profiles(_ context.Context, ids []int64) (map[int64]Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[int64]Profile{}
	for _, id := range ids {
		if p, ok := f.m[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

type fakeRel struct {
	mu      sync.Mutex
	blocks  map[[2]int64]bool
	follows map[[2]int64]bool
	levels  map[int64]int16
}

func (f *fakeRel) BlockedEitherWay(_ context.Context, a, b int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.blocks[[2]int64{a, b}] || f.blocks[[2]int64{b, a}], nil
}

func (f *fakeRel) Follows(_ context.Context, follower, followee int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.follows[[2]int64{follower, followee}], nil
}

func (f *fakeRel) TrustLevel(_ context.Context, uid int64) (int16, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.levels[uid], nil
}

type fakePub struct {
	mu  sync.Mutex
	got []Delivery
}

func (f *fakePub) Publish(_ context.Context, ds []Delivery) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, ds...)
}

func (f *fakePub) take() []Delivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.got
	f.got = nil
	return out
}

type memCounter struct {
	mu sync.Mutex
	m  map[string]int64
}

func (c *memCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key]++
	return c.m[key], nil
}

type rig struct {
	svc   *Service
	users *fakeUsers
	rel   *fakeRel
	pub   *fakePub
	clock *time.Time
}

var old = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// newRig gives every listed user an old account; the default recipient
// setting (following) then admits a sender the recipient follows.
func newRig(t *testing.T, uids ...int64) *rig {
	t.Helper()
	cleanTables(t)
	r := &rig{
		users: &fakeUsers{m: map[int64]Profile{}},
		rel:   &fakeRel{blocks: map[[2]int64]bool{}, follows: map[[2]int64]bool{}, levels: map[int64]int16{}},
		pub:   &fakePub{},
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	r.clock = &now
	for _, id := range uids {
		r.users.m[id] = Profile{ID: id, Name: "u" + dto.ID(id), CreatedAt: old}
	}
	r.svc = New(testDB, Options{
		Users: r.users, Relationships: r.rel, Publisher: r.pub,
		Counter: &memCounter{m: map[string]int64{}},
		Now:     func() time.Time { return *r.clock },
	})
	return r
}

func (r *rig) follow(follower, followee int64) { r.rel.follows[[2]int64{follower, followee}] = true }
func (r *rig) block(a, b int64)                { r.rel.blocks[[2]int64{a, b}] = true }
func (r *rig) advance(d time.Duration)         { *r.clock = r.clock.Add(d) }

func actor(uid int64) Actor {
	return Actor{UserID: uid, Site: "letmoe", Hosts: []string{"letmoe.example"}}
}

// direct opens an accepted direct conversation: the peer follows the opener.
func (r *rig) direct(t *testing.T, a, b int64) int64 {
	t.Helper()
	r.follow(b, a)
	res, err := r.svc.EnsureDirect(context.Background(), actor(a), b)
	if err != nil {
		t.Fatalf("open direct %d->%d: %v", a, b, err)
	}
	id, _ := dto.ParseID(res.Conversation.ID)
	return id
}

func (r *rig) send(t *testing.T, from, conv int64, text string) dto.Message {
	t.Helper()
	res, err := r.svc.Send(context.Background(), actor(from), conv, SendInput{Text: text})
	if err != nil {
		t.Fatalf("send %q: %v", text, err)
	}
	return res.Message
}

func member(t *testing.T, conv, uid int64) model.ChatMember {
	t.Helper()
	var m model.ChatMember
	if err := testDB.Where("conversation_id = ? AND user_id = ?", conv, uid).Take(&m).Error; err != nil {
		t.Fatalf("member %d/%d: %v", conv, uid, err)
	}
	return m
}

func updatesOf(t *testing.T, uid int64) []model.ChatUpdate {
	t.Helper()
	var ups []model.ChatUpdate
	if err := testDB.Where("user_id = ?", uid).Order("update_seq").Find(&ups).Error; err != nil {
		t.Fatalf("updates of %d: %v", uid, err)
	}
	return ups
}

func kinds(ups []model.ChatUpdate) []string {
	out := make([]string, len(ups))
	for i, u := range ups {
		out[i] = u.Kind
	}
	return out
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("want %v, got %v", target, err)
	}
}

func wantInvalid(t *testing.T, err error) {
	t.Helper()
	var ie *InvalidError
	if !errors.As(err, &ie) {
		t.Fatalf("want InvalidError, got %v", err)
	}
}

func decodeData(t *testing.T, u model.ChatUpdate) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(u.Data, &m); err != nil {
		t.Fatalf("decode update data: %v", err)
	}
	return m
}

func ptr[T any](v T) *T { return &v }

func bold(offset, length int) content.Entity {
	return content.Entity{Type: content.TypeBold, Offset: offset, Length: length}
}
