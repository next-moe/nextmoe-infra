package repository

import (
	"fmt"
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *ThreadRepository) CountOpenedByCreatorSince(creatorID int64, since time.Time) (int64, error) {
	var n int64
	err := r.db.Model(&model.CommunityThread{}).
		Where("created_by = ? AND created_at >= ? AND kind IN ?",
			creatorID, since, []int16{model.ThreadKindTopic, model.ThreadKindFeedback}).
		Count(&n).Error
	return n, err
}

type ThreadRepository struct{ db *gorm.DB }

func NewThreadRepository(db *gorm.DB) *ThreadRepository { return &ThreadRepository{db: db} }

func (r *ThreadRepository) GetByID(id int64) (*model.CommunityThread, error) {
	return getThread(r.db, id)
}

func getThread(db *gorm.DB, id int64) (*model.CommunityThread, error) {
	var t model.CommunityThread
	err := db.First(&t, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func GetThreadTx(tx *gorm.DB, id int64) (*model.CommunityThread, error) {
	return getThread(tx, id)
}

func (r *ThreadRepository) GetLiveCommentsThread(site string, anchorKind int16, anchorID string) (*model.CommunityThread, error) {
	return getLiveCommentsThread(r.db, site, anchorKind, anchorID)
}

func GetLiveCommentsThreadTx(tx *gorm.DB, site string, anchorKind int16, anchorID string) (*model.CommunityThread, error) {
	return getLiveCommentsThread(tx, site, anchorKind, anchorID)
}

func getLiveCommentsThread(db *gorm.DB, site string, anchorKind int16, anchorID string) (*model.CommunityThread, error) {
	q := db.Where(
		"anchor_kind = ? AND anchor_id = ? AND kind = ? AND status <> ?",
		anchorKind, anchorID, model.ThreadKindComments, model.ThreadStatusDeleted,
	)
	if model.AnchorIsSiteLocal(anchorKind) {
		q = q.Where("site = ?", site)
	}
	var t model.CommunityThread
	err := q.First(&t).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ThreadSort names the orderings the site thread list can be read in. The key
// is part of the cursor (see the handler's codec): a cursor minted under one
// sort cannot be replayed under another.
type ThreadSort string

const (
	ThreadSortActivity ThreadSort = "activity"
	ThreadSortCreated  ThreadSort = "created"
	ThreadSortPosts    ThreadSort = "posts"
)

// ThreadCursor carries whichever sort key the page ended on; ID == 0 means
// "first page". Only the field matching Sort is populated.
type ThreadCursor struct {
	Sort           ThreadSort
	LastPostedNull bool
	LastPosted     time.Time
	Created        time.Time
	PostsCount     int32
	ID             int64
}

type PinFilter string

const (
	PinFilterAny     PinFilter = "any"
	PinFilterOnly    PinFilter = "only"
	PinFilterExclude PinFilter = "exclude"
)

type ThreadListQuery struct {
	Site       string
	Kind       int16
	AnchorKind int16
	AnchorID   string
	BoardIDs   []int64
	Sort       ThreadSort
	HasPosts   bool
	Pinned     PinFilter
	Cursor     ThreadCursor
	Limit      int
}

func (r *ThreadRepository) List(q ThreadListQuery) ([]model.CommunityThread, error) {
	db := r.db.Where("site = ? AND kind = ?", q.Site, q.Kind)
	switch {
	case len(q.BoardIDs) > 0:
		anchors := make([]string, len(q.BoardIDs))
		for i, id := range q.BoardIDs {
			anchors[i] = model.BoardAnchorID(id)
		}
		db = db.Where(boardTopicSQL).Where("anchor_id IN ?", anchors)
	case q.AnchorID != "":
		db = db.Where("anchor_kind = ? AND anchor_id = ?", q.AnchorKind, q.AnchorID)
	}
	if q.HasPosts {
		db = db.Where("posts_count > 0")
	}
	pinned := pinnedSQL(len(q.BoardIDs) > 0)
	switch q.Pinned {
	case PinFilterOnly:
		var rows []model.CommunityThread
		err := db.Where(pinned, time.Now()).Order("pinned_at DESC, id DESC").Limit(q.Limit).Find(&rows).Error
		return rows, err
	case PinFilterExclude:
		db = db.Where("NOT "+pinned, time.Now())
	}
	db = orderThreads(db, q.Sort, q.Cursor)

	var rows []model.CommunityThread
	err := db.Limit(q.Limit).Find(&rows).Error
	return rows, err
}

// The scope is a literal for the same reason boardTopicSQL's values are.
func pinnedSQL(inBoard bool) string {
	scope := model.PinScopeSite
	if inBoard {
		scope = model.PinScopeBoard
	}
	return fmt.Sprintf("(community_thread.pin_scope >= %d AND "+
		"(community_thread.pinned_until IS NULL OR community_thread.pinned_until > ?))", scope)
}

func orderThreads(db *gorm.DB, sort ThreadSort, cursor ThreadCursor) *gorm.DB {
	switch sort {
	case ThreadSortCreated:
		if cursor.ID != 0 {
			db = db.Where("created_at < ? OR (created_at = ? AND id < ?)", cursor.Created, cursor.Created, cursor.ID)
		}
		return db.Order("created_at DESC, id DESC")
	case ThreadSortPosts:
		if cursor.ID != 0 {
			db = db.Where("posts_count < ? OR (posts_count = ? AND id < ?)", cursor.PostsCount, cursor.PostsCount, cursor.ID)
		}
		return db.Order("posts_count DESC, id DESC")
	default:
		if cursor.ID != 0 {
			if cursor.LastPostedNull {
				db = db.Where("last_posted_at IS NULL AND id < ?", cursor.ID)
			} else {
				db = db.Where(
					"last_posted_at IS NULL OR last_posted_at < ? OR (last_posted_at = ? AND id < ?)",
					cursor.LastPosted, cursor.LastPosted, cursor.ID,
				)
			}
		}
		return db.Order("last_posted_at DESC NULLS LAST, id DESC")
	}
}

type OpeningPostMeta struct {
	ThreadID int64 `gorm:"column:thread_id"`
	Status   int16 `gorm:"column:status"`
	AuthorID int64 `gorm:"column:author_id"`
}

func (r *ThreadRepository) OpeningPostMetaByThreadIDs(threadIDs []int64) (map[int64]OpeningPostMeta, error) {
	out := make(map[int64]OpeningPostMeta, len(threadIDs))
	if len(threadIDs) == 0 {
		return out, nil
	}
	var rows []OpeningPostMeta
	err := r.db.Model(&model.CommunityPost{}).
		Select("thread_id, status, author_id").
		Where("thread_id IN ? AND post_number = 1", threadIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, m := range rows {
		out[m.ThreadID] = m
	}
	return out, nil
}

func (r *ThreadRepository) ListByAnchor(site string, anchorKind int16, anchorID string, kind int16) ([]model.CommunityThread, error) {
	q := r.db.Where("anchor_kind = ? AND anchor_id = ? AND kind = ?", anchorKind, anchorID, kind)
	if model.AnchorIsSiteLocal(anchorKind) {
		q = q.Where("site = ?", site)
	}
	var rows []model.CommunityThread
	err := q.Order("last_posted_at DESC NULLS LAST, id DESC").Find(&rows).Error
	return rows, err
}

func CreateThreadTx(tx *gorm.DB, t *model.CommunityThread, bornEmpty bool) error {
	if err := tx.Create(t).Error; err != nil {
		return err
	}
	if bornEmpty {
		return zeroParticipantsTx(tx, t)
	}
	return nil
}

// CreateCommentsThreadIfAbsentTx mints an anchor's comments thread unless a
// concurrent first comment beat this one to it, and reports whether this call
// is the one that created it. The conflict is swallowed by Postgres rather than
// raised: the caller is already inside the transaction that writes the first
// post, and a duplicate-key error would abort that transaction instead of
// letting it fall back to the row the winner inserted.
func CreateCommentsThreadIfAbsentTx(tx *gorm.DB, t *model.CommunityThread) (bool, error) {
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(t)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	return true, zeroParticipantsTx(tx, t)
}

// participants_count carries `default:1`, and GORM sends the column default for
// a zero-valued field — so a thread whose participant is counted later, by
// AllocateReplyTx, has to be reset to 0 after the insert or it counts one
// person twice.
func zeroParticipantsTx(tx *gorm.DB, t *model.CommunityThread) error {
	if err := tx.Model(t).UpdateColumn("participants_count", 0).Error; err != nil {
		return err
	}
	t.ParticipantsCount = 0
	return nil
}

func AllocateReplyTx(tx *gorm.DB, threadID int64, at time.Time, newParticipant bool) (int32, error) {
	incParticipant := 0
	if newParticipant {
		incParticipant = 1
	}
	var number int32
	err := tx.Raw(`
		UPDATE community_thread
		   SET posts_count         = posts_count + 1,
		       highest_post_number = highest_post_number + 1,
		       participants_count  = participants_count + ?,
		       last_posted_at      = ?,
		       updated_at          = ?
		 WHERE id = ?
		RETURNING highest_post_number`,
		incParticipant, at, at, threadID,
	).Scan(&number).Error
	return number, err
}

func LockThreadTx(tx *gorm.DB, id int64) (*model.CommunityThread, error) {
	return getThread(tx.Clauses(clause.Locking{Strength: LockUpdate}), id)
}

func UpdateThreadTx(tx *gorm.DB, id int64, updates map[string]any) error {
	updates["updated_at"] = time.Now()
	return tx.Model(&model.CommunityThread{}).Where("id = ?", id).Updates(updates).Error
}

func RaisePostRatingTx(tx *gorm.DB, threadID int64, floor int16) error {
	return tx.Model(&model.CommunityPost{}).
		Where("thread_id = ? AND content_rating < ?", threadID, floor).
		Update("content_rating", floor).Error
}
