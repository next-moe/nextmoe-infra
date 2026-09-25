package handler

import (
	"fmt"

	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/dto"
	catsvc "api/internal/platform/catalog/service"
)

func companyFromListItem(it dto.PublicLabelListItem, include []string, logoURL string) repr.Company {
	kind := it.Kind
	if _, ok := repr.CompanyKindFromKey(kind); !ok {
		kind = "group"
	}
	out := repr.Company{
		Object: "company", ID: repr.ID(it.ID), DisplayName: it.DisplayName,
		Latin: optString(it.Latin), Lang: optString(it.Lang), Localized: localizedFrom(it.Localized),
		CompanyKind: kind, WorkCount: it.WorkCount,
	}
	for _, t := range include {
		switch t {
		case "aliases":
			out.Aliases = ptrSlice(entityNamesFrom(it.Aliases))
		case "logo":
			if logoURL != "" {
				out.Logo = imageFromPublicMeta(logoURL, it.LogoMeta, "")
			}
		}
	}
	return out
}

func companyFromDetail(rec dto.PublicLabel, include []string, logoURL string) repr.Company {
	kind := rec.Kind
	if _, ok := repr.CompanyKindFromKey(kind); !ok {
		kind = "group"
	}
	out := repr.Company{
		Object: "company", ID: repr.ID(rec.ID), DisplayName: rec.DisplayName,
		Latin: optString(rec.Latin), Lang: optString(rec.Lang), Localized: localizedFrom(rec.Localized),
		CompanyKind: kind, WorkCount: rec.WorkCount,
	}
	for _, t := range include {
		switch t {
		case "aliases":
			out.Aliases = ptrSlice(entityNamesFrom(rec.Aliases))
		case "logo":
			if logoURL != "" {
				out.Logo = imageFromPublicMeta(logoURL, rec.LogoMeta, "")
			}
		case "intros":
			out.Intros = ptrSlice(introsFrom(rec.Intros))
		case "links":
			out.Links = ptrSlice(labelLinksFrom(rec.Links))
		}
	}
	return out
}

func entityNamesFrom(in []dto.PublicAlias) []repr.EntityName {
	out := make([]repr.EntityName, 0, len(in))
	for _, a := range in {
		kind := a.Kind
		if kind != "translation" {
			kind = "spelling_variant"
		}
		out = append(out, repr.EntityName{Lang: a.Lang, Value: a.Value, AliasKind: kind, IsMachine: a.Machine})
	}
	return out
}

func labelLinksFrom(in []dto.PublicLabelLink) []repr.WorkLink {
	out := make([]repr.WorkLink, 0, len(in))
	for _, l := range in {
		out = append(out, repr.WorkLink{Source: l.Source, URL: l.URL})
	}
	return out
}

func tagFromListItem(it dto.PublicTagListItem) repr.Tag {
	return repr.Tag{
		Object: "tag", ID: repr.ID(it.ID), DisplayName: it.Name,
		Tier: it.Tier, TagKind: it.Kind, WorkCount: it.WorkCount, IsSexual: it.Sexual,
	}
}

func engineFromListItem(it dto.PublicEngineListItem) repr.Engine {
	aliases := it.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	return repr.Engine{
		Object: "engine", ID: repr.ID(it.ID), DisplayName: it.Name, WorkCount: it.WorkCount,
		Description: it.Description, Aliases: aliases,
	}
}

// roleFromListItem mirrors the display-name chain credit groups use
// (public_work_blocks firstNonEmptyPub): name_cn, then name_ja, then key.
func roleFromListItem(it dto.PublicRoleListItem) repr.Role {
	display := it.NameCN
	if display == "" {
		display = it.NameJA
	}
	if display == "" {
		display = it.Key
	}
	localized := map[string]repr.LocalizedText{}
	if it.NameCN != "" {
		localized["zh-Hans"] = repr.LocalizedText{Value: it.NameCN}
	}
	if it.NameJA != "" {
		localized["ja"] = repr.LocalizedText{Value: it.NameJA}
	}
	if it.NameEN != "" {
		localized["en"] = repr.LocalizedText{Value: it.NameEN}
	}
	return repr.Role{
		Object: "role", ID: repr.ID(it.ID), Key: it.Key, Category: it.Category,
		DisplayName: display, Localized: localized, Deprecated: it.Deprecated,
	}
}

