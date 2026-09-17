package llmsuggest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortCreditName(t *testing.T) {
	for name, want := range map[string]bool{
		"ん":       true,
		"小云":      true,
		"ゆ ら":     true,
		"yura":    true,
		"Nep":     true,
		"White":   false,
		"アナベル":    false,
		"村瀬 迪与":   false,
		"Takashi": false,
		"ab12":    true,
		"かの しうこ":  false,
	} {
		assert.Equal(t, want, shortCreditName(name), name)
	}
}

func TestCompanySide(t *testing.T) {
	side := func(name string, aliases []string, roles map[string]int) creditSideDossier {
		n := 0
		for _, c := range roles {
			n += c
		}
		return creditSideDossier{Name: name, aliasRaw: aliases, roleKeys: roles, Credits: n}
	}
	assert.True(t, companySide(side("NHKエンタープライズ", []string{"株式会社NHKエンタープライズ"}, nil)))
	assert.True(t, companySide(side("Foo Co., Ltd.", nil, nil)))
	assert.True(t, companySide(side("ホワイト", nil, map[string]int{"developer": 2, "publisher": 2})))
	assert.True(t, companySide(side("サークル㈱", nil, nil)), "NFKC folds the enclosed ideograph")
	assert.False(t, companySide(side("ホワイト", nil, map[string]int{"developer": 2, "scenario": 1})))
	assert.False(t, companySide(side("Vincent", nil, map[string]int{"voice-actor": 1})))
	assert.False(t, companySide(side("無名", nil, nil)), "no credits and no marker is not a company")
}

func TestCreditNameGuard(t *testing.T) {
	one, two := int64(1), int64(2)
	person := func(name string, pid *int64) creditSideDossier {
		return creditSideDossier{Name: name, personID: pid, roleKeys: map[string]int{"voice-actor": 1}, Credits: 1}
	}
	pair := func(a, b creditSideDossier) creditPairDossier { return creditPairDossier{A: a, B: b} }

	assert.Equal(t, guardBothLinked, creditNameGuard(pair(person("ん", &one), person("ん", &two))))
	assert.Equal(t, guardShortName, creditNameGuard(pair(person("ん", &one), person("会田孝信", nil))))
	assert.Equal(t, guardCompany, creditNameGuard(pair(
		creditSideDossier{Name: "ホワイトソフト", roleKeys: map[string]int{"developer": 3}, Credits: 3},
		person("White Taro", nil))))
	assert.Empty(t, creditNameGuard(pair(person("村瀬 迪与", &one), person("村瀬迪与", nil))),
		"one linked side is the case LinkService attaches")
}

func TestSpanOfWorks(t *testing.T) {
	y := func(v int) *int { return &v }
	var ws []creditWorkEv
	for _, v := range []int{2010, 1999, 2020, 2005, 2015, 2001, 2018, 2012} {
		ws = append(ws, creditWorkEv{Title: "w", Year: y(v)})
	}
	ws = append(ws, creditWorkEv{Title: "undated"})
	var got []int
	for _, w := range spanOfWorks(ws, 6) {
		got = append(got, *w.Year)
	}
	assert.Equal(t, []int{1999, 2001, 2005, 2015, 2018, 2020}, got)

	short := spanOfWorks([]creditWorkEv{{Title: "b"}, {Title: "a", Year: y(2003)}}, 6)
	require.Len(t, short, 2)
	assert.Equal(t, "a", short[0].Title, "dated works first")

	few := []creditWorkEv{{Title: "a", Year: y(2001)}, {Title: "b"}, {Title: "c"}, {Title: "d"}}
	assert.Len(t, spanOfWorks(few, 3), 3, "too few dated works falls back to the first n")
}

type recordingLLM struct {
	mu    sync.Mutex
	users []string
}

