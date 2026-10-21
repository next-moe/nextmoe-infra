package discussion

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MaxBodyRunes = 2000
	MaxMentions  = 10
)

var (
	ErrUnknownKind     = errors.New("discussion: unknown anchor kind")
	ErrAnchorNotFound  = errors.New("discussion: anchor not found")
	ErrForbidden       = errors.New("discussion: viewer may not see this anchor")
	ErrPostNotFound    = errors.New("discussion: post not found")
	ErrNotAuthor       = errors.New("discussion: only the author may change this post")
	ErrPostClosed      = errors.New("discussion: the post is hidden or deleted")
	ErrEmptyBody       = errors.New("discussion: body is empty")
	ErrBodyTooLong     = errors.New("discussion: body is too long")
	ErrTooManyMentions = errors.New("discussion: too many mentions")
	ErrBadReplyTo      = errors.New("discussion: reply_to is not a post in this thread")
)

type Service struct {
	db    *gorm.DB
	kinds map[string]Kind
	names []string
}

func New(db *gorm.DB, kinds ...Kind) *Service {
	s := &Service{db: db, kinds: map[string]Kind{}}
	for _, k := range kinds {
		s.kinds[k.Name] = k
		s.names = append(s.names, k.Name)
	}
	sort.Strings(s.names)
	return s
}

func (s *Service) KindNames() []string { return slices.Clone(s.names) }

func (s *Service) kind(name string) (Kind, error) {
	k, ok := s.kinds[name]
	if !ok {
		return Kind{}, ErrUnknownKind
	}
	return k, nil
}

func (s *Service) CheckVisible(ctx context.Context, kindName string, anchorID int64, v Viewer) error {
	k, err := s.kind(kindName)
	if err != nil {
		return err
	}
	ok, err := k.Exists(ctx, s.db, anchorID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAnchorNotFound
	}
	if k.Public {
		return nil
	}
	if k.CanSee == nil {
		return ErrForbidden
	}
	ok, err = k.CanSee(ctx, s.db, v, anchorID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func (s *Service) IsReviewer(kindName string, roles []string) bool {
	k, err := s.kind(kindName)
	if err != nil || k.Reviewer == nil {
		return false
	}
	return k.Reviewer(roles)
}

func (s *Service) Owner(ctx context.Context, kindName string, anchorID int64) (int64, error) {
	k, err := s.kind(kindName)
	if err != nil {
		return 0, err
	}
	if k.Owner == nil {
		return 0, nil
	}
	return k.Owner(ctx, s.db, anchorID)
}

func (s *Service) List(ctx context.Context, kindName string, anchorID, afterID int64, limit int) ([]Post, error) {
	var rows []Post
	q := s.db.WithContext(ctx).Where("anchor_kind = ? AND anchor_id = ?", kindName, anchorID)
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	if err := q.Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Service) Count(ctx context.Context, kindName string, anchorID int64) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Post{}).
		Where("anchor_kind = ? AND anchor_id = ? AND status <> ?", kindName, anchorID, StatusDeleted).
		Count(&n).Error
	return n, err
}

func (s *Service) Get(ctx context.Context, id int64) (*Post, error) {
	var p Post
	if err := s.db.WithContext(ctx).First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPostNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (s *Service) CountByAuthorSince(ctx context.Context, uid int64, since time.Time) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Post{}).
		Where("author_uid = ? AND created_at >= ?", uid, since).Count(&n).Error
	return n, err
}

func NormalizeBody(raw string) (string, error) {
	body := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if body == "" {
		return "", ErrEmptyBody
	}
	if utf8.RuneCountInString(body) > MaxBodyRunes {
		return "", ErrBodyTooLong
	}
	return body, nil
}

func normalizeMentions(uids []int64, author int64) ([]int64, error) {
	out := make([]int64, 0, len(uids))
	for _, uid := range uids {
		if uid > 0 && uid != author && !slices.Contains(out, uid) {
			out = append(out, uid)
		}
	}
	if len(out) > MaxMentions {
		return nil, ErrTooManyMentions
	}
	return out, nil
}

