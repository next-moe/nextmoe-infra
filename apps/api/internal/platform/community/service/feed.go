package service

import "api/internal/platform/community/repository"

func (s *PostService) SiteFeed(q repository.PostFeedQuery) ([]repository.AuthorPostRow, error) {
	q.Limit = clampLimit(q.Limit)
	return s.posts.ListSiteFeed(q)
}
