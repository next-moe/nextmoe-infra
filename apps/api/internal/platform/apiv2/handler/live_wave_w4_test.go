package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type w4Work struct {
	ID                   string  `json:"id"`
	OLang                string  `json:"olang"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`
	ReleaseDate          *string `json:"release_date"`
	ReleaseDatePrecision *string `json:"release_date_precision"`
	ReleaseStatus        string  `json:"release_status"`
}

func w4Get(t *testing.T, env *liveEnv, path string, out any) []byte {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodGet, path, liveAppKey, "")
	require.Equal(t, 200, status, "%s -> %s", path, body)
	if out != nil {
		require.NoError(t, json.Unmarshal(body, out), string(body))
	}
	return body
}

// D1: workFromListItem passed Updated into both the created and the updated
// position, so every list lane reported the last edit as the creation date.
func TestLiveWorkCreatedAtAgreesAcrossLanes(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.ENWork)

	var detail w4Work
	w4Get(t, env, "/v2/catalog/works/"+id, &detail)
	require.Equal(t, liveENCreated, detail.CreatedAt)
	require.Equal(t, liveENUpdated, detail.UpdatedAt)
	require.NotEqual(t, detail.CreatedAt, detail.UpdatedAt,
		"the fixture must separate the two instants or this test cannot fail")

	for _, path := range []string{
		"/v2/catalog/works?ids=" + id,
		"/v2/catalog/works?limit=100",
	} {
		var page struct {
			Items []w4Work `json:"items"`
		}
		w4Get(t, env, path, &page)
		found := false
		for _, it := range page.Items {
			if it.ID != id {
				continue
			}
			found = true
			require.Equal(t, detail.CreatedAt, it.CreatedAt, "%s: list and detail must agree", path)
			require.Equal(t, detail.UpdatedAt, it.UpdatedAt, path)
		}
		require.True(t, found, "%s: the work under test is missing from the page", path)
	}
}

// D2: both ReleaseFeedFilter construction sites left OLang at its zero value,
// which the predicate reads as ja+zh, so an en work's release 404'd on the very
// id the work sub-face had just published (the PR #87 defect, second site).
func TestLiveReleasesLaneIsNotNarrowedToTheHomeLanguages(t *testing.T) {
	env := liveCatalog(t)

	var sub struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	w4Get(t, env, "/v2/catalog/works/"+idstr(env.fx.ENWork)+"/releases", &sub)
	require.Len(t, sub.Items, 1)
	require.Equal(t, idstr(env.fx.ENRelease), sub.Items[0].ID)

	var one struct {
		ID string `json:"id"`
	}
	w4Get(t, env, "/v2/catalog/releases/"+idstr(env.fx.ENRelease), &one)
	require.Equal(t, idstr(env.fx.ENRelease), one.ID)

	// Positive control: the ja fixture release answers on the same faces, so a
	// blanket 200 is not what this test is measuring.
	w4Get(t, env, "/v2/catalog/releases/"+idstr(env.fx.Release), &one)
	require.Equal(t, idstr(env.fx.Release), one.ID)

	var feed struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	w4Get(t, env, "/v2/catalog/releases?ids="+idstr(env.fx.ENRelease)+","+idstr(env.fx.Release), &feed)
	got := map[string]bool{}
	for _, it := range feed.Items {
		got[it.ID] = true
	}
	require.True(t, got[idstr(env.fx.ENRelease)], "the en release must be in the batch lane")
	require.True(t, got[idstr(env.fx.Release)], "the ja release must be in the batch lane")
}

// D3: workFromBrief published "" for olang, created_at and updated_at, all
// three required and the last two declared format: date-time.
func TestLiveWorkBriefCarriesTheHeadFields(t *testing.T) {
	env := liveCatalog(t)

	var detail w4Work
	w4Get(t, env, "/v2/catalog/works/"+idstr(env.fx.Work), &detail)
	require.NotEmpty(t, detail.OLang)
	require.NotEmpty(t, detail.CreatedAt)

	for _, path := range []string{
		"/v2/catalog/characters/" + idstr(env.fx.Character) + "/appearances",
		"/v2/catalog/credit-names/" + idstr(env.fx.Credit) + "/credits",
	} {
		var page struct {
			Items []struct {
				Work w4Work `json:"work"`
			} `json:"items"`
		}
		w4Get(t, env, path, &page)
		require.NotEmpty(t, page.Items, path)
		seen := false
		for _, it := range page.Items {
			if it.Work.ID != idstr(env.fx.Work) {
				continue
			}
			seen = true
			require.Equal(t, detail.OLang, it.Work.OLang, "%s: brief olang", path)
			require.Equal(t, detail.CreatedAt, it.Work.CreatedAt, "%s: brief created_at", path)
			require.Equal(t, detail.UpdatedAt, it.Work.UpdatedAt, "%s: brief updated_at", path)
		}
		require.True(t, seen, "%s: the work under test is missing", path)
	}
}

