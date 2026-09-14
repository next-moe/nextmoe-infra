package model

// Source ids as seeded in catalog_source. Only the ones code reasons about by
// identity are named here.
const (
	SourceUser          int16 = 1
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
// wrongly exempting a source over-rejects and the surviving duplicate can be
// merged later, while wrongly trusting one merges two provably different works
// — and MergeService.Unmerge has no route and no CLI, so an executed merge
// cannot be undone in production.
//
// Two different reasons land a source here, kept in one list because the query
// is the same.
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
// 2,107 covered works (2.373%), against 0 of 65,232 for vndb, 0 of 21,476 for
// erogamescape, 0 of 10,746 for dmm and 1 of 33,267 for bangumi.
// ひぐらしのなく頃に解 alone holds three howlongtobeat ids.
var IdentityVetoExemptSourceIDs = []int16{
	SourceUser,          // first-party: manual curation, not an import source
	SourceCurated,       // first-party: curated/human lane (was galgame_wiki until wave 161)
	SourceUpscale,       // first-party: AI-upscaled cover derivation
	SourceDerived,       // first-party: machine inference over catalog facts
	SourceNextMoe,       // first-party: measurements aggregated from our own users
	SourceCensored,      // first-party: blurred stand-in derived from official art
	SourceHowLongToBeat, // does not deduplicate itself: one entry per platform/release
}