func (r *recordingLLM) client(t *testing.T, verdict string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var in struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &in)
		r.mu.Lock()
		for _, m := range in.Messages {
			if m.Role == "user" {
				r.users = append(r.users, m.Content)
			}
		}
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": verdict},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "mock-model")
}

func TestQueueCreditNameJudgesOnADossier(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(`TRUNCATE catalog_match_candidate, catalog_credit, catalog_name_alias,
		catalog_credit_name, catalog_person, catalog_external_ref, catalog_work_label, catalog_label,
		catalog_release, catalog_work, edit_suppressed_row RESTART IDENTITY CASCADE`).Error)

	id := func(q string) int64 {
		var v int64
		require.NoError(t, db.Raw(q).Scan(&v).Error)
		require.NotZero(t, v, q)
		return v
	}
	galgame := int16(id(`SELECT id FROM catalog_medium WHERE key = 'galgame'`))
	vndb := int16(id(`SELECT id FROM catalog_source WHERE key = 'vndb'`))
	bangumi := int16(id(`SELECT id FROM catalog_source WHERE key = 'bangumi'`))
	dlsite := int16(id(`SELECT id FROM catalog_source WHERE key = 'dlsite'`))
	erogamescape := int16(id(`SELECT id FROM catalog_source WHERE key = 'erogamescape'`))
	roleVA := id(`SELECT id FROM catalog_role WHERE key = 'voice-actor'`)
	roleDev := id(`SELECT id FROM catalog_role WHERE key = 'developer'`)

	label := func(name string) int64 {
		l := &model.CatalogLabel{DisplayName: name, Lang: "ja"}
		require.NoError(t, db.Create(l).Error)
		return l.ID
	}
	work := func(name string, year int16, labelID int64) int64 {
		w := &model.CatalogWork{MediumID: galgame, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive}
		require.NoError(t, db.Create(w).Error)
		require.NoError(t, db.Create(&model.CatalogRelease{WorkID: w.ID, ReleasedY: &year}).Error)
		require.NoError(t, db.Create(&model.CatalogWorkLabel{WorkID: w.ID, LabelID: labelID}).Error)
		return w.ID
	}
	name := func(n string, src int16, ext string, personID *int64, aliases ...string) int64 {
		cn := &model.CatalogCreditName{Name: n, Lang: "ja", PersonID: personID}
		require.NoError(t, db.Create(cn).Error)
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeCreditName, EntityID: cn.ID, SourceID: src,
			ExternalID: ext, LinkKind: model.LinkKindExact, MatchedBy: "test",
		}).Error)
		for _, a := range aliases {
			require.NoError(t, db.Create(&model.CatalogNameAlias{CreditNameID: cn.ID, Name: a, Lang: "ja"}).Error)
		}
		return cn.ID
	}
	credit := func(workID, nameID, roleID int64) {
		require.NoError(t, db.Create(&model.CatalogCredit{WorkID: workID, CreditNameID: nameID, RoleID: roleID}).Error)
	}
	candidate := func(a, b int64, status int16) {
		require.NoError(t, db.Create(&model.CatalogMatchCandidate{
			EntityType: model.EntityTypeCreditName, AID: min(a, b), BID: max(a, b),
			Reason: model.CandidateReasonAliasDeclared, Status: status,
		}).Error)
	}
	person := func(n string) *int64 {
		p := &model.CatalogPerson{DisplayName: n}
		require.NoError(t, db.Create(p).Error)
		return &p.ID
	}

	brand, other := label("ブランドA"), label("ブランドB")
	w1, w2 := work("作品一", 2005, brand), work("作品二", 2008, brand)
	hidden := work("隠し作品", 1990, other)

	yamadaV := name("山田 太郎", vndb, "s1", nil)
	yamadaB := name("山田太郎", bangumi, "p1", nil, "山田 太郎", "Yamada Tarou")
	credit(w1, yamadaV, roleVA)
	credit(hidden, yamadaV, roleVA)
	require.NoError(t, db.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: hidden, FieldKey: editspec.FieldWorkCredits,
		IdentityKey: editspec.CreditIdentity(roleVA, yamadaV, 0),
	}).Error)
	credit(w2, yamadaB, roleVA)
	candidate(yamadaV, yamadaB, model.CandidateStatusPending)

	yuraV := name("ゆら", vndb, "s2", nil)
	yuraB := name("yura", bangumi, "p2", nil, "ゆら")
	candidate(yuraV, yuraB, model.CandidateStatusPending)

	whiteV := name("White Taro", vndb, "s3", nil)
	whiteB := name("ホワイトソフト", bangumi, "p3", nil, "White Taro")
	credit(w1, whiteB, roleDev)
	credit(w2, whiteV, roleVA)
	candidate(whiteV, whiteB, model.CandidateStatusPending)

	linkedV := name("佐藤 花子", vndb, "s4", person("佐藤花子"))
	linkedB := name("佐藤花子", bangumi, "p4", person("佐藤 花子"), "佐藤 花子")
	candidate(linkedV, linkedB, model.CandidateStatusPending)

	doneV := name("鈴木 一郎", vndb, "s5", nil)
	doneB := name("鈴木一郎", bangumi, "p5", nil, "鈴木 一郎")
	candidate(doneV, doneB, model.CandidateStatusRejected)

	bareV := name("高橋 次郎", vndb, "s6", nil)
	bareB := name("高橋次郎", bangumi, "p6", nil, "高橋 次郎")
	credit(w1, bareV, roleVA)
	candidate(bareV, bareB, model.CandidateStatusPending)

	nickname := name("中野 三郎", dlsite, "d1", nil)
	credit(w2, nickname, roleVA)
	claimA := name("中野三郎子", bangumi, "p7", nil, "中野 三郎")
	credit(w1, claimA, roleVA)
	claimB := name("中野 三郎太", vndb, "s7", person("中野三郎太"), "中野 三郎")
	credit(w2, claimB, roleVA)
	candidate(nickname, claimA, model.CandidateStatusPending)
	candidate(nickname, claimB, model.CandidateStatusPending)

	declarer := name("伊藤 四郎", erogamescape, "e1", nil, "伊藤しろう", "イトウシロウ")
	credit(w1, declarer, roleVA)
	aliasOne := name("伊藤しろう", vndb, "s8", nil)
	credit(w2, aliasOne, roleVA)
	aliasTwo := name("イトウシロウ", bangumi, "p8", nil)
	credit(w2, aliasTwo, roleVA)
	candidate(declarer, aliasOne, model.CandidateStatusPending)
	candidate(declarer, aliasTwo, model.CandidateStatusPending)

	ozawaV := name("小澤 亜李", vndb, "s10", nil)
	credit(w1, ozawaV, roleVA)
	ozawaD := name("小澤亜李", dlsite, "d3", nil)
	credit(w2, ozawaD, roleVA)
	candidate(ozawaV, ozawaD, model.CandidateStatusPending)

	takoV := name("たこやき", vndb, "s11", nil)
	credit(w1, takoV, roleVA)
	takoB := name("タコ焼き", bangumi, "p10", nil, "たこやき")
	credit(w2, takoB, roleVA)
	candidate(takoV, takoB, model.CandidateStatusPending)

	onePerson := person("渡辺五郎")
	lone := name("渡辺 五郎", dlsite, "d2", nil)
	credit(w2, lone, roleVA)
	twinA := name("渡辺五郎", bangumi, "p9", onePerson, "渡辺 五郎")
	credit(w1, twinA, roleVA)
	twinB := name("渡辺 五郎", vndb, "s9", onePerson)
	credit(w1, twinB, roleVA)
	candidate(lone, twinA, model.CandidateStatusPending)
	candidate(lone, twinB, model.CandidateStatusPending)

	llm := &recordingLLM{}
	c := llm.client(t, `{"verdict":"same","reason":"same career under one brand","confidence":0.97}`)
	judged, errs, err := RunQueueCreditName(t.Context(), db, c, Options{Model: "mock-model", Concurrency: 1})
	require.NoError(t, err)
	assert.Zero(t, errs)
	assert.Equal(t, 13, judged, "seven judged by the model and six held by a guard")
	require.Len(t, llm.users, 7, "only the unguarded pairs reach the model")

	var user string
	for _, u := range llm.users {
		if strings.Contains(u, "山田") {
			user = u
		}
	}
	require.NotEmpty(t, user)
	raw := user[strings.Index(user, "{"):]
	var d struct {
		WhyPaired       string   `json:"why_paired"`
		AliasDeclaredBy string   `json:"alias_declared_by"`
		SharedWorks     int      `json:"shared_works"`
		SharedLabels    []string `json:"shared_labels"`
		A               struct {
			Name      string   `json:"name"`
			Sources   []string `json:"sources"`
			Credits   int      `json:"credits"`
			FirstYear *int     `json:"first_year"`
			Works     []struct {
				Title  string `json:"title"`
				Medium string `json:"medium"`
				Year   *int   `json:"year"`
				Role   string `json:"role"`
			} `json:"works"`
			Labels []string `json:"labels"`
		} `json:"a"`
		B struct {
			Name    string   `json:"name"`
			Aliases []string `json:"aliases"`
			Roles   []struct {
				Role    string `json:"role"`
				Credits int    `json:"credits"`
			} `json:"roles"`
		} `json:"b"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &d), raw)
	assert.Equal(t, "alias_declared", d.WhyPaired)
	assert.Equal(t, "b", d.AliasDeclaredBy, "the bangumi side lists the vndb spelling")
	assert.Equal(t, []string{"ブランドA"}, d.SharedLabels)
	assert.Zero(t, d.SharedWorks)
	assert.Equal(t, "山田 太郎", d.A.Name)
	assert.Equal(t, []string{"vndb"}, d.A.Sources)
	assert.Equal(t, 1, d.A.Credits, "the suppressed credit is not career evidence")
	require.Len(t, d.A.Works, 1)
	assert.Equal(t, "作品一", d.A.Works[0].Title)
	assert.Equal(t, "galgame", d.A.Works[0].Medium)
	assert.Equal(t, "voice-actor", d.A.Works[0].Role)
	require.NotNil(t, d.A.FirstYear)
	assert.Equal(t, 2005, *d.A.FirstYear)
	assert.Equal(t, []string{"ブランドA"}, d.A.Labels, "the suppressed work's label stays out")
	assert.Equal(t, []string{"山田 太郎", "Yamada Tarou"}, d.B.Aliases)
	require.Len(t, d.B.Roles, 1)
	assert.Equal(t, "voice-actor", d.B.Roles[0].Role)

	var rows []QueueVerdict
	require.NoError(t, db.Where("queue = ?", QueueCreditName).Order("a_id").Find(&rows).Error)
	require.Len(t, rows, 13)
	byPair := map[[2]int64]QueueVerdict{}
	for _, r := range rows {
		byPair[[2]int64{r.AID, r.BID}] = r
		assert.Equal(t, PromptCreditName, r.PromptVersion)
		assert.NotEmpty(t, string(r.Evidence))
	}
	key := func(a, b int64) [2]int64 { return [2]int64{min(a, b), max(a, b)} }
	assert.Equal(t, LaneLLM, byPair[key(yamadaV, yamadaB)].Lane)
	assert.Equal(t, VerdictSame, byPair[key(yamadaV, yamadaB)].Verdict)
	for _, k := range [][2]int64{key(declarer, aliasOne), key(declarer, aliasTwo), key(lone, twinA), key(lone, twinB)} {
		assert.Equal(t, LaneLLM, byPair[k].Lane, "%v: a name that declares all its partners, or partners of one person, is not contested", k)
	}
	for k, g := range map[[2]int64]string{
		key(yuraV, yuraB):     guardShortName,
		key(whiteV, whiteB):   guardCompany,
		key(linkedV, linkedB): guardBothLinked,
		key(bareV, bareB):     guardNoCareer,
		key(nickname, claimA): guardContested,
		key(nickname, claimB): guardContested,
	} {
		r := byPair[k]
		assert.Equal(t, LaneGuard, r.Lane, g)
		assert.Equal(t, VerdictUnsure, r.Verdict, g)
		assert.Equal(t, "guard: "+g, r.Reason)
	}

	judged, _, err = RunQueueCreditName(t.Context(), db, c, Options{Model: "mock-model", Concurrency: 1})
	require.NoError(t, err)
	assert.Zero(t, judged, "a second night asks nothing again")
	assert.Len(t, llm.users, 7)

	stale := byPair[key(nickname, claimA)]
	require.NoError(t, db.Model(&QueueVerdict{}).Where("id = ?", stale.ID).
		Updates(map[string]any{"lane": LaneLLM, "verdict": VerdictSame, "confidence": 0.99}).Error)
	for _, k := range [][2]int64{key(ozawaV, ozawaD), key(takoV, takoB)} {
		require.NoError(t, db.Model(&QueueVerdict{}).Where("id = ?", byPair[k].ID).
			Updates(map[string]any{"verdict": VerdictDifferent, "confidence": 0.95}).Error)
	}

	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueCreditName, Actor: 1, MinConfidence: 0.95, MinConfidenceReject: 0.7, Model: "mock-model",
	})
	require.NoError(t, err)
	assert.Equal(t, 6, st.Applied, "counts: %v", st.Counts)
	assert.Equal(t, 1, st.Counts[skipHeldByGuard], "a verdict judged before the pair became contested is held: %v", st.Counts)
	assert.Equal(t, 1, st.Counts[skipSameNameDifferent], "an identical spelling judged different waits for a person: %v", st.Counts)
	assert.Equal(t, 1, st.Counts["applied_"+applyReject], "a differently spelled pair judged different is rejected: %v", st.Counts)
	var statuses []struct {
		AID    int64 `gorm:"column:a_id"`
		Status int16
	}
	require.NoError(t, db.Raw(`SELECT a_id, status FROM catalog_match_candidate
		WHERE entity_type = ? ORDER BY a_id`, model.EntityTypeCreditName).Scan(&statuses).Error)
	got := map[int64]int16{}
	for _, s := range statuses {
		got[s.AID] = s.Status
	}
	assert.Equal(t, model.CandidateStatusAccepted, got[min(yamadaV, yamadaB)])
	pending := 0
	var all []struct {
		AID, BID int64
		Status   int16
	}
	require.NoError(t, db.Raw(`SELECT a_id, b_id, status FROM catalog_match_candidate WHERE entity_type = ?`,
		model.EntityTypeCreditName).Scan(&all).Error)
	for _, c := range all {
		k := [2]int64{c.AID, c.BID}
		switch k {
		case key(yuraV, yuraB), key(whiteV, whiteB), key(linkedV, linkedB), key(bareV, bareB),
			key(nickname, claimA), key(nickname, claimB), key(ozawaV, ozawaD):
			assert.Equal(t, model.CandidateStatusPending, c.Status, "a guarded pair waits for a person: %v", k)
			pending++
		}
	}
	assert.Equal(t, 7, pending)
	assert.Equal(t, model.CandidateStatusRejected, got[min(takoV, takoB)])
	assert.Equal(t, model.CandidateStatusAccepted, got[min(declarer, aliasOne)])
	var linked int64
	require.NoError(t, db.Raw(`SELECT count(DISTINCT person_id) FROM catalog_credit_name
		WHERE id IN (?, ?) AND person_id IS NOT NULL`, yamadaV, yamadaB).Scan(&linked).Error)
	assert.Equal(t, int64(1), linked)
}