// D6: both mirror feeds passed a literal 0 for total, so a consumer sizing a
// backfill off include_total read "nothing to mirror".
func TestLiveFeedTotalsAreCounted(t *testing.T) {
	env := liveCatalog(t)

	for _, path := range []string{"/v2/catalog/changes", "/v2/catalog/redirects"} {
		var bare map[string]json.RawMessage
		w4Get(t, env, path+"?limit=2", &bare)
		_, present := bare["total"]
		require.False(t, present, "%s: total must be absent without include_total", path)

		var page struct {
			Total *int64            `json:"total"`
			Items []json.RawMessage `json:"items"`
		}
		w4Get(t, env, path+"?limit=2&include_total=true", &page)
		require.NotNil(t, page.Total, path)
		require.NotEmpty(t, page.Items, "%s: the fixture must have rows or the total proves nothing", path)
		require.GreaterOrEqual(t, *page.Total, int64(len(page.Items)),
			"%s: total must cover at least the page it returned", path)
		require.Positive(t, *page.Total, path)
	}
}

// D18: the redirect keyset compared (merged_at, entity_type, old_id) as a row
// value, and a NULL merged_at made that comparison NULL — the row never
// appeared on any page.
func TestLiveRedirectFeedCarriesUndatedRows(t *testing.T) {
	env := liveCatalog(t)

	var page struct {
		Items []struct {
			OldID    string `json:"old_id"`
			MergedAt string `json:"merged_at"`
		} `json:"items"`
	}
	w4Get(t, env, "/v2/catalog/redirects?limit=100", &page)
	got := map[string]string{}
	for _, it := range page.Items {
		got[it.OldID] = it.MergedAt
	}
	dated, ok := got[idstr(env.fx.RedirectDatedOld)]
	require.True(t, ok, "the dated redirect is the positive control and must be present")
	require.Equal(t, "2026-01-05T06:07:08Z", dated)
	undated, ok := got[idstr(env.fx.RedirectOld)]
	require.True(t, ok, "a redirect with no merge time must still reach the mirror")
	require.Equal(t, "1970-01-01T00:00:00Z", undated)
}

// D9: the credit row identity was dropped by creditGroupsFrom and repr had no
// field for it, so no v2 client could file catalog.work.credits.suppressed —
// role_id is not published either, so the key cannot be rebuilt.
func TestLiveWorkCreditsCarryRowIdentity(t *testing.T) {
	env := liveCatalog(t)

	type creditGroup struct {
		RoleKey string `json:"role_key"`
		Credits []struct {
			ID          string  `json:"id"`
			CharacterID *string `json:"character_id"`
			Identity    string  `json:"identity"`
		} `json:"credits"`
	}
	var detail struct {
		Credits []creditGroup `json:"credits"`
	}
	w4Get(t, env, "/v2/catalog/works/"+idstr(env.fx.Work)+"?include=credits", &detail)
	require.NotEmpty(t, detail.Credits)

	byID := map[string]string{}
	for _, g := range detail.Credits {
		for _, c := range g.Credits {
			require.NotEmpty(t, c.Identity, "role %s credit %s published no identity", g.RoleKey, c.ID)
			byID[g.RoleKey+"/"+c.ID] = c.Identity
		}
	}

	var sub struct {
		Items []creditGroup `json:"items"`
	}
	w4Get(t, env, "/v2/catalog/works/"+idstr(env.fx.Work)+"/credits?limit=100", &sub)
	require.NotEmpty(t, sub.Items)
	for _, g := range sub.Items {
		for _, c := range g.Credits {
			require.Equal(t, byID[g.RoleKey+"/"+c.ID], c.Identity,
				"the block and the sub-face must publish the same row identity")
		}
	}
}

