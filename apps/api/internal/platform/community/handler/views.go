package handler

import (
	"encoding/base64"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/datatypes"
)

func toThreadView(t *model.CommunityThread) dto.ThreadView {
	return dto.ThreadView{
		ID: t.ID, Site: t.Site, Kind: t.Kind, AnchorKind: t.AnchorKind, AnchorID: t.AnchorID,
		Title: t.Title, HeaderImageHashes: headerImageHashes(t.HeaderImageHashes),
		ContentRating: t.ContentRating, Status: t.Status,
		FbStatus: t.FbStatus, FbResponse: t.FbResponse, AnswerPostID: t.AnswerPostID, MergedIntoID: t.MergedIntoID,
		PostsCount: t.PostsCount, ParticipantsCount: t.ParticipantsCount, HighestPostNumber: t.HighestPostNumber,
		LastPostedAt: t.LastPostedAt, CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt,
	}
}

func headerImageHashes(raw datatypes.JSON) []string {
	if len(raw) == 0 {
		return nil
	}
	var hashes []string
	if err := json.Unmarshal(raw, &hashes); err != nil {
		return nil
	}
	return hashes
}

func toPostView(p *model.CommunityPost) dto.PostView {
	return dto.PostView{
		ID: p.ID, ThreadID: p.ThreadID, PostNumber: p.PostNumber,
		RootPostID: p.RootPostID, ReplyToPostID: p.ReplyToPostID, TargetUserID: p.TargetUserID,
		AuthorID: p.AuthorID, ContentRaw: p.ContentRaw, ContentHTML: p.ContentHTML,
		ContentRating: p.ContentRating, Status: p.Status,
		EditedAt: p.EditedAt, EditedByModerator: p.EditedByModerator, CreatedAt: p.CreatedAt,
	}
}

func toPostViews(posts []model.CommunityPost) []dto.PostView {
	out := make([]dto.PostView, len(posts))
	for i := range posts {
		out[i] = toPostView(&posts[i])
	}
	return out
}

func toThreadViews(threads []model.CommunityThread) []dto.ThreadView {
	out := make([]dto.ThreadView, len(threads))
	for i := range threads {
		out[i] = toThreadView(&threads[i])
	}
	return out
}

func toThreadViewsWithOpening(threads []model.CommunityThread, metas map[int64]repository.OpeningPostMeta) []dto.ThreadView {
	out := make([]dto.ThreadView, len(threads))
	for i := range threads {
		v := toThreadView(&threads[i])
		if m, ok := metas[threads[i].ID]; ok {
			status, author := m.Status, m.AuthorID
			v.OpeningStatus = &status
			v.OpeningAuthorID = &author
		}
		out[i] = v
	}
	return out
}

func toTrustView(t *model.CommunityTrust) dto.TrustView {
	return dto.TrustView{
		UserID: t.UserID, Level: t.Level,
		TopicsEntered: t.TopicsEntered, PostsRead: t.PostsRead, ReadTimeS: t.ReadTimeS, DaysVisited: t.DaysVisited,
		LikesGiven: t.LikesGiven, LikesReceived: t.LikesReceived,
		FlagsAgreed: t.FlagsAgreed, FlagsDisagreed: t.FlagsDisagreed,
		FirstPostsHeldRemaining: t.FirstPostsHeldRemaining, GrantedBoost: t.GrantedBoost,
	}
}

func toReviewItemView(it *repository.ReviewItemRow) dto.ReviewItemView {
	v := dto.ReviewItemView{
		ID: it.ID, PostID: it.PostID, ThreadID: it.ThreadID, AuthorID: it.AuthorID,
		Source: it.Source, Status: it.Status, DecidedBy: it.DecidedBy,
	}
	if it.Site != nil {
		v.Site = *it.Site
	}
	return v
}

func toReviewItemViews(items []repository.ReviewItemRow) []dto.ReviewItemView {
	out := make([]dto.ReviewItemView, len(items))
	for i := range items {
		out[i] = toReviewItemView(&items[i])
	}
	return out
}

