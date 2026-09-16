package service

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

const (
	boardSlugMaxLen          = 64
	boardNameMaxRunes        = 64
	boardDescriptionMaxRunes = 1000
	boardIconMaxRunes        = 64
	boardColorMaxRunes       = 32
	boardTemplateMaxRunes    = 10000
)

var (
	boardSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	allDigits        = regexp.MustCompile(`^[0-9]+$`)
)

type BoardService struct {
	db     *gorm.DB
	boards *repository.BoardRepository
}

func NewBoardService(db *gorm.DB) *BoardService {
	return &BoardService{db: db, boards: repository.NewBoardRepository(db)}
}

type Board struct {
	model.CommunityBoard
	Stats repository.BoardStatsRow
}

func (s *BoardService) List(site string) ([]Board, error) {
	rows, err := s.boards.ListBySite(site)
	if err != nil {
		return nil, err
	}
	stats, err := s.boards.SiteStats(site)
	if err != nil {
		return nil, err
	}
	ordered := treeOrder(rows)
	out := make([]Board, len(ordered))
	for i := range ordered {
		out[i] = Board{CommunityBoard: ordered[i], Stats: stats[ordered[i].ID]}
	}
	return out, nil
}

func (s *BoardService) Get(site string, id int64) (*Board, error) {
	b, err := s.boards.Get(site, id)
	if err != nil {
		return nil, err
	}
	return s.withStats(b)
}

func (s *BoardService) GetBySlug(site, slug string) (*Board, error) {
	b, err := s.boards.GetBySlug(site, slug)
	if err != nil {
		return nil, err
	}
	return s.withStats(b)
}

func (s *BoardService) ListingIDs(site string, id int64, withSubboards bool) ([]int64, error) {
	b, err := s.boards.Get(site, id)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrBoardNotFound
	}
	if !withSubboards {
		return []int64{id}, nil
	}
	return s.boards.SubtreeIDs(site, id)
}

func (s *BoardService) withStats(b *model.CommunityBoard) (*Board, error) {
	if b == nil {
		return nil, ErrBoardNotFound
	}
	stats, err := s.boards.BoardStats(b.Site, b.ID)
	if err != nil {
		return nil, err
	}
	return &Board{CommunityBoard: *b, Stats: stats}, nil
}

type BoardFields struct {
	ParentID           *int64
	Slug               string
	Name               string
	Description        *string
	Icon               *string
	Color              *string
	TopicTemplate      *string
	Format             int16
	ContentRating      int16
	TopicMinTrustLevel int16
	ReplyMinTrustLevel int16
}

func (s *BoardService) Create(ctx context.Context, site string, actorID int64, f BoardFields) (*Board, error) {
	b := model.CommunityBoard{
		Site: site, ParentID: f.ParentID, Slug: f.Slug, Name: strings.TrimSpace(f.Name),
		Description: optionalText(f.Description), Icon: optionalText(f.Icon), Color: optionalText(f.Color),
		TopicTemplate: optionalText(f.TopicTemplate),
		Format:        f.Format, Status: model.BoardStatusActive, ContentRating: f.ContentRating,
		TopicMinTrustLevel: f.TopicMinTrustLevel, ReplyMinTrustLevel: f.ReplyMinTrustLevel,
	}
	if err := validateBoard(&b); err != nil {
		return nil, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if b.ParentID != nil {
			if err := checkParentTx(tx, site, *b.ParentID, 0); err != nil {
				return err
			}
		}
		pos, err := repository.NextBoardPositionTx(tx, site, b.ParentID)
		if err != nil {
			return err
		}
		b.Position = pos
		return boardWriteErr(repository.CreateBoardTx(tx, &b))
	})
	if err != nil {
		return nil, err
	}
	slog.Info("community board created", "site", site, "board_id", b.ID, "slug", b.Slug, "actor_id", actorID)
	return &Board{CommunityBoard: b}, nil
}

type BoardPatch struct {
	ParentID           *int64
	Slug               *string
	Name               *string
	Description        *string
	Icon               *string
	Color              *string
	TopicTemplate      *string
	Format             *int16
	Status             *int16
	ContentRating      *int16
	TopicMinTrustLevel *int16
	ReplyMinTrustLevel *int16
}