func tagFromDetail(rec dto.PublicTagDetail, include []string) repr.Tag {
	out := repr.Tag{
		Object: "tag", ID: repr.ID(rec.ID), DisplayName: rec.Name,
		Tier: rec.Tier, TagKind: rec.Kind, WorkCount: rec.WorkCount, IsSexual: rec.Sexual,
	}
	for _, t := range include {
		if t == "intros" {
			out.Intros = ptrSlice(introsFrom(rec.Intros))
		}
	}
	return out
}

func seriesFromDetail(rec dto.PublicSeriesDetail, include []string) repr.Series {
	out := repr.Series{
		Object: "series", ID: repr.ID(rec.ID), DisplayName: rec.DisplayName,
		WorkCount: rec.WorkCount, HasNSFW: rec.HasNSFW,
	}
	for _, t := range include {
		switch t {
		case "intros":
			out.Intros = ptrSlice(seriesIntrosFrom(rec.Intros))
		case "refs":
			out.Refs = ptrSlice(refsFrom(rec.Refs))
		}
	}
	return out
}

func seriesFromListItem(it dto.PublicSeriesListItem) repr.Series {
	return repr.Series{
		Object: "series", ID: repr.ID(it.ID), DisplayName: it.DisplayName,
		WorkCount: it.WorkCount, HasNSFW: it.HasNSFW,
	}
}

func seriesIntrosFrom(in []dto.PublicSeriesIntro) []repr.Intro {
	out := make([]repr.Intro, 0, len(in))
	for _, s := range in {
		out = append(out, repr.Intro{Lang: s.Lang, Value: s.Intro, Source: s.Source})
	}
	return out
}

func characterFromRow(it catsvc.EntityListRow, include []string) repr.Character {
	out := repr.Character{
		Object: "character", ID: repr.ID(it.ID), DisplayName: it.DisplayName,
		Latin: it.Latin, Lang: optString(it.Lang), Localized: localizedFrom(it.Localized),
	}
	if it.Attrs != nil {
		attachCharacterAttrs(&out, *it.Attrs)
	}
	for _, t := range include {
		switch t {
		case "image":
			if it.Image != "" {
				out.Image = imageFromPublicMeta(it.Image, it.ImageMeta, "")
			}
		case "figure":
			if it.Figure != "" {
				out.Figure = imageFromPublicMeta(it.Figure, it.FigureMeta, "")
			}
		case "traits":
			out.Traits = ptrSlice(characterTraitsFrom(it.Traits))
		case "aliases":
			out.Aliases = ptrSlice(entityNamesFrom(it.Aliases))
		case "intros":
			out.Intros = ptrSlice(introsFrom(it.Intros))
		case "refs":
			out.Refs = ptrSlice(refsFrom(it.Refs))
		}
	}
	out.MatchedTraitIDs = it.MatchedTraitIDs
	out.WorkCount = it.WorkCount
	return out
}

func creditNameFromRow(it catsvc.EntityListRow) repr.CreditName {
	var personID *string
	if it.PersonID != nil && *it.PersonID > 0 {
		s := repr.ID(*it.PersonID)
		personID = &s
	}
	return repr.CreditName{
		Object: "credit_name", ID: repr.ID(it.ID), DisplayName: it.DisplayName,
		Latin: it.Latin, Lang: optString(it.Lang), Localized: localizedFrom(it.Localized),
		PersonID: personID,
	}
}

func personFromRow(it catsvc.EntityListRow) repr.Person {
	var primary *string
	if it.PrimaryCreditNameID != nil && *it.PrimaryCreditNameID > 0 {
		s := repr.ID(*it.PrimaryCreditNameID)
		primary = &s
	}
	g, _ := repr.Gender(it.Gender)
	return repr.Person{
		Object: "person", ID: repr.ID(it.ID), DisplayName: it.DisplayName,
		PrimaryCreditNameID: primary, Gender: g,
	}
}

