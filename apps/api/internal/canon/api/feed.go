package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"api/internal/canon/ent"
	"api/internal/canon/ent/predicate"
	"api/internal/canon/ent/revision"
	"api/internal/canon/ent/work"
	"api/internal/canon/ent/worktitle"

	"github.com/danielgtaylor/huma/v2"
)

type ChangesInput struct {
	Cursor string `query:"cursor" doc:"next_cursor of the previous page; empty starts from the beginning."`
	Entity string `query:"entity" enum:"work,work_part,release,person,org," doc:"Only changes shown on this kind of entity."`
	Limit  int    `query:"limit" minimum:"1" maximum:"500" default:"100"`
}

type Change struct {
	Seq      string    `json:"seq"`
	Entity   string    `json:"entity" doc:"The public entity the change shows on: a title edit names its work."`
	EntityID string    `json:"entity_id"`
	Action   string    `json:"action" doc:"create, update, undo, remove, merged_into, absorbed, unmerge, takedown or restore."`
	At       time.Time `json:"at"`
}

type ChangeList struct {
	Items      []Change `json:"items"`
	NextCursor string   `json:"next_cursor" doc:"Always set; poll with it to follow the feed."`
}

type ChangesOutput struct{ Body ChangeList }

type SearchInput struct {
	Q      string `query:"q" minLength:"1" maxLength:"200"`
	Medium string `query:"medium" enum:"game,novel,manga,anime,audio,"`
	Limit  int    `query:"limit" minimum:"1" maximum:"50" default:"20"`
}

func (a *API) feed(h huma.API) {
	huma.Register(h, huma.Operation{OperationID: "listChanges", Method: http.MethodGet, Path: "/v3/catalog/changes",
		Summary: "Change feed", Description: "Every committed change in order, named by the public entity it shows on. Mirrors and indexes follow it and refetch the entity.",
		Tags: []string{"changes"}, Errors: []int{http.StatusBadRequest}},
		func(ctx context.Context, in *ChangesInput) (*ChangesOutput, error) {
			after, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			q := a.client.Revision.Query().Where(revision.IDGT(after), revision.RootEntityNEQ("")).Order(ent.Asc(revision.FieldID)).Limit(in.Limit)
			if in.Entity != "" {
				q = q.Where(revision.RootEntity(in.Entity))
			}
			rows, err := q.All(ctx)
			if err != nil {
				return nil, err
			}
			out := ChangeList{Items: []Change{}, NextCursor: encodeCursor(after)}
			for _, r := range rows {
				out.Items = append(out.Items, Change{Seq: id(r.ID), Entity: r.RootEntity, EntityID: id(r.RootID), Action: r.Action, At: r.CreatedAt})
				out.NextCursor = encodeCursor(r.ID)
			}
			return &ChangesOutput{Body: out}, nil
		})
	huma.Register(h, huma.Operation{OperationID: "searchWorks", Method: http.MethodGet, Path: "/v3/catalog/search",
		Summary: "Search works by title", Tags: []string{"works"}, Errors: []int{http.StatusBadRequest}},
		func(ctx context.Context, in *SearchInput) (*WorkListOutput, error) {
			q := strings.TrimSpace(in.Q)
			titles := a.client.WorkTitle.Query().Where(worktitle.TextContainsFold(q)).Limit(in.Limit * 5)
			ts, err := titles.All(ctx)
			if err != nil {
				return nil, err
			}
			rank := map[int64]int{}
			for _, t := range ts {
				score := 2
				switch {
				case strings.EqualFold(t.Text, q):
					score = 0
				case strings.HasPrefix(strings.ToLower(t.Text), strings.ToLower(q)):
					score = 1
				}
				if s, ok := rank[t.WorkID]; !ok || score < s {
					rank[t.WorkID] = score
				}
			}
			ids := make([]int64, 0, len(rank))
			for w := range rank {
				ids = append(ids, w)
			}
			preds := []predicate.Work{work.IDIn(ids...)}
			if in.Medium != "" {
				preds = append(preds, work.Medium(in.Medium))
			}
			ws, err := a.workQuery().Where(preds...).All(ctx)
			if err != nil {
				return nil, err
			}
			gone, err := a.goneSet(ctx, "work", workIDs(ws))
			if err != nil {
				return nil, err
			}
			visible := ws[:0]
			for _, w := range ws {
				if !gone[w.ID] {
					visible = append(visible, w)
				}
			}
			sortByRank(visible, rank)
			if len(visible) > in.Limit {
				visible = visible[:in.Limit]
			}
			items, err := a.shapeWorks(ctx, visible)
			if err != nil {
				return nil, err
			}
			return &WorkListOutput{Body: WorkList{Items: items}}, nil
		})
}

func sortByRank(ws []*ent.Work, rank map[int64]int) {
	for i := 1; i < len(ws); i++ {
		for j := i; j > 0 && (rank[ws[j].ID] < rank[ws[j-1].ID] || rank[ws[j].ID] == rank[ws[j-1].ID] && ws[j].ID < ws[j-1].ID); j-- {
			ws[j], ws[j-1] = ws[j-1], ws[j]
		}
	}
}