// A thread cursor carries the sort key the page ended on. The activity shape is
// two parts ("<nanos>|n:<id>") and predates the other sorts, so it stays
// tagless — cursors minted before this face gained `sort` keep working.
func encodeThreadCursor(t *model.CommunityThread, sort repository.ThreadSort) string {
	var head string
	switch sort {
	case repository.ThreadSortCreated:
		head = "c:" + strconv.FormatInt(t.CreatedAt.UnixNano(), 10)
	case repository.ThreadSortPosts:
		head = "p:" + strconv.FormatInt(int64(t.PostsCount), 10)
	default:
		head = "n"
		if t.LastPostedAt != nil {
			head = strconv.FormatInt(t.LastPostedAt.UnixNano(), 10)
		}
	}
	return base64.RawURLEncoding.EncodeToString([]byte(head + ":" + strconv.FormatInt(t.ID, 10)))
}

func decodeThreadCursor(s string) (repository.ThreadCursor, error) {
	if s == "" {
		return repository.ThreadCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return repository.ThreadCursor{}, err
	}
	parts := strings.Split(string(raw), ":")
	switch len(parts) {
	case 2:
		return decodeActivityCursor(parts[0], parts[1])
	case 3:
		return decodeSortedCursor(parts[0], parts[1], parts[2])
	default:
		return repository.ThreadCursor{}, stderrors.New("bad cursor arity")
	}
}

func decodeActivityCursor(head, idStr string) (repository.ThreadCursor, error) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return repository.ThreadCursor{}, err
	}
	if head == "n" {
		return repository.ThreadCursor{Sort: repository.ThreadSortActivity, LastPostedNull: true, ID: id}, nil
	}
	nano, err := strconv.ParseInt(head, 10, 64)
	if err != nil {
		return repository.ThreadCursor{}, err
	}
	return repository.ThreadCursor{Sort: repository.ThreadSortActivity, LastPosted: time.Unix(0, nano), ID: id}, nil
}

func decodeSortedCursor(tag, key, idStr string) (repository.ThreadCursor, error) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return repository.ThreadCursor{}, err
	}
	n, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		return repository.ThreadCursor{}, err
	}
	switch tag {
	case "c":
		return repository.ThreadCursor{Sort: repository.ThreadSortCreated, Created: time.Unix(0, n), ID: id}, nil
	case "p":
		return repository.ThreadCursor{Sort: repository.ThreadSortPosts, PostsCount: int32(n), ID: id}, nil
	default:
		return repository.ThreadCursor{}, stderrors.New("unknown cursor sort")
	}
}

func parseThreadSort(raw string) (repository.ThreadSort, bool) {
	switch repository.ThreadSort(raw) {
	case "":
		return repository.ThreadSortActivity, true
	case repository.ThreadSortActivity:
		return repository.ThreadSortActivity, true
	case repository.ThreadSortCreated:
		return repository.ThreadSortCreated, true
	case repository.ThreadSortPosts:
		return repository.ThreadSortPosts, true
	default:
		return "", false
	}
}

func encodePostCursor(row *repository.AuthorPostRow) string {
	return base64.RawURLEncoding.EncodeToString([]byte(
		strconv.FormatInt(row.CreatedAt.UnixNano(), 10) + ":" + strconv.FormatInt(row.ID, 10)))
}

func decodePostCursor(s string) (repository.PostFeedCursor, error) {
	if s == "" {
		return repository.PostFeedCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return repository.PostFeedCursor{}, err
	}
	nanoStr, idStr, ok := strings.Cut(string(raw), ":")
	if !ok {
		return repository.PostFeedCursor{}, stderrors.New("bad cursor arity")
	}
	nano, err := strconv.ParseInt(nanoStr, 10, 64)
	if err != nil {
		return repository.PostFeedCursor{}, err
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return repository.PostFeedCursor{}, err
	}
	return repository.PostFeedCursor{CreatedAt: time.Unix(0, nano), ID: id}, nil
}

func postFeedPageCursor(rows []repository.AuthorPostRow, limit int) string {
	if len(rows) < limit || len(rows) == 0 {
		return ""
	}
	return encodePostCursor(&rows[len(rows)-1])
}

func postsPageCursor(posts []dto.PostView, limit int) string {
	if len(posts) < limit || len(posts) == 0 {
		return ""
	}
	return fmt.Sprintf("%d", posts[len(posts)-1].PostNumber)
}

func threadsPageCursor(threads []model.CommunityThread, sort repository.ThreadSort, limit int) string {
	if len(threads) < limit || len(threads) == 0 {
		return ""
	}
	return encodeThreadCursor(&threads[len(threads)-1], sort)
}