func traitFromRow(it catsvc.EntityListRow, include []string) repr.Trait {
	parents := make([]repr.TraitRef, 0, len(it.Parents))
	for _, p := range it.Parents {
		parents = append(parents, traitRefFrom(p))
	}
	out := repr.Trait{
		Object: "trait", ID: repr.ID(it.ID), DisplayName: it.DisplayName,
		NameZh: it.NameZh, VndbTID: it.VndbTID, IsSexual: it.Sexual,
		Localized: localizedFrom(it.Localized), Parents: parents,
		ChildCount: it.ChildCount, RootOrder: it.RootOrder,
		IsSearchable: it.Searchable, IsApplicable: it.Applicable,
	}
	out.GroupID, out.Group, out.GroupLocalized = traitGroupFrom(it.Group)
	for _, t := range include {
		switch t {
		case "aliases":
			out.Aliases = ptrSlice(it.TraitAliases)
		case "description":
			d := ""
			if it.TraitDescription != nil {
				d = *it.TraitDescription
			}
			out.Description = &d
		}
	}
	return out
}

func traitGroupFrom(g *catsvc.TraitRefRow) (*string, *string, map[string]repr.LocalizedText) {
	if g == nil {
		return nil, nil, map[string]repr.LocalizedText{}
	}
	id, name := repr.ID(g.ID), g.DisplayName
	return &id, &name, localizedFrom(g.Localized)
}

func traitRefFrom(r catsvc.TraitRefRow) repr.TraitRef {
	return repr.TraitRef{
		Object: "trait", ID: repr.ID(r.ID), DisplayName: r.DisplayName,
		Localized: localizedFrom(r.Localized),
	}
}

func attachCharacterBlocks(out *repr.Character, rec dto.PublicCharacter, include []string) {
	for _, t := range include {
		switch t {
		case "image":
			if rec.Image != "" {
				out.Image = imageFromPublicMeta(rec.Image, rec.ImageMeta, "")
			}
		case "figure":
			if rec.Figure != "" {
				out.Figure = imageFromPublicMeta(rec.Figure, rec.FigureMeta, "")
			}
		case "traits":
			out.Traits = ptrSlice(characterTraitsFrom(rec.Traits))
		case "aliases":
			out.Aliases = ptrSlice(entityNamesFrom(rec.Aliases))
		case "intros":
			out.Intros = ptrSlice(introsFrom(rec.Intros))
		case "refs":
			out.Refs = ptrSlice(refsFrom(rec.Refs))
		}
	}
}

func characterTraitsFrom(in []dto.PublicCharacterTrait) []repr.CharacterTrait {
	out := make([]repr.CharacterTrait, 0, len(in))
	for _, tr := range in {
		sp, ok := repr.Spoiler(tr.Spoiler)
		if !ok {
			sp = "none"
		}
		out = append(out, repr.CharacterTrait{
			Object: "character_trait", ID: repr.ID(tr.ID), DisplayName: tr.Name,
			Group: optString(tr.Group), Localized: localizedFrom(tr.Localized),
			GroupLocalized: localizedFrom(tr.GroupLocalized),
			Spoiler:        sp, IsSexual: tr.Sexual, IsLie: tr.Lie,
		})
	}
	return out
}

func characterWantsAttrs(include []string) bool {
	for _, t := range include {
		switch t {
		case "gender", "birthday", "height_cm", "weight_kg", "measurements", "blood_type", "instance_of_id":
			return true
		}
	}
	return false
}

func characterWantsWorkCount(include []string) bool {
	for _, t := range include {
		if t == "work_count" {
			return true
		}
	}
	return false
}

func attachCharacterAttrs(out *repr.Character, a catsvc.CharacterAttributes) {
	g, ok := repr.Gender(a.Gender)
	if ok {
		out.Gender = g
	}
	out.BloodType, _ = repr.BloodType(a.BloodType)
	out.HeightCm = intFromI16(a.HeightCm)
	out.WeightKg = intFromI16(a.WeightKg)
	if a.BirthdayMonth != nil && a.BirthdayDay != nil && *a.BirthdayMonth > 0 && *a.BirthdayDay > 0 {
		s := fmt.Sprintf("%02d-%02d", *a.BirthdayMonth, *a.BirthdayDay)
		out.Birthday = &s
	}
	if a.InstanceOf != nil && *a.InstanceOf > 0 {
		s := repr.ID(*a.InstanceOf)
		out.InstanceOfID = &s
	}
	m := repr.Measurements{
		BustCm: intFromI16(a.BustCm), WaistCm: intFromI16(a.WaistCm),
		HipCm: intFromI16(a.HipCm), Cup: a.Cup,
	}
	if m.BustCm != nil || m.WaistCm != nil || m.HipCm != nil || m.Cup != nil {
		out.Measurements = &m
	}
}

func intFromI16(v *int16) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}