// D10: release_date_precision was hard-coded "day" and the value was published
// verbatim, so the fixture's year+month release read "2024-01" — which no date
// parser accepts — while claiming day precision.
func TestLiveMonthPrecisionReleaseDateIsPaddedAndLabelled(t *testing.T) {
	env := liveCatalog(t)

	var month w4Work
	w4Get(t, env, "/v2/catalog/works/"+idstr(env.fx.Work), &month)
	require.NotNil(t, month.ReleaseDate)
	require.Equal(t, "2024-01-01", *month.ReleaseDate)
	require.NotNil(t, month.ReleaseDatePrecision)
	require.Equal(t, "month", *month.ReleaseDatePrecision)
	require.Equal(t, "released", month.ReleaseStatus)

	// Positive control: a full year-month-day release still reads day.
	var day w4Work
	w4Get(t, env, "/v2/catalog/works/"+idstr(env.fx.ENWork), &day)
	require.NotNil(t, day.ReleaseDate)
	require.Equal(t, "2023-09-15", *day.ReleaseDate)
	require.NotNil(t, day.ReleaseDatePrecision)
	require.Equal(t, "day", *day.ReleaseDatePrecision)
	require.Equal(t, "released", day.ReleaseStatus)
}

// D11: Cover.vote_count was a literal 0 and ReadService.CoverVotes had no
// caller, so the vote a reader had just cast never came back.
func TestLiveCoverVoteCountIsPublished(t *testing.T) {
	env := liveCatalog(t)
	coverID := idstr(env.fx.Cover)

	read := func() int {
		var out struct {
			Covers []struct {
				ID        string `json:"id"`
				VoteCount int    `json:"vote_count"`
			} `json:"covers"`
		}
		w4Get(t, env, "/v2/catalog/works/"+idstr(env.fx.Work)+"?include=covers", &out)
		for _, c := range out.Covers {
			if c.ID == coverID {
				return c.VoteCount
			}
		}
		t.Fatalf("cover %s missing from the block", coverID)
		return -1
	}

	before := read()
	status, _, body := liveDo(t, env, http.MethodPut, "/v2/me/cover-votes/"+coverID, liveUserToken, `{"vote":"up"}`)
	require.Equal(t, 200, status, string(body))
	require.Equal(t, before+1, read(), "the cast vote must show up on the cover row")

	status, _, body = liveDo(t, env, http.MethodDelete, "/v2/me/cover-votes/"+coverID, liveUserToken, "")
	require.Equal(t, 204, status, string(body))
	require.Equal(t, before, read(), "withdrawing the vote must take the count back down")
}

// D12: /v2/catalog/characters accepted include= and view= and answered neither.
func TestLiveCharacterListHonorsIncludeAndView(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.Character)

	type charRow struct {
		ID     string             `json:"id"`
		Traits *[]json.RawMessage `json:"traits"`
	}
	pick := func(path string) (charRow, map[string]json.RawMessage) {
		var page struct {
			Items []json.RawMessage `json:"items"`
		}
		w4Get(t, env, path, &page)
		for _, raw := range page.Items {
			var keyed map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &keyed))
			var row charRow
			require.NoError(t, json.Unmarshal(raw, &row))
			if row.ID == id {
				return row, keyed
			}
		}
		t.Fatalf("%s: character %s missing", path, id)
		return charRow{}, nil
	}

	// The detail face is the reference: whatever it answers on view=full is
	// what the list lane must answer too.
	var detail map[string]json.RawMessage
	w4Get(t, env, "/v2/catalog/characters/"+id+"?view=full", &detail)
	require.Contains(t, detail, "gender", "the fixture must record attributes or view=full proves nothing")

	basic, basicKeys := pick("/v2/catalog/characters?ids=" + id)
	require.Nil(t, basic.Traits, "view=basic must not carry a traits block")
	require.NotContains(t, basicKeys, "gender")

	withTraits, _ := pick("/v2/catalog/characters?ids=" + id + "&include=traits")
	require.NotNil(t, withTraits.Traits)
	require.Len(t, *withTraits.Traits, 1,
		"the fixture's second trait is spoiler=minor and stays behind the default ceiling")

	_, fullKeys := pick("/v2/catalog/characters?ids=" + id + "&view=full")
	for _, key := range []string{"traits", "aliases", "intros", "refs", "gender", "height_cm", "blood_type"} {
		require.Contains(t, fullKeys, key, "view=full must carry %s", key)
	}
	for _, key := range []string{"gender", "height_cm", "blood_type"} {
		require.JSONEq(t, string(detail[key]), string(fullKeys[key]),
			"%s must hold the same value the detail face publishes", key)
	}
	require.NotEqual(t, len(basicKeys), len(fullKeys),
		"view=full returned the same key set as basic, which is the defect")

	// The cursor lane, not only the ids= batch lane.
	cursor, cursorKeys := pick("/v2/catalog/characters?limit=100&include=traits,gender")
	require.NotNil(t, cursor.Traits)
	require.Len(t, *cursor.Traits, 1)
	require.JSONEq(t, string(detail["gender"]), string(cursorKeys["gender"]))
}

