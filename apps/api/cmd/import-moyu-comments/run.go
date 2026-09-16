package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/sanitize"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const trustBatch = 1000

func run(src, tgt *gorm.DB, site string, apply bool) (*Report, error) {
	rep := &Report{}

	comments, unparsableEdits, err := loadComments(src)
	if err != nil {
		return nil, fmt.Errorf("load patch_comment: %w", err)
	}
	rep.SourceComments = len(comments)
	rep.UnparsableEdits = unparsableEdits

	existingMap, err := loadExistingMap(src)
	if err != nil {
		return nil, fmt.Errorf("load ledger: %w", err)
	}

	if apply {
		if err := src.Exec(mapTableDDL).Error; err != nil {
			return nil, fmt.Errorf("create ledger table: %w", err)
		}
	}

	byWall, walls := groupByWall(comments)
	rep.Walls = len(walls)

	authors := make(map[int64]bool)
	oldToNew := make(map[int]int64, len(comments))
	for old, post := range existingMap {
		oldToNew[old] = post
	}

	for _, w := range walls {
		plan := planWall(w, byWall[w.key()])
		for i := range plan.Posts {
			authors[plan.Posts[i].AuthorID] = true
		}
		rep.DanglingParents += plan.Dangling
		rep.HiddenRows += plan.Hidden
		rep.OverLenRows += plan.OverLen
		if w.ResourceID != nil {
			rep.ResourceWalls++
		} else {
			rep.GameWalls++
		}

		if err := importWall(tgt, src, site, plan, oldToNew, apply, rep); err != nil {
			return nil, fmt.Errorf("wall %v: %w", w, err)
		}
	}

	if err := seedTrust(tgt, authors, apply, rep); err != nil {
		return nil, err
	}
	known := make(map[int]bool, len(comments))
	for i := range comments {
		known[comments[i].ID] = true
	}
	if err := importLikes(src, tgt, oldToNew, known, apply, rep); err != nil {
		return nil, err
	}
	return rep, nil
}

func importWall(tgt, src *gorm.DB, site string, plan threadPlan,
	oldToNew map[int]int64, apply bool, rep *Report) error {

	anchorKind, anchorID := plan.Wall.anchor()

	if !apply {
		return dryRunWall(tgt, site, anchorKind, anchorID, plan, oldToNew, rep)
	}

	type mapRow struct {
		old  int
		post int64
	}
	var newRows []mapRow
	var threadID int64

	write := func(tx *gorm.DB) error {
		var th model.CommunityThread
		findErr := tx.Where(
			"site = ? AND anchor_kind = ? AND anchor_id = ? AND kind = ? AND status <> ?",
			site, anchorKind, anchorID, model.ThreadKindComments, model.ThreadStatusDeleted,
		).First(&th).Error

		switch {
		case findErr == nil:
			rep.ThreadsExisting++
		case errors.Is(findErr, gorm.ErrRecordNotFound):
			th = model.CommunityThread{
				Site:              site,
				Kind:              model.ThreadKindComments,
				AnchorKind:        anchorKind,
				AnchorID:          anchorID,
				ContentRating:     model.ContentRatingAll,
				Status:            model.ThreadStatusOpen,
				PostsCount:        plan.PostsCount,
				ParticipantsCount: plan.ParticipantsCount,
				HighestPostNumber: plan.HighestPostNumber,
				LastPostedAt:      &plan.LastPostedAt,
				CreatedBy:         plan.FirstAuthorID,
				CreatedAt:         plan.CreatedAt,
				UpdatedAt:         plan.LastPostedAt,
			}
			if err := tx.Create(&th).Error; err != nil {
				return fmt.Errorf("create thread: %w", err)
			}
			rep.ThreadsCreated++
		default:
			return fmt.Errorf("find thread: %w", findErr)
		}
		threadID = th.ID

		// A post already carrying this thread's post_number was written by an
		// earlier run whose ledger write did not land. Adopt it instead of
		// inserting a second copy: the unique on (thread_id, post_number) would
		// refuse it anyway, and the ledger is what the deep links read.
		existingByNumber := make(map[int32]int64)
		var pnRows []struct {
			PostNumber int32
			ID         int64
		}
		if err := tx.Model(&model.CommunityPost{}).
			Select("post_number, id").Where("thread_id = ?", th.ID).
			Find(&pnRows).Error; err != nil {
			return fmt.Errorf("load existing posts: %w", err)
		}
		for _, r := range pnRows {
			existingByNumber[r.PostNumber] = r.ID
		}

		for i := range plan.Posts {
			pp := plan.Posts[i]
			if _, done := oldToNew[pp.OldID]; done {
				rep.PostsExisting++
				continue
			}
			if id, ok := existingByNumber[pp.PostNumber]; ok {
				oldToNew[pp.OldID] = id
				newRows = append(newRows, mapRow{pp.OldID, id})
				rep.PostsExisting++
				continue
			}
			cooked := sanitize.Cook(pp.Content)
			post := model.CommunityPost{
				ThreadID:         th.ID,
				PostNumber:       pp.PostNumber,
				RootPostID:       resolvePtr(oldToNew, pp.RootOldID),
				ReplyToPostID:    resolvePtr(oldToNew, pp.ReplyToOldID),
				TargetUserID:     pp.TargetUserID,
				AuthorID:         pp.AuthorID,
				ContentRaw:       pp.Content,
				ContentHTML:      cooked.HTML,
				SanitizerVersion: int32(cooked.Version),
				ContentRating:    model.ContentRatingAll,
				Status:           pp.Status,
				EditedAt:         pp.EditedAt,
				CreatedAt:        pp.CreatedAt,
			}
			if err := tx.Create(&post).Error; err != nil {
				return fmt.Errorf("create post (old_id=%d): %w", pp.OldID, err)
			}
			oldToNew[pp.OldID] = post.ID
			newRows = append(newRows, mapRow{pp.OldID, post.ID})
			rep.PostsInserted++
		}

		return recomputeThreadCounters(tx, th.ID)
	}

	if err := tgt.Transaction(write); err != nil {
		return err
	}

	if len(newRows) > 0 {
		rows := make([]commentMap, 0, len(newRows))
		for _, m := range newRows {
			rows = append(rows, commentMap{
				OldCommentID: m.old, ThreadID: threadID, PostID: m.post,
				GalgameID: plan.Wall.GalgameID, ResourceID: plan.Wall.ResourceID,
			})
		}
		if err := src.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 500).Error; err != nil {
			return fmt.Errorf("write ledger rows: %w", err)
		}
		rep.LedgerRows += len(rows)
	}
	return nil
}

