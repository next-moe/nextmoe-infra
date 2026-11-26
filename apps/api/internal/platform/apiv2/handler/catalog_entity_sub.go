package handler

import (
	"context"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catmodel "api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

func (c *Catalog) CreditNameCredits(ctx context.Context, id int64, gate catsvc.EntityWorksGate, cursor string, limit int) (repr.List[repr.NameCredit], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.NameCredit]{}, err
	}
	if c == nil || c.Public == nil {
		return repr.List[repr.NameCredit]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.Name(ctx, id, true, gate, limit, offset)
	if err != nil {
		return repr.List[repr.NameCredit]{}, err
	}
	if !found {
		return repr.List[repr.NameCredit]{}, c.mergedOrNotFound(ctx, catmodel.EntityTypeCreditName, "credit_name", id)
	}
	items := make([]repr.NameCredit, 0, len(rec.Credits))
	for _, row := range rec.Credits {
		roles := make([]repr.NameCreditRole, 0, len(row.Roles))
		for _, r := range row.Roles {
			var ch *string
			if r.CharacterID > 0 {
				s := repr.ID(r.CharacterID)
				ch = &s
			}
			roles = append(roles, repr.NameCreditRole{
				RoleKey: r.RoleKey, RoleName: r.RoleName, CharacterID: ch,
				CharacterName: optString(r.Character),
			})
		}
		items = append(items, repr.NameCredit{Object: "name_credit", Work: workFromBrief(row.Work), Roles: roles})
	}
	out := repr.NewList(items, collect.EncodeOffset(derefInt(rec.NextOffset)))
	out.Total = rec.WorksTotal
	return out, nil
}

func (c *Catalog) CharacterAppearances(ctx context.Context, id int64, gate catsvc.EntityWorksGate, cursor string, limit int) (repr.List[repr.Appearance], error) {
	offset, limit, err := c.subPage(cursor, limit)
	if err != nil {
		return repr.List[repr.Appearance]{}, err
	}
	if c == nil || c.Public == nil {
		return repr.List[repr.Appearance]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rec, found, err := c.Public.Character(ctx, id, true, gate, 0, limit, offset)
	if err != nil {
		return repr.List[repr.Appearance]{}, err
	}
	if !found {
		return repr.List[repr.Appearance]{}, c.mergedOrNotFound(ctx, catmodel.EntityTypeCharacter, "character", id)
	}
	items := make([]repr.Appearance, 0, len(rec.Works))
	for _, row := range rec.Works {
		role, ok := repr.RosterRoleFromKey(row.Kind)
		if !ok {
			role = "unknown"
		}
		sp, ok := repr.Spoiler(row.Spoiler)
		if !ok {
			sp = "none"
		}
		voices := make([]repr.CreditName, 0, len(row.Voices))
		for _, v := range row.Voices {
			voices = append(voices, repr.CreditName{
				Object: "credit_name", ID: repr.ID(v.ID), DisplayName: v.DisplayName,
				Latin: optString(v.Latin), Lang: optString(v.Lang),
				Localized: localizedFrom(v.Localized), PersonID: optID(v.PersonID), Note: optString(v.Note),
			})
		}
		items = append(items, repr.Appearance{
			Object: "appearance", Work: workFromBrief(row.Work),
			RosterRole: role, Spoiler: sp, Identity: optString(row.Identity), Voices: voices,
		})
	}
	out := repr.NewList(items, collect.EncodeOffset(derefInt(rec.NextOffset)))
	out.Total = rec.WorksTotal
	return out, nil
}

func (c *Catalog) PersonCreditNames(ctx context.Context, id int64) (repr.List[repr.CreditName], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.CreditName]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	rows, found, err := c.Public.PersonNames(ctx, id)
	if err != nil {
		return repr.List[repr.CreditName]{}, err
	}
	if !found {
		return repr.List[repr.CreditName]{}, c.mergedOrNotFound(ctx, catmodel.EntityTypePerson, "person", id)
	}
	items := make([]repr.CreditName, 0, len(rows))
	for _, it := range rows {
		items = append(items, creditNameFromRow(it))
	}
	return finishList(items, nil, int64(len(items)), collect.Query{}, nil), nil
}
