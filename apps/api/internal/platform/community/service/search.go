package service

import (
	"strings"
	"unicode/utf8"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

// One character matches nearly every post and extracts no trigram, so the index
// cannot help and the scan is the whole corpus; two is the shortest query a
// Chinese reader actually types.
const (
	searchMinRunes = 2
	searchMaxRunes = 100
)

type SearchService struct {
	posts   *repository.PostRepository
	threads *repository.ThreadRepository
}

func NewSearchService(db *gorm.DB) *SearchService {
	return &SearchService{posts: repository.NewPostRepository(db), threads: repository.NewThreadRepository(db)}
}

func (s *SearchService) Posts(q repository.SearchQuery) ([]repository.AuthorPostRow, error) {
	if err := normalizeSearch(&q); err != nil {
		return nil, err
	}
	return s.posts.SearchPosts(q)
}

func (s *SearchService) Threads(q repository.SearchQuery) ([]model.CommunityThread, error) {
	if err := normalizeSearch(&q); err != nil {
		return nil, err
	}
	return s.threads.SearchThreads(q)
}

func normalizeSearch(q *repository.SearchQuery) error {
	q.Q = strings.TrimSpace(q.Q)
	if n := utf8.RuneCountInString(q.Q); n < searchMinRunes || n > searchMaxRunes {
		return ErrInvalidSearchQuery
	}
	q.Limit = clampLimit(q.Limit)
	return nil
}