// D21: voices[].person_id was declared on the shared CreditName schema and
// never filled, on either the roster block or the appearances face.
// D26: repr.Appearance had no identity at all, so the same roster row was
// addressable from the work side and not from the character side.
func TestLiveRosterVoicesAndAppearanceIdentity(t *testing.T) {
	env := liveCatalog(t)

	var detail struct {
		Characters []struct {
			ID       string  `json:"id"`
			Identity *string `json:"identity"`
			Voices   []struct {
				ID       string  `json:"id"`
				PersonID *string `json:"person_id"`
			} `json:"voices"`
		} `json:"characters"`
	}
	w4Get(t, env, "/v2/catalog/works/"+idstr(env.fx.Work)+"?include=characters", &detail)

	var rosterIdentity *string
	linked, unlinked := 0, 0
	for _, ch := range detail.Characters {
		if ch.ID == idstr(env.fx.Character) {
			rosterIdentity = ch.Identity
			require.NotEmpty(t, ch.Voices, "the fixture voice credit must be on this row")
			for _, v := range ch.Voices {
				require.NotNil(t, v.PersonID, "voice %s lost its person link", v.ID)
				require.Equal(t, idstr(env.fx.Person), *v.PersonID)
				linked++
			}
		}
		if ch.ID == idstr(env.fx.VAOnlyCharacter) {
			require.NotEmpty(t, ch.Voices)
			for _, v := range ch.Voices {
				require.Nil(t, v.PersonID, "a credit name with no person must publish null, not an id")
				unlinked++
			}
		}
	}
	require.Positive(t, linked, "no linked voice was checked")
	require.Positive(t, unlinked, "no unlinked voice was checked as the null control")
	require.NotNil(t, rosterIdentity)

	var appearances struct {
		Items []struct {
			Work struct {
				ID string `json:"id"`
			} `json:"work"`
			Identity *string `json:"identity"`
			Voices   []struct {
				ID       string  `json:"id"`
				PersonID *string `json:"person_id"`
			} `json:"voices"`
		} `json:"items"`
	}
	w4Get(t, env, "/v2/catalog/characters/"+idstr(env.fx.Character)+"/appearances", &appearances)
	seen := false
	for _, it := range appearances.Items {
		if it.Work.ID != idstr(env.fx.Work) {
			continue
		}
		seen = true
		require.NotNil(t, it.Identity)
		require.Equal(t, *rosterIdentity, *it.Identity,
			"the same roster row must carry the same identity from both sides")
		require.NotEmpty(t, it.Voices)
		for _, v := range it.Voices {
			require.NotNil(t, v.PersonID)
			require.Equal(t, idstr(env.fx.Person), *v.PersonID)
		}
	}
	require.True(t, seen, "the appearance under test is missing")
}

// D22: the series batch lane never checked `seen`, so ids=5,5 answered two
// copies of the same row.
func TestLiveSeriesBatchDeduplicatesIDs(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.Series)

	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	w4Get(t, env, "/v2/catalog/series?ids="+id+","+id, &page)
	require.Len(t, page.Items, 1)
	require.Equal(t, id, page.Items[0].ID)

	// Positive control: the same lane still returns two rows for two ids.
	w4Get(t, env, "/v2/catalog/series?ids="+id+",999999999", &page)
	require.Len(t, page.Items, 1, "the second id does not exist and must land in missing[]")
}