func (s *BoardService) Update(ctx context.Context, site string, id, actorID int64, p BoardPatch) (*Board, error) {
	var next model.CommunityBoard
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cur, err := repository.LockBoardTx(tx, site, id, repository.LockUpdate)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrBoardNotFound
		}
		next = *cur
		applyBoardPatch(&next, p)
		if err := validateBoard(&next); err != nil {
			return err
		}
		if p.ParentID != nil {
			if err := reparentTx(tx, cur, &next, *p.ParentID); err != nil {
				return err
			}
		}
		return boardWriteErr(repository.UpdateBoardTx(tx, id, map[string]any{
			"parent_id": next.ParentID, "position": next.Position,
			"slug": next.Slug, "name": next.Name,
			"description": next.Description, "icon": next.Icon, "color": next.Color,
			"topic_template": next.TopicTemplate,
			"format":         next.Format, "status": next.Status, "content_rating": next.ContentRating,
			"topic_min_trust_level": next.TopicMinTrustLevel, "reply_min_trust_level": next.ReplyMinTrustLevel,
		}))
	})
	if err != nil {
		return nil, err
	}
	slog.Info("community board updated", "site", site, "board_id", id, "actor_id", actorID)
	return s.Get(site, id)
}

func applyBoardPatch(b *model.CommunityBoard, p BoardPatch) {
	if p.Slug != nil {
		b.Slug = *p.Slug
	}
	if p.Name != nil {
		b.Name = strings.TrimSpace(*p.Name)
	}
	for _, f := range []struct {
		in  *string
		out **string
	}{
		{p.Description, &b.Description}, {p.Icon, &b.Icon}, {p.Color, &b.Color}, {p.TopicTemplate, &b.TopicTemplate},
	} {
		if f.in != nil {
			*f.out = optionalText(f.in)
		}
	}
	for _, f := range []struct {
		in  *int16
		out *int16
	}{
		{p.Format, &b.Format}, {p.Status, &b.Status}, {p.ContentRating, &b.ContentRating},
		{p.TopicMinTrustLevel, &b.TopicMinTrustLevel}, {p.ReplyMinTrustLevel, &b.ReplyMinTrustLevel},
	} {
		if f.in != nil {
			*f.out = *f.in
		}
	}
}

func reparentTx(tx *gorm.DB, cur, next *model.CommunityBoard, parentID int64) error {
	var parent *int64
	if parentID > 0 {
		parent = &parentID
	}
	if sameParent(cur.ParentID, parent) {
		return nil
	}
	if parent != nil {
		hasChildren, err := repository.BoardHasChildrenTx(tx, cur.ID)
		if err != nil {
			return err
		}
		if hasChildren {
			return &ConflictError{Reason: "a board with sub-boards cannot become a sub-board"}
		}
		if err := checkParentTx(tx, cur.Site, parentID, cur.ID); err != nil {
			return err
		}
	}
	pos, err := repository.NextBoardPositionTx(tx, cur.Site, parent)
	if err != nil {
		return err
	}
	next.ParentID, next.Position = parent, pos
	return nil
}

func (s *BoardService) Delete(ctx context.Context, site string, id, actorID int64) error {
	var slug string
	var anchorSubs int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		b, err := repository.LockBoardTx(tx, site, id, repository.LockUpdate)
		if err != nil {
			return err
		}
		if b == nil {
			return ErrBoardNotFound
		}
		slug = b.Slug
		hasChildren, err := repository.BoardHasChildrenTx(tx, id)
		if err != nil {
			return err
		}
		if hasChildren {
			return &ConflictError{Reason: "the board still has sub-boards"}
		}
		hasThreads, err := repository.BoardHasThreadsTx(tx, b)
		if err != nil {
			return err
		}
		if hasThreads {
			return &ConflictError{Reason: "the board still holds topics; move them away or archive the board"}
		}
		n, err := repository.DeleteBoardAnchorUsersTx(tx, site, id)
		if err != nil {
			return err
		}
		anchorSubs = n
		return repository.DeleteBoardTx(tx, id)
	})
	if err != nil {
		return err
	}
	slog.Info("community board deleted", "site", site, "board_id", id, "slug", slug, "actor_id", actorID,
		"anchor_subscriptions_deleted", anchorSubs)
	return nil
}

