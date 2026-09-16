package main

import (
	"sort"
	"time"

	"api/internal/platform/community/model"
)

// wall is one comment wall: a game page, or one patch resource under it.
//
// The split is the trap of this import. A game's comments and its resources'
// comments are ONE table on the source side, separated only by resource_id, and
// a run that ignored that would pile a resource's conversation onto the game
// page it hangs under.
type wall struct {
	GalgameID  int
	ResourceID *int
}

func (w wall) anchor() (int16, string) {
	if w.ResourceID != nil {
		return model.AnchorKindSiteResource, itoa(*w.ResourceID)
	}
	return model.AnchorKindSiteGame, itoa(w.GalgameID)
}

func (w wall) key() [2]int {
	if w.ResourceID != nil {
		return [2]int{1, *w.ResourceID}
	}
	return [2]int{0, w.GalgameID}
}

type plannedPost struct {
	OldID        int
	PostNumber   int32
	AuthorID     int64
	Content      string
	Status       int16
	EditedAt     *time.Time
	CreatedAt    time.Time
	ReplyToOldID *int
	RootOldID    *int
	TargetUserID *int64
}

type threadPlan struct {
	Wall              wall
	Posts             []plannedPost
	PostsCount        int32
	ParticipantsCount int32
	HighestPostNumber int32
	FirstAuthorID     int64
	CreatedAt         time.Time
	LastPostedAt      time.Time

	Dangling int
	Hidden   int
	OverLen  int
}

func groupByWall(rows []srcComment) (map[[2]int][]srcComment, []wall) {
	byWall := make(map[[2]int][]srcComment)
	walls := make(map[[2]int]wall)
	for i := range rows {
		w := wall{GalgameID: rows[i].GalgameID, ResourceID: rows[i].ResourceID}
		byWall[w.key()] = append(byWall[w.key()], rows[i])
		walls[w.key()] = w
	}
	keys := make([][2]int, 0, len(walls))
	for k := range walls {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	ordered := make([]wall, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, walls[k])
	}
	return byWall, ordered
}

// planWall numbers a wall's comments 1..N in (created, id) order.
//
// The order is load-bearing twice over: post_number is the thread's cursor, and
// a parent must be numbered before its child so the child's root_post_id can be
// resolved to an id that already exists.
func planWall(w wall, comments []srcComment) threadPlan {
	sorted := make([]srcComment, len(comments))
	copy(sorted, comments)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].Created.Equal(sorted[j].Created) {
			return sorted[i].Created.Before(sorted[j].Created)
		}
		return sorted[i].ID < sorted[j].ID
	})

	present := make(map[int]bool, len(sorted))
	authorOf := make(map[int]int64, len(sorted))
	for i := range sorted {
		present[sorted[i].ID] = true
		authorOf[sorted[i].ID] = int64(sorted[i].UserID)
	}

	plan := threadPlan{Wall: w, Posts: make([]plannedPost, 0, len(sorted))}
	rootOf := make(map[int]*int, len(sorted))
	participants := make(map[int64]bool, len(sorted))

	var num int32
	for i := range sorted {
		c := sorted[i]
		num++
		p := plannedPost{
			OldID:      c.ID,
			PostNumber: num,
			AuthorID:   int64(c.UserID),
			Content:    c.Content,
			Status:     model.PostStatusVisible,
			EditedAt:   c.EditedAt,
			CreatedAt:  c.Created,
		}
		// Every production row is status 0 today. A row that is not stays out of
		// sight rather than out of the import: hiding a comment is reversible,
		// dropping one is not.
		if c.Status != 0 {
			p.Status = model.PostStatusHidden
			plan.Hidden++
		}
		if c.ParentID != nil {
			if present[*c.ParentID] {
				pid := *c.ParentID
				p.ReplyToOldID = &pid
				if rootOf[pid] != nil {
					p.RootOldID = rootOf[pid]
				} else {
					p.RootOldID = &pid
				}
				// moyu addresses the parent's author, which is what its own write
				// path fills in today.
				tgt := authorOf[pid]
				p.TargetUserID = &tgt
			} else {
				plan.Dangling++
			}
		}
		rootOf[c.ID] = p.RootOldID
		if len([]rune(c.Content)) > maxRunes {
			plan.OverLen++
		}
		participants[p.AuthorID] = true
		plan.Posts = append(plan.Posts, p)
	}

	plan.PostsCount = num
	plan.HighestPostNumber = num
	plan.ParticipantsCount = int32(len(participants))
	if len(plan.Posts) > 0 {
		plan.FirstAuthorID = plan.Posts[0].AuthorID
		plan.CreatedAt = plan.Posts[0].CreatedAt
		plan.LastPostedAt = plan.Posts[len(plan.Posts)-1].CreatedAt
	}
	return plan
}