type CreateInput struct {
	Kind        string
	AnchorID    int64
	AuthorUID   int64
	Roles       []string
	Body        string
	ReplyToID   *int64
	MentionUIDs []int64
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*Post, error) {
	k, err := s.kind(in.Kind)
	if err != nil {
		return nil, err
	}
	body, err := NormalizeBody(in.Body)
	if err != nil {
		return nil, err
	}
	mentions, err := normalizeMentions(in.MentionUIDs, in.AuthorUID)
	if err != nil {
		return nil, err
	}
	mentionJSON, _ := json.Marshal(mentions)
	post := Post{
		AnchorKind: in.Kind, AnchorID: in.AnchorID, AuthorUID: in.AuthorUID, Body: body,
		ReplyToID: in.ReplyToID, MentionUIDs: datatypes.JSON(mentionJSON),
		AuthorReviewer: k.Reviewer != nil && k.Reviewer(in.Roles), Status: StatusVisible,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if in.ReplyToID != nil {
			var n int64
			if err := tx.Model(&Post{}).Where("id = ? AND anchor_kind = ? AND anchor_id = ?",
				*in.ReplyToID, in.Kind, in.AnchorID).Count(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				return ErrBadReplyTo
			}
		}
		if err := tx.Create(&post).Error; err != nil {
			return err
		}
		watchers := append([]int64{in.AuthorUID}, mentions...)
		if k.Participants != nil {
			more, err := k.Participants(ctx, tx, in.AnchorID)
			if err != nil {
				return err
			}
			watchers = append(watchers, more...)
		}
		return watch(tx, in.Kind, in.AnchorID, watchers)
	})
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func watch(tx *gorm.DB, kind string, anchorID int64, uids []int64) error {
	rows := make([]Watcher, 0, len(uids))
	seen := map[int64]bool{}
	for _, uid := range uids {
		if uid <= 0 || seen[uid] {
			continue
		}
		seen[uid] = true
		rows = append(rows, Watcher{AnchorKind: kind, AnchorID: anchorID, UserID: uid})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (s *Service) Edit(ctx context.Context, postID, authorUID int64, raw string) (*Post, error) {
	body, err := NormalizeBody(raw)
	if err != nil {
		return nil, err
	}
	var post Post
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&post, postID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPostNotFound
			}
			return err
		}
		if post.AuthorUID != authorUID {
			return ErrNotAuthor
		}
		if post.Status != StatusVisible {
			return ErrPostClosed
		}
		if post.Body == body {
			return nil
		}
		now := time.Now()
		post.Body, post.EditedAt = body, &now
		return tx.Model(&post).Updates(map[string]any{"body": body, "edited_at": now}).Error
	})
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (s *Service) Delete(ctx context.Context, postID, authorUID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var post Post
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&post, postID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPostNotFound
			}
			return err
		}
		if post.AuthorUID != authorUID {
			return ErrNotAuthor
		}
		if post.Status == StatusDeleted {
			return nil
		}
		return tx.Model(&post).Updates(map[string]any{"status": StatusDeleted, "body": ""}).Error
	})
}

func (s *Service) Moderate(ctx context.Context, postID, moderatorUID int64, hidden bool, note string) (*Post, error) {
	var post Post
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&post, postID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPostNotFound
			}
			return err
		}
		if post.Status == StatusDeleted {
			return ErrPostClosed
		}
		next := StatusVisible
		if hidden {
			next = StatusHidden
		}
		now := time.Now()
		post.Status, post.ModeratedByUID, post.ModeratedAt, post.ModerationNote = next, &moderatorUID, &now, note
		return tx.Model(&post).Updates(map[string]any{
			"status": next, "moderated_by_uid": moderatorUID, "moderated_at": now, "moderation_note": note,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &post, nil
}

const (
	ReasonReply   = "reply"
	ReasonMention = "mention"
)

type Notice struct {
	Post
	Reason string `gorm:"column:reason"`
}

func (s *Service) Notices(ctx context.Context, uid int64, since time.Time, limit int) ([]Notice, error) {
	var rows []Notice
	err := s.db.WithContext(ctx).Raw(`
		SELECT p.*, CASE WHEN p.mention_uids @> to_jsonb(?::bigint) THEN 'mention' ELSE 'reply' END AS reason
		  FROM edit_discussion_post p
		  JOIN edit_discussion_watcher w
		    ON w.anchor_kind = p.anchor_kind AND w.anchor_id = p.anchor_id AND w.user_id = ?
		 WHERE p.status = ? AND p.author_uid <> ? AND p.created_at > ?
		   AND (NOT w.muted OR p.mention_uids @> to_jsonb(?::bigint))
		 ORDER BY p.id DESC
		 LIMIT ?`, uid, uid, StatusVisible, uid, since, uid, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Service) SetMuted(ctx context.Context, kindName string, anchorID, uid int64, muted bool) error {
	if _, err := s.kind(kindName); err != nil {
		return err
	}
	row := Watcher{AnchorKind: kindName, AnchorID: anchorID, UserID: uid, Muted: muted}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "anchor_kind"}, {Name: "anchor_id"}, {Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{"muted": muted, "updated_at": time.Now()}),
	}).Create(&row).Error
}

type WatchState struct {
	Watching bool
	Muted    bool
}

func (s *Service) WatchState(ctx context.Context, kindName string, anchorID, uid int64) (WatchState, error) {
	var w Watcher
	err := s.db.WithContext(ctx).
		Where("anchor_kind = ? AND anchor_id = ? AND user_id = ?", kindName, anchorID, uid).
		Take(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return WatchState{}, nil
	}
	if err != nil {
		return WatchState{}, err
	}
	return WatchState{Watching: !w.Muted, Muted: w.Muted}, nil
}

func MentionsOf(p *Post) []int64 {
	var out []int64
	_ = json.Unmarshal(p.MentionUIDs, &out)
	return out
}
