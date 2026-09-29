package service

import (
	"strings"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func TestCalendarFollowedBy(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	follows := NewUserEntityFollowService(testDB)
	svc := newPublicSvc()

	brand := createLabel(t, "Followed Brand", model.LabelKindGameBrand)
	circle := createLabel(t, "Followed Circle", model.LabelKindDoujinCircle)
	publisher := createLabel(t, "Unfollowed Publisher", model.LabelKindPublisher)

	dated := func(name, olang string, day int16, label int64) int64 {
		t.Helper()
		w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, name)
		if olang != "ja" {
			setOLang(t, w.ID, olang)
		}
		createRelease(t, w.ID, 2024, 6, day)
		if label != 0 {
			linkCompanies(t, w.ID, label)
		}
		return w.ID
	}
	ja := dated("Brand JA", "ja", 1, brand)
	zh := dated("Brand ZH", "zh-Hans", 2, brand)
	en := dated("Brand EN", "en", 3, brand)
	circleWork := dated("Circle Work", "ja", 4, circle)
	dated("Publisher Work", "ja", 5, publisher)
	dated("No Company", "ja", 6, 0)

	_, err := follows.FollowCompany(ctx, 1, brand, "client", "site")
	require.NoError(t, err)
	_, err = follows.FollowCompany(ctx, 2, circle, "client", "site")
	require.NoError(t, err)

	filter := func(uid int64) CalendarFilter {
		return CalendarFilter{FollowedBy: uid, OLang: PublicOLang{All: true}}
	}
	require.Equal(t, []int64{ja, zh, en}, calIDs(t, svc, june2024(), filter(1)))
	require.Equal(t, []int64{circleWork}, calIDs(t, svc, june2024(), filter(2)))
	require.Empty(t, calIDs(t, svc, june2024(), filter(3)))

	_, _, found, err := svc.CalendarBounds(ctx, filter(3))
	require.NoError(t, err)
	require.False(t, found)
}

func TestCalendarFollowedByBypassesTotalsCache(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Cached Public")
	createRelease(t, w.ID, 2024, 6, 1)

	public := CalendarFilter{}
	minOrd, _, found, err := svc.CalendarBounds(t.Context(), public)
	require.NoError(t, err)
	require.True(t, found)

	pubKey := "calendar-bounds\x00" + public.PopulationKey() + "\x00min"
	svc.totals.mu.Lock()
	before, ok := svc.totals.entries[pubKey]
	svc.totals.mu.Unlock()
	require.True(t, ok, "public bounds key %s", pubKey)
	require.Equal(t, minOrd, before.val)

	user := CalendarFilter{FollowedBy: 42, OLang: PublicOLang{All: true}}
	_, _, found, err = svc.CalendarBounds(t.Context(), user)
	require.NoError(t, err)
	require.False(t, found)

	svc.totals.mu.Lock()
	defer svc.totals.mu.Unlock()
	needle := user.PopulationKey()
	for key := range svc.totals.entries {
		if strings.Contains(key, needle) {
			t.Fatalf("user-scoped bounds cached under %s", key)
		}
	}
	after, ok := svc.totals.entries[pubKey]
	require.True(t, ok)
	require.Equal(t, before.val, after.val)
}
