package handler

import (
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/dto"
)

func ptrSlice[T any](in []T) *[]T {
	if in == nil {
		in = []T{}
	}
	return &in
}

func titlesFrom(in []dto.PublicCatalogTitle) []repr.WorkTitle {
	out := make([]repr.WorkTitle, 0, len(in))
	for _, t := range in {
		kind, ok := repr.TitleKindFromKey(t.Kind)
		if !ok {
			kind = "alias"
		}
		out = append(out, repr.WorkTitle{
			Lang: t.Lang, Title: t.Title, Latin: optString(t.Latin), TitleKind: kind, IsMachine: t.Machine,
		})
	}
	return out
}

func refsFrom(in []dto.PublicCatalogRef) []repr.Ref {
	out := make([]repr.Ref, 0, len(in))
	for _, r := range in {
		out = append(out, repr.Ref{Source: r.Source, ExternalID: r.ExternalID})
	}
	return out
}

func introsFrom(in []dto.PublicIntro) []repr.Intro {
	out := make([]repr.Intro, 0, len(in))
	for _, t := range in {
		out = append(out, repr.Intro{Lang: t.Lang, Value: t.Intro, IsMachine: t.Machine, Source: t.Source})
	}
	return out
}

func coversFrom(in []dto.PublicCover) []repr.Cover {
	out := make([]repr.Cover, 0, len(in))
	for _, c := range in {
		if cov := coverFromPublic(c); cov != nil {
			out = append(out, *cov)
		}
	}
	return out
}

func coverFromPublic(c dto.PublicCover) *repr.Cover {
	img := imageFromURLMeta(c.URL, c.Source, c.Width, c.Height, c.Thumbhash, &c.Sexual)
	if img == nil {
		return nil
	}
	id := repr.ID(c.ID)
	if id == "" {
		return nil
	}
	return &repr.Cover{
		ID: id, VoteCount: c.VoteCount, PortraitPinned: c.PortraitPinned, Kind: c.Kind,
		URL: img.URL, Hash: img.Hash, Width: img.Width, Height: img.Height,
		Thumbhash: img.Thumbhash, Sexual: img.Sexual, Violence: img.Violence, Source: img.Source,
	}
}

func screenshotsFrom(in []dto.PublicScreenshot) []repr.Screenshot {
	out := make([]repr.Screenshot, 0, len(in))
	for _, s := range in {
		img := imageFromURLMeta(s.URL, s.Source, s.Width, s.Height, s.Thumbhash, &s.Sexual)
		if img == nil {
			continue
		}
		out = append(out, repr.Screenshot{
			URL: img.URL, Hash: img.Hash, Width: img.Width, Height: img.Height,
			Thumbhash: img.Thumbhash, Sexual: img.Sexual, Violence: img.Violence, Source: img.Source,
			Caption: s.Caption,
		})
	}
	return out
}

func workTagsFrom(in []dto.PublicTag) []repr.WorkTag {
	out := make([]repr.WorkTag, 0, len(in))
	for _, t := range in {
		sp, ok := repr.Spoiler(t.Spoiler)
		if !ok {
			sp = "none"
		}
		wt := repr.WorkTag{DisplayName: t.Name, Source: t.Source, Spoiler: sp, IsSexual: t.Sexual}
		if t.CanonicalID > 0 {
			id := repr.ID(t.CanonicalID)
			wt.ID = &id
			if t.Tier != "" {
				tier := t.Tier
				wt.Tier = &tier
			}
			if t.Kind != "" {
				k := t.Kind
				wt.TagKind = &k
			}
			wt.WorkCount = t.WorkCount
		}
		out = append(out, wt)
	}
	return out
}

func workCharactersFrom(in []dto.PublicRosterCharacter) []repr.WorkCharacter {
	out := make([]repr.WorkCharacter, 0, len(in))
	for _, ch := range in {
		role, ok := repr.RosterRoleFromKey(ch.Kind)
		if !ok {
			role = "unknown"
		}
		sp, ok := repr.Spoiler(ch.Spoiler)
		if !ok {
			sp = "none"
		}
		item := repr.WorkCharacter{
			Object: "character", ID: repr.ID(ch.ID), DisplayName: ch.DisplayName,
			Latin: optString(ch.Latin), Localized: localizedFrom(ch.Localized),
			RosterRole: role, Spoiler: sp,
			Identity: optString(ch.Identity), Voices: rosterVoicesFrom(ch.Voices),
		}
		if ch.Image != "" {
			item.Image = imageFromPublicMeta(ch.Image, ch.ImageMeta, "")
		}
		if ch.Figure != "" {
			item.Figure = imageFromPublicMeta(ch.Figure, ch.FigureMeta, "")
		}
		out = append(out, item)
	}
	return out
}

