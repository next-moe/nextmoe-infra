package handler

import (
	"api/internal/platform/community/dto"
)

// hydratePostReactions fills reaction_count (and viewer_reacted when the caller
// named a viewer) on a page of posts. Two queries per page, keyed on the ids
// already in hand — never one per post.
func (s *Server) hydratePostReactions(viewerID int64, posts []dto.PostView) error {
	if len(posts) == 0 {
		return nil
	}
	ids := make([]int64, len(posts))
	for i := range posts {
		ids[i] = posts[i].ID
	}
	counts, mine, err := s.reactionState(viewerID, ids)
	if err != nil {
		return err
	}
	for i := range posts {
		posts[i].ReactionCount = counts[posts[i].ID]
		posts[i].ViewerReacted = mine[posts[i].ID]
	}
	return nil
}

func (s *Server) hydrateAuthorPostReactions(viewerID int64, rows []dto.AuthorPostView) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int64, len(rows))
	for i := range rows {
		ids[i] = rows[i].Post.ID
	}
	counts, mine, err := s.reactionState(viewerID, ids)
	if err != nil {
		return err
	}
	for i := range rows {
		rows[i].Post.ReactionCount = counts[rows[i].Post.ID]
		rows[i].Post.ViewerReacted = mine[rows[i].Post.ID]
	}
	return nil
}

func (s *Server) reactionState(viewerID int64, ids []int64) (map[int64]int32, map[int64]bool, error) {
	counts, err := s.reactions.Counts(ids)
	if err != nil {
		return nil, nil, err
	}
	mine, err := s.reactions.ReactedBy(viewerID, ids)
	if err != nil {
		return nil, nil, err
	}
	return counts, mine, nil
}