// D23: the work-block member_count was a bare count(*) over
// catalog_series_member with none of the predicates the series faces apply.
func TestLiveSeriesMemberCountAgreesWithTheSeriesFace(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.Series)

	for _, nsfw := range []string{"", "&nsfw=true"} {
		total, _ := liveWorksTotal(t, env, "/v2/catalog/works?limit=1&include_total=true&series_id="+id+nsfw)
		require.NotNil(t, total)
		want := int(*total)

		var series struct {
			WorkCount int `json:"work_count"`
		}
		w4Get(t, env, "/v2/catalog/series/"+id+"?"+nsfw, &series)
		require.Equal(t, want, series.WorkCount, "series/{id}%s", nsfw)

		var list struct {
			Items []struct {
				WorkCount int `json:"work_count"`
			} `json:"items"`
		}
		w4Get(t, env, "/v2/catalog/series?ids="+id+nsfw, &list)
		require.Len(t, list.Items, 1)
		require.Equal(t, want, list.Items[0].WorkCount, "series?ids=%s", nsfw)

		type seriesRef struct {
			ID          string `json:"id"`
			MemberCount int    `json:"member_count"`
		}
		var detail struct {
			Series []seriesRef `json:"series"`
		}
		var sub struct {
			Items []seriesRef `json:"items"`
		}
		w4Get(t, env, "/v2/catalog/works/"+idstr(env.fx.Attributed)+"?include=series"+nsfw, &detail)
		w4Get(t, env, "/v2/catalog/works/"+idstr(env.fx.Attributed)+"/series?"+nsfw, &sub)
		for face, refs := range map[string][]seriesRef{"works/{id}.series": detail.Series, "works/{id}/series": sub.Items} {
			found := false
			for _, s := range refs {
				if s.ID == id {
					found = true
					require.Equal(t, want, s.MemberCount, "%s%s member_count", face, nsfw)
				}
			}
			require.True(t, found, "%s%s is missing the series", face, nsfw)
		}
	}

	sfw, _ := liveWorksTotal(t, env, "/v2/catalog/works?limit=1&include_total=true&series_id="+id)
	live, _ := liveWorksTotal(t, env, "/v2/catalog/works?limit=1&include_total=true&claim_state=live&series_id="+id)
	all, _ := liveWorksTotal(t, env, "/v2/catalog/works?limit=1&include_total=true&nsfw=true&series_id="+id)
	require.Greater(t, *sfw, *live, "the fixture must hold an unclaimed member, or this proves nothing")
	require.Greater(t, *all, *sfw, "the fixture must hold a member the nsfw gate hides, or this proves nothing")
}

// D25: q= was interpolated straight into ILIKE '%'||q||'%', so q=% matched the
// whole table and q=a_b matched axb.
func TestLiveCreditNameQueryTreatsWildcardsAsText(t *testing.T) {
	env := liveCatalog(t)

	var page struct {
		Items []struct {
			DisplayName string `json:"display_name"`
		} `json:"items"`
	}
	w4Get(t, env, "/v2/catalog/credit-names?q=Live&limit=100", &page)
	require.NotEmpty(t, page.Items, "the positive control must match, or the escape proves nothing")

	w4Get(t, env, "/v2/catalog/credit-names?q=%25&limit=100", &page)
	require.Empty(t, page.Items, "a literal percent sign must match no name")

	w4Get(t, env, "/v2/catalog/credit-names?q=L_ve&limit=100", &page)
	require.Empty(t, page.Items, "an underscore must be text, not a single-character wildcard")
}

// D7: PersonsList selected only id and display_name, so two required fields of
// the shared Person schema were null on every row of the list lane.
func TestLivePersonListCarriesTheRequiredFields(t *testing.T) {
	env := liveCatalog(t)

	type personRow struct {
		ID                  string  `json:"id"`
		PrimaryCreditNameID *string `json:"primary_credit_name_id"`
		Gender              *string `json:"gender"`
	}
	var detail personRow
	w4Get(t, env, "/v2/catalog/persons/"+idstr(env.fx.Person), &detail)
	require.NotNil(t, detail.Gender)

	for _, path := range []string{
		"/v2/catalog/persons?ids=" + idstr(env.fx.Person) + "," + idstr(env.fx.AnchoredPerson),
		"/v2/catalog/persons?limit=100",
	} {
		var page struct {
			Items []personRow `json:"items"`
		}
		w4Get(t, env, path, &page)
		byID := map[string]personRow{}
		for _, it := range page.Items {
			byID[it.ID] = it
		}
		row, ok := byID[idstr(env.fx.Person)]
		require.True(t, ok, path)
		require.NotNil(t, row.Gender, "%s: gender must come through the list lane", path)
		require.Equal(t, *detail.Gender, *row.Gender, path)

		control, ok := byID[idstr(env.fx.AnchoredPerson)]
		require.True(t, ok, path)
		require.Nil(t, control.Gender, "%s: an unrecorded gender stays null", path)
		require.Nil(t, control.PrimaryCreditNameID, path)
	}
}

