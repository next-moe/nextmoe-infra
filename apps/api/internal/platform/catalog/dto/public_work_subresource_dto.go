package dto

type PublicWorkCoversData struct {
	Items      []PublicCover `json:"items" doc:"Cover images of this work, in the same order and the same shape as works/{id}.covers[] — one row per stored image, width/height/thumbhash filled from the image service. Rows with no renderable bytes are dropped before paging, so a page is never short of its limit while rows remain"`
	NextOffset *int          `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page. ABSENT on the last page — this face knows the block's true size, so its absence means \"no more rows\", never \"maybe\""`
}

type PublicWorkScreenshotsData struct {
	Items      []PublicScreenshot `json:"items" doc:"Screenshots of this work, same order and shape as works/{id}.screenshots[]. Rows with no renderable bytes are dropped before paging"`
	NextOffset *int               `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page; absent on the last page"`
}

type PublicWorkTagsData struct {
	Items      []PublicTag `json:"items" doc:"Source tags carried by this work, same order and shape as works/{id}.tags[] (count DESC, name, source). work_count is the nsfw-aware population of works?tag_id=<canonical_id>, resolved for THIS page only"`
	NextOffset *int        `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page; absent on the last page"`
}

type PublicWorkCharactersData struct {
	Items      []PublicRosterCharacter `json:"items" doc:"The character roster, same order and shape as works/{id}.characters[] (main before secondary before appears, credit-only last). Rows carry spoiler for the CALLER to gate on — this face applies no spoiler ceiling, exactly like the parent block"`
	NextOffset *int                    `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page; absent on the last page"`
}

type PublicWorkCreditsData struct {
	Items      []PublicCreditGroup `json:"items" doc:"Credits grouped by role, same shape as works/{id}.credits[]. PAGING IS OVER CREDIT ROWS, not groups: limit/offset count individual credits, and a role whose credits straddle a page boundary appears in BOTH pages as its own group carrying that page's slice. Concatenating pages therefore means merging the groups that share a role_key"`
	NextOffset *int                `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page, counted in credit rows rather than groups; absent on the last page"`
}

type PublicWorkReleasesData struct {
	Items      []PublicRelease `json:"items" doc:"Release rows of this work, same order (id ASC) and shape as works/{id}.releases[], each with its exact refs[] and its own labels[]"`
	NextOffset *int            `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page; absent on the last page"`
}

type PublicWorkIntrosData struct {
	Items      []PublicIntro `json:"items" doc:"One synopsis per language, same election and shape as works/{id}.intros[] — a source-written row beats a machine-translated one for the same language"`
	NextOffset *int          `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page; absent on the last page"`
}

type PublicWorkRatingsData struct {
	Items      []PublicRating `json:"items" doc:"Per-source ratings, same order and shape as works/{id}.ratings[] — distribution and stats included, because this IS the work-detail projection of that block"`
	NextOffset *int           `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page; absent on the last page"`
}

type PublicWorkRelationsData struct {
	Items      []PublicRelation `json:"items" doc:"Related works, same order and shape as works/{id}.relations[] under include=relations — one edge rendered per direction. With nsfw absent or 0 an r18 relation end is dropped whole rather than emptied"`
	NextOffset *int             `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page, counted AFTER the r18 ends were dropped; absent on the last page"`
}

type PublicWorkSeriesData struct {
	Items      []PublicSeries `json:"items" doc:"Series this work belongs to, same order and shape as works/{id}.series[]; member_count is the series' full membership, not this page"`
	NextOffset *int           `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page; absent on the last page"`
}

type PublicWorkLinksData struct {
	Items      []PublicLink `json:"items" doc:"Outbound links of this work, same order and shape as works/{id}.links[]: its own upstream pages (is_identity=true) first, then related links. Sources whose storefront URL cannot be reconstructed from the stored code (dlsite, dmm) are absent by design — they stay reachable as anchors through refs[]"`
	NextOffset *int         `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page; absent on the last page"`
}

type PublicWorkEnginesData struct {
	Items      []PublicWorkEngine `json:"items" doc:"Engines this work is built with, same order and shape as works/{id}.engines[]; work_count is the nsfw-aware population of works?engine_id=<id>, resolved for THIS page only"`
	NextOffset *int               `json:"next_offset,omitempty" doc:"Pass back as offset= for the next page; absent on the last page"`
}
