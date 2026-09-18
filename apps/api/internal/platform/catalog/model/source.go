package model

// Source ids as seeded in catalog_source. Only the ones code reasons about by
// identity are named here.
const (
	SourceUser          int16 = 1
	SourceBangumi       int16 = 3
	SourceErogameScape  int16 = 5
	SourceCurated       int16 = 12
	SourceUpscale       int16 = 13
	SourceDerived       int16 = 18
	SourceNextMoe       int16 = 19
	SourceHowLongToBeat int16 = 20
	SourceCensored      int16 = 21
)

// IdentityVetoExemptSourceIDs are the sources whose exact ids must never veto a
// merge. A source absent from this list is treated as an independent registry
// that deduplicates its own catalog, so two works holding different ids under
// it are provably different works. That default is deliberate and asymmetric:
// wrongly trusting a source over-rejects and the surviving duplicate can be
// merged later, while wrongly exempting one merges two provably different works
// — and MergeService.Unmerge has no route and no CLI, so an executed merge
// cannot be undone in production.
//
// Three different reasons land a source here, kept in one list because the
// query is the same.
//
// First-party — this platform mints the id itself, so two works holding
// different ids under it is evidence that OUR catalog carries a duplicate,
// exactly what a merge exists to fix. On 2026-09-14 six proposals were flagged
// as contradicting purely because the retired galgame wiki had listed the same
// game twice under two gids (source 12, matched_by 'wiki:gid'); five of the six
// pairs had byte-identical titles.
//
// Does not deduplicate itself — an external registry that lists one game more
// than once, so a disagreement carries no information. Works holding two exact
// ids from one source, measured over prod on 2026-09-14: howlongtobeat 50 of
// 2,107 covered works (2.373%), against 0 of 65,232 for vndb and 0 of 10,746
// for dmm. ひぐらしのなく頃に解 alone holds three howlongtobeat ids.
//
// Lists editions as separate records — ErogameScape and Bangumi, exempt for
// works only (IdentityVetoExemptSourceIDsFor). The 2026-09-14 figures "0 of
// 21,476 works hold two EG exacts" and "1 of 33,267 for bangumi" measured our
// own one-primary-per-work policy, not those registries. Cross-links from VNDB
// and EG itself name several EG ids for 1,113 works ([7対応版], Remaster,
// Nitro The Best, 【Android版】, DL版, reprice version are separate EG entries).
// On the night of 2026-09-18 the veto rejected 83 pairs; 55 had a same verdict
// and 52 of those conflicted only on EG ids a new anchoring lane had just
// written (恋姫†無双 1824 vs its DLsite-minted twin 211785; 加奈～いもうと～ vs
// 加奈 ～いもうと～　[7対応版]). Over all nights, pairs rejected with a conflict
// on EG ids alone: 114 same, 32 unsure; on Bangumi ids alone: 84 same, 69
// unsure (赤ノ反照 ×2, Ever17, Dies irae, 円環の柩 — Bangumi keeps separate
// subjects per release). VNDB merges editions into one VN; a VNDB id conflict
// stays a veto.
var IdentityVetoExemptSourceIDs = []int16{
	SourceUser,          // first-party: manual curation, not an import source
	SourceCurated,       // first-party: curated/human lane (was galgame_wiki until wave 161)
	SourceUpscale,       // first-party: AI-upscaled cover derivation
	SourceDerived,       // first-party: machine inference over catalog facts
	SourceNextMoe,       // first-party: measurements aggregated from our own users
	SourceCensored,      // first-party: blurred stand-in derived from official art
	SourceHowLongToBeat, // does not deduplicate itself: one entry per platform/release
}

// EditionSplittingSourceIDs are registries that list editions and re-releases
// of a game as separate records, so for works a disagreement between their ids
// does not veto a merge. Their person and label ids stay a veto. After a merge the target's own pre-merge exact stays
// exact and every other exact from that source becomes related (link_kind 2),
// not probable: every EG consumer reads exact refs only, and the catalog keeps
// one exact EG id per work, so demoting all conflicting exacts to probable
// would cut the merged work off from every EG lane. Bangumi lanes also read
// exact refs. Measured 2026-09-18: VNDB and EG cross-links name several EG ids
// for 1,113 works.
var EditionSplittingSourceIDs = []int16{
	SourceBangumi,
	SourceErogameScape,
}

func IdentityVetoExemptSourceIDsFor(entityType int16) []int16 {
	if entityType != EntityTypeWork {
		return IdentityVetoExemptSourceIDs
	}
	out := append([]int16{}, IdentityVetoExemptSourceIDs...)
	return append(out, EditionSplittingSourceIDs...)
}