func rosterVoicesFrom(in []dto.PublicRosterVoice) []repr.CreditName {
	out := make([]repr.CreditName, 0, len(in))
	for _, v := range in {
		out = append(out, repr.CreditName{
			Object: "credit_name", ID: repr.ID(v.ID), DisplayName: v.DisplayName,
			Latin: optString(v.Latin), Lang: optString(v.Lang),
			Localized: localizedFrom(v.Localized), PersonID: optID(v.PersonID),
		})
	}
	return out
}

func imageFromPublicMeta(url string, meta *dto.PublicImageMeta, source string) *repr.Image {
	if meta == nil {
		return imageFromURL(url, source)
	}
	return imageFromURLMeta(url, source, meta.Width, meta.Height, meta.Thumbhash, meta.Sexual)
}

func creditGroupsFrom(in []dto.PublicCreditGroup) []repr.CreditGroup {
	out := make([]repr.CreditGroup, 0, len(in))
	for _, g := range in {
		credits := make([]repr.CreditEntry, 0, len(g.Credits))
		for _, c := range g.Credits {
			var charID *string
			if c.CharacterID > 0 {
				s := repr.ID(c.CharacterID)
				charID = &s
			}
			credits = append(credits, repr.CreditEntry{
				Object: "credit_name", ID: repr.ID(c.ID), DisplayName: c.DisplayName,
				Latin: optString(c.Latin), Localized: localizedFrom(c.Localized), CharacterID: charID,
				Identity: c.Identity,
			})
		}
		out = append(out, repr.CreditGroup{RoleKey: g.RoleKey, RoleName: g.RoleName, Credits: credits})
	}
	return out
}

func releaseFromFeed(it dto.PublicReleaseFeedItem) repr.Release {
	kind, ok := repr.ReleaseKindFromKey(it.Kind)
	if !ok {
		kind = "default"
	}
	plats := it.Platforms
	if plats == nil {
		plats = []string{}
	}
	var workID *string
	if it.Work.ID > 0 {
		s := repr.ID(it.Work.ID)
		workID = &s
	}
	return repr.Release{
		Object: "release", ID: repr.ID(it.ID), WorkID: workID, ReleaseKind: kind, Date: it.Date,
		Title: optString(it.Title), Lang: it.Lang, Platform: it.Platform, Platforms: plats,
		Refs: refsFrom(it.Refs),
	}
}

func releasesFrom(in []dto.PublicRelease) []repr.Release {
	out := make([]repr.Release, 0, len(in))
	for _, r := range in {
		kind, ok := repr.ReleaseKindFromKey(r.Kind)
		if !ok {
			kind = "default"
		}
		plats := r.Platforms
		if plats == nil {
			plats = []string{}
		}
		out = append(out, repr.Release{
			Object: "release", ID: repr.ID(r.ID), ReleaseKind: kind, Date: r.Date,
			Title: optString(r.Title), Lang: r.Lang, Platform: r.Platform, Platforms: plats,
			Refs: refsFrom(r.Refs),
		})
	}
	return out
}

func relationsFrom(in []dto.PublicRelation) []repr.Relation {
	out := make([]repr.Relation, 0, len(in))
	for _, r := range in {
		out = append(out, repr.Relation{
			RelationType: r.RelationType, Phrase: r.Phrase,
			Work: workFromBrief(r.Work),
		})
	}
	return out
}

