package handler

import (
	"context"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catsvc "api/internal/platform/catalog/service"
)

func (c *Catalog) ListCoverVotes(ctx context.Context) (repr.List[repr.CoverVote], error) {
	if c == nil || c.CoverVotes == nil {
		return repr.List[repr.CoverVote]{}, problem.New(problem.CodeServiceUnavailable, "", "", "cover votes are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.List[repr.CoverVote]{}, err
	}
	rows, lerr := c.CoverVotes.ListMine(ctx, uid)
	if lerr != nil {
		return repr.List[repr.CoverVote]{}, lerr
	}
	items := make([]repr.CoverVote, 0, len(rows))
	for _, r := range rows {
		items = append(items, repr.CoverVote{
			Object: "cover_vote", CoverID: repr.ID(r.CoverID), WorkID: repr.ID(r.WorkID), Vote: "up",
		})
	}
	return finishList(items, nil, int64(len(items)), collect.Query{}, nil), nil
}

func (c *Catalog) PutCoverVote(ctx context.Context, coverID int64, vote string) (repr.CoverVote, error) {
	if c == nil || c.CoverVotes == nil {
		return repr.CoverVote{}, problem.New(problem.CodeServiceUnavailable, "", "", "cover votes are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.CoverVote{}, err
	}
	if vote != "up" {
		p := problem.New(problem.CodeValidationFailed, "", "", "only vote=up is stored.")
		p.Errors = []problem.FieldError{{Pointer: "/vote", Reason: problem.ReasonUnknownValue, Detail: "allowed values: up",
			Params: &problem.FieldParams{Allowed: &[]string{"up"}}}}
		return repr.CoverVote{}, p
	}
	workID, werr := c.CoverVotes.WorkIDForCover(ctx, coverID)
	if werr != nil {
		return repr.CoverVote{}, werr
	}
	if workID == 0 {
		return repr.CoverVote{}, problem.New(problem.CodeNotFound, "", "", "No cover with this id.")
	}
	_, verr := c.CoverVotes.Vote(ctx, catsvc.CoverVoteParams{WorkID: workID, CoverID: coverID, ActorUID: uid})
	if verr != nil {
		return repr.CoverVote{}, coverVoteErr(verr)
	}
	return repr.CoverVote{Object: "cover_vote", CoverID: repr.ID(coverID), WorkID: repr.ID(workID), Vote: "up"}, nil
}

func (c *Catalog) DeleteCoverVote(ctx context.Context, coverID int64) error {
	if c == nil || c.CoverVotes == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "cover votes are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return err
	}
	workID, werr := c.CoverVotes.WorkIDForCover(ctx, coverID)
	if werr != nil {
		return werr
	}
	if workID == 0 {
		return problem.New(problem.CodeNotFound, "", "", "No cover with this id.")
	}
	return coverVoteErr(c.CoverVotes.Unvote(ctx, workID, uid))
}

func coverVoteErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case err == catsvc.ErrVoteWorkUnavailable, err == catsvc.ErrVoteCoverNotOnWork:
		return problem.New(problem.CodeNotFound, "", "", err.Error())
	case err == catsvc.ErrVoteActorRequired:
		return problem.New(problem.CodeUserIdentityRequired, "", "", err.Error())
	}
	return err
}