func dryRunWall(tgt *gorm.DB, site string, anchorKind int16, anchorID string,
	plan threadPlan, oldToNew map[int]int64, rep *Report) error {

	var th model.CommunityThread
	findErr := tgt.Where(
		"site = ? AND anchor_kind = ? AND anchor_id = ? AND kind = ? AND status <> ?",
		site, anchorKind, anchorID, model.ThreadKindComments, model.ThreadStatusDeleted,
	).First(&th).Error
	switch {
	case findErr == nil:
		rep.ThreadsExisting++
	case errors.Is(findErr, gorm.ErrRecordNotFound):
		rep.ThreadsCreated++
	default:
		return fmt.Errorf("dry-run find thread: %w", findErr)
	}

	for i := range plan.Posts {
		if _, imported := oldToNew[plan.Posts[i].OldID]; imported {
			rep.PostsExisting++
		} else {
			rep.PostsInserted++
			rep.LedgerRows++
		}
	}
	return nil
}

// importLikes replays the likes into community_reaction, which is where the
// read faces count them from.
//
// The trust counters move with them. community_trust.likes_given/received
// describe exactly these rows -- the toggle face maintains them in the same
// transaction as the reaction -- so an import that inserted reactions alone
// would leave every imported like invisible to trust promotion and the drift
// would never be noticed.
func importLikes(src, tgt *gorm.DB, oldToNew map[int]int64, known map[int]bool, apply bool, rep *Report) error {
	likes, err := loadLikes(src)
	if err != nil {
		return fmt.Errorf("load likes: %w", err)
	}
	rep.SourceLikes = len(likes)

	type reaction struct {
		PostID    int64
		UserID    int64
		CreatedAt time.Time
	}
	rows := make([]reaction, 0, len(likes))
	for _, l := range likes {
		postID, ok := oldToNew[l.CommentID]
		if ok {
			rows = append(rows, reaction{PostID: postID, UserID: int64(l.UserID), CreatedAt: l.Created})
			continue
		}
		// A dry run has no post ids yet, so "no post" is only orphaned when the
		// comment it points at is missing from the source too. Counting those as
		// orphans would report all 653 likes lost on the run we read to decide
		// whether to apply.
		if known[l.CommentID] {
			rep.LikesToInsert++
			continue
		}
		rep.LikesOrphaned++
	}
	rep.LikesToInsert += len(rows)
	if !apply || len(rows) == 0 {
		return nil
	}

	given := map[int64]int32{}
	received := map[int64]int32{}
	return tgt.Transaction(func(tx *gorm.DB) error {
		for _, r := range rows {
			res := tx.Exec(`
				INSERT INTO community_reaction (post_id, user_id, kind, created_at)
				VALUES (?, ?, ?, ?)
				ON CONFLICT (post_id, user_id, kind) DO NOTHING`,
				r.PostID, r.UserID, model.ReactionKindLike, r.CreatedAt)
			if res.Error != nil {
				return fmt.Errorf("insert reaction (post=%d user=%d): %w", r.PostID, r.UserID, res.Error)
			}
			if res.RowsAffected == 0 {
				rep.LikesExisting++
				continue
			}
			rep.LikesInserted++
			given[r.UserID]++
			var author int64
			if err := tx.Raw(`SELECT author_id FROM community_post WHERE id = ?`, r.PostID).
				Scan(&author).Error; err != nil {
				return fmt.Errorf("read post author (post=%d): %w", r.PostID, err)
			}
			received[author]++
		}
		for userID, n := range given {
			if err := adjustTrustLikes(tx, userID, n, 0); err != nil {
				return err
			}
		}
		for userID, n := range received {
			if err := adjustTrustLikes(tx, userID, 0, n); err != nil {
				return err
			}
		}
		return nil
	})
}

