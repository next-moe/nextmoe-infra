package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntityWorksGateCountsWhatItLists(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	read := NewReadService(testDB)
	pub := newPublicSvc()
	roleVA := roleByKey(t, "voice-actor")
	va := createCreditName(t, nil, "声優")
	ch := createCharacter(t, "キャラ")

	shelf := func(name string, rating int16, titleNSFW *bool, status int16) int64 {
		w := createWork(t, name)
		require.NoError(t, testDB.Exec(`UPDATE catalog_work SET content_rating = ?, title_nsfw = ?, status = ? WHERE id = ?`,
			rating, titleNSFW, status, w.ID).Error)
		createCredit(t, w.ID, va.ID, roleVA, &ch.ID)
		return w.ID
	}
	no := false
	allAges := shelf("全年齢", model.ContentRatingAllAges, nil, model.WorkStatusLive)
	r18SFW := shelf("R18 表示 sfw", model.ContentRatingR18, &no, model.WorkStatusLive)
	r18NSFW := shelf("R18 表示 nsfw", model.ContentRatingR18, nil, model.WorkStatusLive)
	shelf("統合済み", model.ContentRatingAllAges, nil, model.WorkStatusMerged)
	createWorkCharacter(t, allAges, ch.ID, model.WorkCharacterKindMain, model.SpoilerNone)

	cases := []struct {
		name string
		gate EntityWorksGate
		want []int64
	}{
		{"nsfw, no display gate", EntityWorksGate{NSFW: true}, []int64{allAges, r18SFW, r18NSFW}},
		{"nsfw, sfw shelf", EntityWorksGate{NSFW: true, DisplayLimits: []string{"sfw"}}, []int64{allAges, r18SFW}},
		{"nsfw, nsfw shelf", EntityWorksGate{NSFW: true, DisplayLimits: []string{"nsfw"}}, []int64{r18NSFW}},
		{"no r18", EntityWorksGate{}, []int64{allAges}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.gate.WithTotal = true
			nw, err := read.NameWorks(ctx, va.ID, tc.gate, 50, 0)
			require.NoError(t, err)
			assert.EqualValues(t, len(tc.want), nw.Total)
			var got []int64
			for _, w := range nw.Works {
				got = append(got, w.Brief.WorkID)
			}
			assert.Equal(t, tc.want, got)

			cw, err := read.CharacterWorks(ctx, ch.ID, tc.gate, 50, 0)
			require.NoError(t, err)
			assert.EqualValues(t, len(tc.want), cw.Total)
			got = got[:0]
			for _, w := range cw.Works {
				got = append(got, w.Brief.WorkID)
			}
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("pages under the gate and total only when asked", func(t *testing.T) {
		sfw := EntityWorksGate{NSFW: true, DisplayLimits: []string{"sfw"}}
		p, ok, err := pub.Name(ctx, va.ID, true, sfw, 1, 1)
		require.NoError(t, err)
		require.True(t, ok)
		require.Len(t, p.Credits, 1)
		assert.Equal(t, r18SFW, p.Credits[0].Work.ID)
		assert.Nil(t, p.WorksTotal)

		sfw.WithTotal = true
		c, ok, err := pub.Character(ctx, ch.ID, true, sfw, 0, 1, 0)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, c.WorksTotal)
		assert.EqualValues(t, 2, *c.WorksTotal)
	})
}
