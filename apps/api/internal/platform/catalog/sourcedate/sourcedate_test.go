package sourcedate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaxYear(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, 2029, MaxYear(now))
}

func TestVNDB(t *testing.T) {
	const maxYear = 2029
	cases := []struct {
		in    int64
		state State
		y     int16
		m, d  int
	}{
		{99999999, TBA, 0, 0, 0},
		{0, Unknown, 0, 0, 0},
		{20269999, Dated, 2026, 0, 0},
		{20261299, Dated, 2026, 12, 0},
		{20200115, Dated, 2020, 1, 15},
		{20210300, Dated, 2021, 3, 0},
		{20190000, Dated, 2019, 0, 0},
		{19000101, Unknown, 0, 0, 0},
		{20200015, Dated, 2020, 0, 0},
		{20300101, Unknown, 0, 0, 0},
		{19491231, Unknown, 0, 0, 0},
	}
	for _, c := range cases {
		v := VNDB(c.in, maxYear)
		assert.Equal(t, c.state, v.State, "released %d state", c.in)
		if c.state != Dated {
			assert.Zero(t, v.Y, "released %d year", c.in)
			assert.Nil(t, v.M, "released %d month", c.in)
			assert.Nil(t, v.D, "released %d day", c.in)
			continue
		}
		assert.Equal(t, c.y, v.Y, "released %d year", c.in)
		assertPtr16(t, c.m, v.M, "released %d month", c.in)
		assertPtr16(t, c.d, v.D, "released %d day", c.in)
	}
}

func TestDLsite(t *testing.T) {
	const maxYear = 2029
	assert.Equal(t, Unknown, DLsite("", maxYear).State)
	assert.Equal(t, Unknown, DLsite("not-a-date", maxYear).State)
	assert.Equal(t, Unknown, DLsite("2024-13-01", maxYear).State)
	assert.Equal(t, Unknown, DLsite("2099-12-31", maxYear).State)
	assert.Equal(t, Unknown, DLsite("1949-01-01", maxYear).State)

	v := DLsite("2025-08-18", maxYear)
	require.Equal(t, Dated, v.State)
	assert.Equal(t, int16(2025), v.Y)
	assertPtr16(t, 8, v.M, "month")
	assertPtr16(t, 18, v.D, "day")
}

func TestGetchu(t *testing.T) {
	const maxYear = 2029
	cases := []struct {
		in    string
		state State
		y     int16
		m, d  int
	}{
		{"2003/06/13", Dated, 2003, 6, 13},
		{"2018/5/4", Dated, 2018, 5, 4},
		{"2026/01/下旬", Dated, 2026, 1, 0},
		{"2026/1/予定", Dated, 2026, 1, 0},
		{"2026/01/中旬", Dated, 2026, 1, 0},
		{"2026/01/上旬", Dated, 2026, 1, 0},
		{"未定", TBA, 0, 0, 0},
		{" 未定 ", TBA, 0, 0, 0},
		{"発売中止", Unknown, 0, 0, 0},
		{"", Unknown, 0, 0, 0},
		{"0001/01/01", Unknown, 0, 0, 0},
		{"2026/02/30", Unknown, 0, 0, 0},
		{"1969/12/31", Unknown, 0, 0, 0},
		{"2026/13/01", Unknown, 0, 0, 0},
		{"2026/00/01", Unknown, 0, 0, 0},
		{"2030/01/下旬", Unknown, 0, 0, 0},
		{"2026/13/下旬", Unknown, 0, 0, 0},
	}
	for _, c := range cases {
		v := Getchu(c.in, maxYear)
		assert.Equal(t, c.state, v.State, "%q state", c.in)
		if c.state != Dated {
			continue
		}
		assert.Equal(t, c.y, v.Y, "%q year", c.in)
		assertPtr16(t, c.m, v.M, "%q month", c.in)
		assertPtr16(t, c.d, v.D, "%q day", c.in)
	}
}

func TestEG(t *testing.T) {
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, TBA, EG("2050-01-01", now).State)
	assert.Equal(t, Unknown, EG("", now).State)
	assert.Equal(t, Unknown, EG("not-a-date", now).State)
	assert.Equal(t, Unknown, EG("1949-12-31", now).State)

	v := EG("2027-12-31", now)
	require.Equal(t, Dated, v.State, "2027-12-31 is within two years of 2026-09-18")
	assert.Equal(t, int16(2027), v.Y)
	assertPtr16(t, 12, v.M, "month")
	assertPtr16(t, 31, v.D, "day")

	v = EG(" 2004-05-28 ", now)
	require.Equal(t, Dated, v.State)
	assert.Equal(t, int16(2004), v.Y)
}

func TestBangumi(t *testing.T) {
	const maxYear = 2029
	cases := []struct {
		in    string
		state State
		y     int16
		m, d  int
	}{
		{"2004-04-28", Dated, 2004, 4, 28},
		{"1996-07-01", Dated, 1996, 7, 1},
		{"2016", Dated, 2016, 0, 0},
		{"2016-05", Dated, 2016, 5, 0},
		{"2016-00-00", Dated, 2016, 0, 0},
		{"2016-13-01", Dated, 2016, 0, 0},
		{"2016-05-00", Dated, 2016, 5, 0},
		{"2016-05-40", Dated, 2016, 5, 0},
		{" 2019-09-02 00:00:00 ", Dated, 2019, 9, 2},
		{"2050-01-01", Unknown, 0, 0, 0},
		{"2099-12-31", Unknown, 0, 0, 0},
		{"1949-01-01", Unknown, 0, 0, 0},
		{"abcd-ef-gh", Unknown, 0, 0, 0},
		{"", Unknown, 0, 0, 0},
		{"20", Unknown, 0, 0, 0},
	}
	for _, c := range cases {
		v := Bangumi(c.in, maxYear)
		assert.Equal(t, c.state, v.State, c.in)
		if c.state != Dated {
			continue
		}
		assert.Equal(t, c.y, v.Y, c.in)
		assertPtr16(t, c.m, v.M, c.in+" month")
		assertPtr16(t, c.d, v.D, c.in+" day")
	}
}

func assertPtr16(t *testing.T, want int, got *int16, msgAndArgs ...any) {
	t.Helper()
	if want == 0 {
		assert.Nil(t, got, msgAndArgs...)
		return
	}
	require.NotNil(t, got, msgAndArgs...)
	assert.Equal(t, int16(want), *got, msgAndArgs...)
}