func adjustTrustLikes(tx *gorm.DB, userID int64, given, received int32) error {
	return tx.Exec(`
		INSERT INTO community_trust (user_id, level, first_posts_held_remaining,
		                             likes_given, likes_received, updated_at)
		VALUES (?, ?, 0, ?, ?, now())
		ON CONFLICT (user_id) DO UPDATE
		   SET likes_given    = COALESCE(community_trust.likes_given, 0) + EXCLUDED.likes_given,
		       likes_received = COALESCE(community_trust.likes_received, 0) + EXCLUDED.likes_received,
		       updated_at     = now()`,
		userID, model.TrustLevelBasic, given, received).Error
}

func recomputeThreadCounters(tx *gorm.DB, threadID int64) error {
	var agg struct {
		Posts        int32
		Participants int32
		Highest      int32
		Last         *time.Time
	}
	if err := tx.Model(&model.CommunityPost{}).
		Select("COUNT(*) AS posts, COUNT(DISTINCT author_id) AS participants, "+
			"COALESCE(MAX(post_number),0) AS highest, MAX(created_at) AS last").
		Where("thread_id = ?", threadID).Scan(&agg).Error; err != nil {
		return fmt.Errorf("aggregate counters: %w", err)
	}
	participants := agg.Participants
	if participants < 1 {
		participants = 1
	}
	return tx.Exec(
		"UPDATE community_thread SET posts_count = ?, participants_count = ?, "+
			"highest_post_number = ?, last_posted_at = ?, updated_at = ? WHERE id = ?",
		agg.Posts, participants, agg.Highest, agg.Last, agg.Last, threadID,
	).Error
}

// seedTrust gives every imported author a row, and never touches one that is
// already there: 1,199 of these people are kungal forum users whose trust level
// and held-post budget are theirs, earned on another site — trust is keyed on
// the user alone, with no site column.
func seedTrust(tgt *gorm.DB, authors map[int64]bool, apply bool, rep *Report) error {
	ids := make([]int64, 0, len(authors))
	for id := range authors {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	present := make(map[int64]bool)
	for start := 0; start < len(ids); start += trustBatch {
		end := min(start+trustBatch, len(ids))
		var have []int64
		if err := tgt.Model(&model.CommunityTrust{}).
			Where("user_id IN ?", ids[start:end]).Pluck("user_id", &have).Error; err != nil {
			return fmt.Errorf("probe trust: %w", err)
		}
		for _, id := range have {
			present[id] = true
		}
	}

	var absent []int64
	for _, id := range ids {
		if present[id] {
			rep.TrustPresent++
		} else {
			rep.TrustSeeded++
			absent = append(absent, id)
		}
	}
	if !apply || len(absent) == 0 {
		return nil
	}

	now := time.Now()
	for start := 0; start < len(absent); start += trustBatch {
		end := min(start+trustBatch, len(absent))
		chunk := absent[start:end]
		placeholders := make([]string, 0, len(chunk))
		args := make([]any, 0, len(chunk)*4)
		for _, id := range chunk {
			placeholders = append(placeholders, "(?, ?, ?, ?)")
			args = append(args, id, model.TrustLevelBasic, 0, now)
		}
		sql := "INSERT INTO community_trust (user_id, level, first_posts_held_remaining, updated_at) VALUES " +
			strings.Join(placeholders, ", ") + " ON CONFLICT (user_id) DO NOTHING"
		if err := tgt.Exec(sql, args...).Error; err != nil {
			return fmt.Errorf("seed trust: %w", err)
		}
	}
	return nil
}

func resolvePtr(m map[int]int64, oldID *int) *int64 {
	if oldID == nil {
		return nil
	}
	if v, ok := m[*oldID]; ok {
		return &v
	}
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }
