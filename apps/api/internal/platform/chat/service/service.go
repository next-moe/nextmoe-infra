package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"api/internal/platform/chat/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Actor struct {
	UserID int64
	Site   string
	Hosts  []string
}

type Profile struct {
	ID        int64
	Name      string
	Avatar    string
	Deleted   bool
	CreatedAt time.Time
}

type Users interface {
	Profiles(ctx context.Context, ids []int64) (map[int64]Profile, error)
}

type Relationships interface {
	BlockedEitherWay(ctx context.Context, a, b int64) (bool, error)
	Follows(ctx context.Context, follower, followee int64) (bool, error)
	TrustLevel(ctx context.Context, uid int64) (int16, error)
}

type ImageMeta struct {
	Width     int
	Height    int
	Thumbhash string
}

type Images interface {
	Meta(ctx context.Context, hashes []string) (map[string]ImageMeta, error)
}

type Delivery struct {
	UserID int64
	Data   any
}

type Publisher interface {
	Publish(ctx context.Context, deliveries []Delivery)
}

type Counter interface {
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

type Service struct {
	db      *gorm.DB
	users   Users
	rel     Relationships
	images  Images
	pub     Publisher
	counter Counter
	now     func() time.Time
}

type Options struct {
	Users         Users
	Relationships Relationships
	Images        Images
	Publisher     Publisher
	Counter       Counter
	Now           func() time.Time
}

func New(db *gorm.DB, o Options) *Service {
	s := &Service{db: db, users: o.Users, rel: o.Relationships, images: o.Images, pub: o.Publisher, counter: o.Counter, now: o.Now}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

var (
	ErrNotFound       = errors.New("chat: not found")
	ErrBlocked        = errors.New("chat: one of the two users has blocked the other")
	ErrRequestLimit   = errors.New("chat: the recipient has not accepted yet")
	ErrEditWindow     = errors.New("chat: the edit window has closed")
	ErrNotPermitted   = errors.New("chat: not permitted")
	ErrImagesDisabled = errors.New("chat: image messages are not available")
)

type NotAcceptingError struct{ Reason string }

func (e *NotAcceptingError) Error() string { return "chat: not accepting: " + e.Reason }

type InvalidError struct{ Field, Reason string }

func (e *InvalidError) Error() string { return "chat: invalid " + e.Field + ": " + e.Reason }

type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string { return "chat: rate limited" }

const (
	editWindow          = 48 * time.Hour
	pendingMessageLimit = 3
	maxPinnedDialogs    = 5
	updateRetention     = 30 * 24 * time.Hour
	maxPageLimit        = 100
	defaultPageLimit    = 50
)

func clampLimit(n int) int {
	if n <= 0 {
		return defaultPageLimit
	}
	if n > maxPageLimit {
		return maxPageLimit
	}
	return n
}

func (s *Service) allow(ctx context.Context, key string, limit int64, window time.Duration) error {
	if s.counter == nil {
		return nil
	}
	now := s.now()
	bucket := now.Unix() / int64(window/time.Second)
	n, err := s.counter.Incr(ctx, fmt.Sprintf("chat:rl:%s:%d", key, bucket), window+time.Second)
	if err != nil {
		slog.Warn("chat rate limit unavailable; allowing", "key", key, "err", err)
		return nil
	}
	if n > limit {
		next := time.Unix((bucket+1)*int64(window/time.Second), 0)
		return &RateLimitError{RetryAfter: next.Sub(now)}
	}
	return nil
}

func jsonOf(v any) datatypes.JSON {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return datatypes.JSON(b)
}

// Every write path takes its locks in one order: the conversation row, then
// its member rows, then chat_user rows in ascending user id.
func lockConversation(tx *gorm.DB, id int64) (*model.ChatConversation, error) {
	var c model.ChatConversation
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND deleted_at IS NULL", id).Take(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &c, err
}

func lockMembers(tx *gorm.DB, conversationID int64) ([]model.ChatMember, error) {
	var ms []model.ChatMember
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("conversation_id = ? AND left_at IS NULL", conversationID).
		Order("user_id").Find(&ms).Error
	return ms, err
}

func findMember(ms []model.ChatMember, uid int64) *model.ChatMember {
	for i := range ms {
		if ms[i].UserID == uid {
			return &ms[i]
		}
	}
	return nil
}

// A conversation the actor is not in is reported as not found, so ids
// cannot be probed.
func lockAsMember(tx *gorm.DB, conversationID, uid int64) (*model.ChatConversation, []model.ChatMember, *model.ChatMember, error) {
	c, err := lockConversation(tx, conversationID)
	if err != nil {
		return nil, nil, nil, err
	}
	ms, err := lockMembers(tx, conversationID)
	if err != nil {
		return nil, nil, nil, err
	}
	me := findMember(ms, uid)
	if me == nil {
		return nil, nil, nil, ErrNotFound
	}
	return c, ms, me, nil
}

var onConflictNothing = clause.OnConflict{DoNothing: true}

func ensureChatUsers(tx *gorm.DB, ids []int64, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	rows := make([]model.ChatUser, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, defaultChatUser(id, now))
	}
	return tx.Clauses(onConflictNothing).Create(&rows).Error
}

func defaultChatUser(uid int64, now time.Time) model.ChatUser {
	return model.ChatUser{
		UserID: uid, AllowIncoming: model.AllowFollowing, AcceptRequests: true,
		AllowGroupInvites: model.AllowFollowing, CreatedAt: now, UpdatedAt: now,
	}
}

type pendingUpdate struct {
	userID         int64
	kind           string
	conversationID int64
	data           any
}

func appendUpdates(tx *gorm.DB, now time.Time, ups []pendingUpdate) ([]model.ChatUpdate, error) {
	if len(ups) == 0 {
		return nil, nil
	}
	byUser := map[int64][]pendingUpdate{}
	for _, u := range ups {
		byUser[u.userID] = append(byUser[u.userID], u)
	}
	ids := make([]int64, 0, len(byUser))
	for id := range byUser {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if err := ensureChatUsers(tx, ids, now); err != nil {
		return nil, err
	}
	var users []model.ChatUser
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id IN ?", ids).Order("user_id").Find(&users).Error; err != nil {
		return nil, err
	}
	out := make([]model.ChatUpdate, 0, len(ups))
	for _, u := range users {
		seq := u.LastUpdateSeq
		for _, p := range byUser[u.UserID] {
			seq++
			out = append(out, model.ChatUpdate{
				UserID: u.UserID, UpdateSeq: seq, Kind: p.kind, ConversationID: p.conversationID,
				Data: jsonOf(p.data), CreatedAt: now,
			})
		}
		if err := tx.Model(&model.ChatUser{}).Where("user_id = ?", u.UserID).
			Updates(map[string]any{"last_update_seq": seq, "updated_at": now}).Error; err != nil {
			return nil, err
		}
	}
	if err := tx.Create(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) withRetry(ctx context.Context, fn func(tx *gorm.DB) error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = s.db.WithContext(ctx).Transaction(fn)
		if err == nil || !isDeadlock(err) {
			return err
		}
	}
	return err
}

func isDeadlock(err error) bool {
	type sqlState interface{ SQLState() string }
	var st sqlState
	return errors.As(err, &st) && (st.SQLState() == "40P01" || st.SQLState() == "40001")
}
