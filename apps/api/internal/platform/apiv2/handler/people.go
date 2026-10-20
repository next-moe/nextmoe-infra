package handler

import (
	"context"
	"log/slog"

	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

// UsersOf resolves central account ids to display objects in one batch. Every
// id asked for comes back: an erased or removed account gets the placeholder.
type UsersOf func(ctx context.Context, ids []int64) (map[int64]repr.UserRef, error)

func peopleTransformer(cat *Catalog) huma.Transformer {
	return func(hctx huma.Context, _ string, v any) (any, error) {
		if cat == nil || cat.UsersOf == nil {
			return v, nil
		}
		var props []*repr.ProposalRecord
		var revs []*repr.Revision
		switch body := v.(type) {
		case repr.ProposalRecord:
			props = append(props, &body)
			v = &body
		case *repr.ProposalRecord:
			props = append(props, body)
		case repr.List[repr.ProposalRecord]:
			for i := range body.Items {
				props = append(props, &body.Items[i])
			}
			v = body
		case *repr.List[repr.ProposalRecord]:
			for i := range body.Items {
				props = append(props, &body.Items[i])
			}
		case repr.Revision:
			revs = append(revs, &body)
			v = &body
		case *repr.Revision:
			revs = append(revs, body)
		case repr.List[repr.Revision]:
			for i := range body.Items {
				revs = append(revs, &body.Items[i])
			}
			v = body
		case *repr.List[repr.Revision]:
			for i := range body.Items {
				revs = append(revs, &body.Items[i])
			}
		default:
			return v, nil
		}
		attachPeople(hctx.Context(), cat.UsersOf, props, revs)
		return v, nil
	}
}

func attachPeople(ctx context.Context, usersOf UsersOf, props []*repr.ProposalRecord, revs []*repr.Revision) {
	seen := map[int64]struct{}{}
	var ids []int64
	want := func(raw string) {
		if id, ok := repr.ParseID(raw); ok && id > 0 {
			if _, dup := seen[id]; !dup {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	wantPtr := func(raw *string) {
		if raw != nil {
			want(*raw)
		}
	}
	for _, p := range props {
		want(p.ProposerUID)
		wantPtr(p.DecidedByUID)
		if p.Amendments != nil {
			for _, a := range *p.Amendments {
				want(a.AmenderUID)
			}
		}
	}
	for _, r := range revs {
		want(r.ActorUID)
		wantPtr(r.AmenderUID)
	}
	if len(ids) == 0 {
		return
	}
	users, err := usersOf(ctx, ids)
	if err != nil {
		slog.WarnContext(ctx, "v2: account lookup for edit records failed; user objects stay null", "ids", len(ids), "err", err)
		return
	}
	ref := func(raw string) *repr.UserRef {
		id, ok := repr.ParseID(raw)
		if !ok {
			return nil
		}
		u, ok := users[id]
		if !ok {
			return nil
		}
		return &u
	}
	refPtr := func(raw *string) *repr.UserRef {
		if raw == nil {
			return nil
		}
		return ref(*raw)
	}
	for _, p := range props {
		p.Proposer = ref(p.ProposerUID)
		p.DecidedBy = refPtr(p.DecidedByUID)
		if p.Amendments != nil {
			for i := range *p.Amendments {
				(*p.Amendments)[i].Amender = ref((*p.Amendments)[i].AmenderUID)
			}
		}
	}
	for _, r := range revs {
		r.Actor = ref(r.ActorUID)
		r.Amender = refPtr(r.AmenderUID)
	}
}