func workFromBrief(b dto.PublicWorkBrief) repr.Work {
	created := b.Created
	if created == "" {
		created = b.Updated
	}
	w, _ := repr.NewWork(
		b.ID, b.Medium, b.DisplayName, b.OLang, b.ContentRating, "unknown",
		created, b.Updated, optString(b.Latin), localizedFrom(b.Localized),
		nil, nil, nil, nil, claimFrom(b.ClaimedBy), b.ContentLimit,
	)
	if w.Object == "" {
		w.Object = "work"
		w.ID = repr.ID(b.ID)
		w.Medium = "galgame"
		w.DisplayName = b.DisplayName
		w.ContentRating = b.ContentRating
		w.OLang = b.OLang
		w.Localized = localizedFrom(b.Localized)
		w.Claim = claimFrom(b.ClaimedBy)
		w.ContentLimit = b.ContentLimit
		w.ReleaseStatus = "unknown"
		w.CreatedAt, w.UpdatedAt = created, b.Updated
	}
	return w
}

func workCompaniesFrom(in []dto.PublicWorkLabel, logoURL func(string) string) []repr.WorkCompany {
	out := make([]repr.WorkCompany, 0, len(in))
	for _, l := range in {
		ck, ok := repr.CompanyKindFromKey(l.LabelKind)
		if !ok {
			ck = "group"
		}
		role, ok := repr.AttributionRoleFromKey(l.Kind)
		if !ok {
			role = "brand"
		}
		item := repr.WorkCompany{
			Object: "company", ID: repr.ID(l.ID), DisplayName: l.DisplayName,
			Localized: localizedFrom(l.Localized), CompanyKind: ck, AttributionRole: role, WorkCount: l.WorkCount,
		}
		if l.LogoHash != "" && logoURL != nil {
			item.Logo = imageFromURL(logoURL(l.LogoHash), "")
		}
		out = append(out, item)
	}
	return out
}

func workEnginesFrom(in []dto.PublicWorkEngine) []repr.WorkEngineRef {
	out := make([]repr.WorkEngineRef, 0, len(in))
	for _, e := range in {
		out = append(out, repr.WorkEngineRef{Object: "engine", ID: repr.ID(e.ID), DisplayName: e.Name, WorkCount: e.WorkCount})
	}
	return out
}

func workLinksFrom(in []dto.PublicWorkLink) []repr.WorkLink {
	out := make([]repr.WorkLink, 0, len(in))
	for _, l := range in {
		out = append(out, repr.WorkLink{Source: l.Source, URL: l.URL})
	}
	return out
}

func ratingsFrom(in []dto.PublicRating) []repr.Rating {
	out := make([]repr.Rating, 0, len(in))
	for _, r := range in {
		item := repr.Rating{Source: r.Source, Score: r.Score, VoteCount: r.VoteCount, Rank: r.Rank}
		if len(r.Distribution) > 0 {
			buckets := make([]repr.RatingBucket, 0, len(r.Distribution))
			for _, b := range r.Distribution {
				buckets = append(buckets, repr.RatingBucket{Score: float64(b.Score), Count: b.Count})
			}
			item.Distribution = &buckets
		}
		if r.Stats != nil {
			item.Stats = &repr.RatingStats{
				Average: r.Stats.Average, Stdev: r.Stats.Stdev, Min: r.Stats.Min, Max: r.Stats.Max,
			}
		}
		out = append(out, item)
	}
	return out
}

func popularityFrom(in []dto.PublicPopularity) []repr.Popularity {
	out := make([]repr.Popularity, 0, len(in))
	for _, p := range in {
		out = append(out, repr.Popularity{Source: p.Source, Metric: p.Metric, Value: p.Value})
	}
	return out
}

func playtimesFrom(in []dto.PublicPlaytime) []repr.Playtime {
	out := make([]repr.Playtime, 0, len(in))
	for _, p := range in {
		out = append(out, repr.Playtime{Source: p.Source, Minutes: p.Minutes, VoteCount: p.VoteCount})
	}
	return out
}

func platformsFrom(in []dto.PublicPlatform) []repr.WorkPlatform {
	out := make([]repr.WorkPlatform, 0, len(in))
	for _, p := range in {
		out = append(out, repr.WorkPlatform{Platform: p.Platform, Source: p.Source})
	}
	return out
}

func workSeriesFrom(in []dto.PublicSeries) []repr.WorkSeriesRef {
	out := make([]repr.WorkSeriesRef, 0, len(in))
	for _, s := range in {
		out = append(out, repr.WorkSeriesRef{
			Object: "series", ID: repr.ID(s.ID), DisplayName: s.Name, Source: s.Source, MemberCount: s.MemberCount,
		})
	}
	return out
}