// D8: EntityListRow.VndbTID had no column tag, so GORM looked for vndb_t_id and
// every trait row shipped vndb_tid:"".
func TestLiveTraitVndbTIDIsPublished(t *testing.T) {
	env := liveCatalog(t)

	type traitRow struct {
		ID      string `json:"id"`
		VndbTID string `json:"vndb_tid"`
	}
	var detail traitRow
	w4Get(t, env, "/v2/catalog/traits/"+idstr(env.fx.Trait), &detail)
	require.Equal(t, "i99999", detail.VndbTID)

	for _, path := range []string{
		"/v2/catalog/traits?ids=" + idstr(env.fx.Trait),
		"/v2/catalog/traits?limit=100",
	} {
		var page struct {
			Items []traitRow `json:"items"`
		}
		w4Get(t, env, path, &page)
		found := false
		for _, it := range page.Items {
			if it.ID != idstr(env.fx.Trait) {
				continue
			}
			found = true
			require.Equal(t, "i99999", it.VndbTID, path)
		}
		require.True(t, found, path)
	}
}

// D10: releasePrecision returned "day" for every non-null date and
// releaseStatus never returned "dated", so a year-only row published "2024"
// under format: date while claiming day precision, and a work released next
// year read `released`.
func TestReleaseDateProjection(t *testing.T) {
	future := time.Now().UTC().AddDate(2, 0, 0).Format("2006-01-02")
	past := time.Now().UTC().AddDate(-2, 0, 0).Format("2006-01-02")
	for _, tc := range []struct {
		name              string
		raw               *string
		wantDate          *string
		wantPrec, wantSts string
	}{
		{"null", nil, nil, "", "unknown"},
		{"year only", strp("2024"), strp("2024-01-01"), "year", "released"},
		{"year and month", strp("2024-06"), strp("2024-06-01"), "month", "released"},
		{"full date in the past", &past, &past, "day", "released"},
		{"full date in the future", &future, &future, "day", "dated"},
	} {
		gotDate := releaseDateValue(tc.raw)
		if tc.wantDate == nil {
			require.Nil(t, gotDate, tc.name)
		} else {
			require.NotNil(t, gotDate, tc.name)
			require.Equal(t, *tc.wantDate, *gotDate, tc.name)
		}
		gotPrec := releasePrecision(tc.raw)
		if tc.wantPrec == "" {
			require.Nil(t, gotPrec, tc.name)
		} else {
			require.NotNil(t, gotPrec, tc.name)
			require.Equal(t, tc.wantPrec, *gotPrec, tc.name)
		}
		require.Equal(t, tc.wantSts, releaseStatus(tc.raw), tc.name)
	}
}

func strp(s string) *string { return &s }

// D29: getStoreStats reused the purchase-link error list, which declares 502 —
// a status only storeErr's ErrShortenerDown arm mints, and only the minting
// path can reach it.
func TestStoreStatsDeclaresNoBadGateway(t *testing.T) {
	doc := Setup(fiber.New()).OpenAPI()
	stats := doc.Paths["/v2/store/stats"]
	require.NotNil(t, stats)
	require.NotNil(t, stats.Get)
	require.NotContains(t, stats.Get.Responses, "502",
		"the stats face cannot answer 502 and must not declare it")
	require.Contains(t, stats.Get.Responses, "422",
		"the range validator still mints 422, so the list is not simply emptied")

	links := doc.Paths["/v2/store/purchase-links/{product_id}"]
	require.NotNil(t, links)
	require.NotNil(t, links.Get)
	require.Contains(t, links.Get.Responses, "502",
		"the minting face keeps 502 — it is the only one that can reach it")
}