func (s *BoardService) Reorder(ctx context.Context, site string, parentID int64, ids []int64, actorID int64) error {
	var parent *int64
	if parentID > 0 {
		parent = &parentID
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if parent != nil {
			p, err := repository.LockBoardTx(tx, site, parentID, repository.LockShare)
			if err != nil {
				return err
			}
			if p == nil {
				return ErrBoardNotFound
			}
		}
		siblings, err := repository.LockSiblingBoardsTx(tx, site, parent)
		if err != nil {
			return err
		}
		if !sameIDSet(siblings, ids) {
			return &InvalidError{Reason: "board_ids must name every board under this parent exactly once"}
		}
		for i, id := range ids {
			if err := repository.SetBoardPositionTx(tx, id, int32(i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	slog.Info("community boards reordered", "site", site, "parent_id", parentID, "board_ids", ids, "actor_id", actorID)
	return nil
}

func checkParentTx(tx *gorm.DB, site string, parentID, childID int64) error {
	if parentID == childID {
		return &InvalidError{Reason: "a board cannot be its own parent"}
	}
	parent, err := repository.LockBoardTx(tx, site, parentID, repository.LockShare)
	if err != nil {
		return err
	}
	if parent == nil {
		return &InvalidError{Reason: "parent board not found"}
	}
	if parent.ParentID != nil {
		return &InvalidError{Reason: "boards nest one level: the parent is itself a sub-board"}
	}
	return nil
}

func validateBoard(b *model.CommunityBoard) error {
	reason := ""
	switch {
	case len(b.Slug) > boardSlugMaxLen || !boardSlugPattern.MatchString(b.Slug):
		reason = fmt.Sprintf("slug must be 1-%d lowercase letters, digits and single hyphens", boardSlugMaxLen)
	case allDigits.MatchString(b.Slug):
		reason = "slug cannot be all digits: a numeric board key is read as a board id"
	case b.Name == "" || utf8.RuneCountInString(b.Name) > boardNameMaxRunes:
		reason = fmt.Sprintf("name must be 1-%d characters", boardNameMaxRunes)
	case tooLong(b.Description, boardDescriptionMaxRunes):
		reason = fmt.Sprintf("description must be at most %d characters", boardDescriptionMaxRunes)
	case tooLong(b.Icon, boardIconMaxRunes):
		reason = fmt.Sprintf("icon must be at most %d characters", boardIconMaxRunes)
	case tooLong(b.Color, boardColorMaxRunes):
		reason = fmt.Sprintf("color must be at most %d characters", boardColorMaxRunes)
	case tooLong(b.TopicTemplate, boardTemplateMaxRunes):
		reason = fmt.Sprintf("topic_template must be at most %d characters", boardTemplateMaxRunes)
	case b.Format < model.BoardFormatDiscussion || b.Format > model.BoardFormatAnnouncement:
		reason = "format must be 0 (discussion), 1 (qa) or 2 (announcement)"
	case b.Status != model.BoardStatusActive && b.Status != model.BoardStatusArchived:
		reason = "status must be 0 (active) or 1 (archived)"
	case b.ContentRating < model.ContentRatingAll || b.ContentRating > model.ContentRatingR18:
		reason = "content_rating must be 0-2"
	case !trustGateInRange(b.TopicMinTrustLevel) || !trustGateInRange(b.ReplyMinTrustLevel):
		reason = "trust level gates must be 0-3: staff are floored at 3, so a higher gate would lock them out"
	}
	if reason != "" {
		return &InvalidError{Reason: reason}
	}
	return nil
}

func trustGateInRange(level int16) bool {
	return level >= model.TrustLevelNew && level <= model.TrustLevelRegular
}

func topicGate(b *model.CommunityBoard, level int16, asModerator bool) error {
	if b.Status == model.BoardStatusArchived {
		return &ConflictError{Reason: "board is archived"}
	}
	if asModerator {
		return nil
	}
	if b.Format == model.BoardFormatAnnouncement {
		return &ForbiddenError{Reason: "only moderators open topics on an announcement board"}
	}
	if level < b.TopicMinTrustLevel {
		return &ForbiddenError{Reason: fmt.Sprintf("opening a topic on this board takes trust level %d", b.TopicMinTrustLevel)}
	}
	return nil
}

func replyGate(b *model.CommunityBoard, level int16) error {
	if b.Status == model.BoardStatusArchived {
		return &ConflictError{Reason: "board is archived"}
	}
	if level < b.ReplyMinTrustLevel {
		return &ForbiddenError{Reason: fmt.Sprintf("replying on this board takes trust level %d", b.ReplyMinTrustLevel)}
	}
	return nil
}

func treeOrder(boards []model.CommunityBoard) []model.CommunityBoard {
	children := make(map[int64][]model.CommunityBoard)
	var roots []model.CommunityBoard
	for _, b := range boards {
		if b.ParentID == nil {
			roots = append(roots, b)
		} else {
			children[*b.ParentID] = append(children[*b.ParentID], b)
		}
	}
	out := make([]model.CommunityBoard, 0, len(boards))
	for _, r := range roots {
		out = append(out, r)
		out = append(out, children[r.ID]...)
	}
	return out
}

func sameIDSet(boards []model.CommunityBoard, ids []int64) bool {
	if len(boards) != len(ids) {
		return false
	}
	want := make(map[int64]bool, len(boards))
	for _, b := range boards {
		want[b.ID] = true
	}
	for _, id := range ids {
		if !want[id] {
			return false
		}
		delete(want, id)
	}
	return true
}

func sameParent(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func optionalText(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

func tooLong(s *string, max int) bool {
	return s != nil && utf8.RuneCountInString(*s) > max
}

func boardWriteErr(err error) error {
	if isDuplicate(err) {
		return &ConflictError{Reason: "slug is already taken on this site"}
	}
	return err
}
