package anchorliveness

import (
	"strings"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaneTableCoversConfirmedPairs(t *testing.T) {
	want := []struct {
		source, entity, table, idCol string
		numeric                      bool
		floor                        int64
		fresh                        bool
		etype                        int16
	}{
		{SourceVNDB, EntityWork, "src_vndb.vn", "id", false, 50_000, false, model.EntityTypeWork},
		{SourceVNDB, EntityRelease, "src_vndb.releases", "id", false, 100_000, false, model.EntityTypeRelease},
		{SourceVNDB, EntityCharacter, "src_vndb.chars", "id", false, 100_000, false, model.EntityTypeCharacter},
		{SourceVNDB, EntityPerson, "src_vndb.staff", "id", false, 20_000, false, model.EntityTypePerson},
		{SourceVNDB, EntityCreditName, "src_vndb.staff_alias", "aid", true, 20_000, false, model.EntityTypeCreditName},
		{SourceVNDB, EntityLabel, "src_vndb.producers", "id", false, 10_000, false, model.EntityTypeLabel},
		{SourceBangumi, EntityWork, "src_bangumi.subject", "id", true, 500_000, false, model.EntityTypeWork},
		{SourceBangumi, EntityCharacter, "src_bangumi.character", "id", true, 100_000, false, model.EntityTypeCharacter},
		{SourceBangumi, EntityPerson, "src_bangumi.person", "id", true, 50_000, false, model.EntityTypePerson},
		{SourceBangumi, EntityCreditName, "src_bangumi.person", "id", true, 50_000, false, model.EntityTypeCreditName},
		{SourceBangumi, EntityLabel, "src_bangumi.person", "id", true, 50_000, false, model.EntityTypeLabel},
		{SourceEG, EntityPerson, "creaters", "id", true, 30_000, true, model.EntityTypePerson},
		{SourceEG, EntityCreditName, "creaters", "id", true, 30_000, true, model.EntityTypeCreditName},
		{SourceEG, EntityLabel, "brands", "id", true, 5_000, true, model.EntityTypeLabel},
		{SourceEG, EntityCharacter, "characters", "id", true, 20_000, true, model.EntityTypeCharacter},
		{SourceEG, EntityWork, "games", "id", true, 30_000, true, model.EntityTypeWork},
	}
	got := AllLanes()
	require.Len(t, got, len(want))
	for i, w := range want {
		ln := got[i]
		assert.Equal(t, w.source, ln.Source, i)
		assert.Equal(t, w.entity, ln.Entity, i)
		assert.Equal(t, w.table, ln.Table, i)
		assert.Equal(t, w.idCol, ln.IDColumn, i)
		assert.Equal(t, w.numeric, ln.NumericID, i)
		assert.Equal(t, w.floor, ln.Floor, i)
		assert.Equal(t, w.fresh, ln.Freshness, i)
		assert.Equal(t, w.etype, ln.Type, i)
	}
	assert.Len(t, LanesFor(SourceVNDB), 6)
	assert.Len(t, LanesFor(SourceBangumi), 5)
	assert.Len(t, LanesFor(SourceEG), 5)
	assert.Empty(t, LanesFor("dlsite"))
}

func TestSummaryKeysHaveNoSuffixCollisions(t *testing.T) {
	keys := SummaryKeys()
	require.NotEmpty(t, keys)
	for i, a := range keys {
		for j, b := range keys {
			if i == j {
				continue
			}
			if strings.HasSuffix(b, a) {
				t.Errorf("%q is a suffix of %q — cron sed 's/.*%s=\\\\([0-9]*\\\\).*/' would read the wrong counter", a, b, a)
			}
		}
	}
}

func TestValidateOptsDSNAndEGDSN(t *testing.T) {
	err := ValidateOpts(Opts{Source: SourceVNDB})
	require.Error(t, err)
	assert.Equal(t, "catalog DSN is required (--dsn); refusing to guess", err.Error())

	require.NoError(t, ValidateOpts(Opts{DSN: "host=x dbname=y", Source: SourceVNDB}))
	require.NoError(t, ValidateOpts(Opts{DSN: "host=x dbname=y", Source: SourceBangumi}))
	require.NoError(t, ValidateOpts(Opts{DSN: "host=x dbname=y", Source: SourceEG, EGDSN: "host=x dbname=eg"}))

	err = ValidateOpts(Opts{DSN: "host=x", Source: SourceEG})
	require.Error(t, err)
	assert.Equal(t, "--eg-dsn is required when --source erogamescape", err.Error())

	err = ValidateOpts(Opts{DSN: "host=x", Source: SourceVNDB, EGDSN: "host=x dbname=eg"})
	require.Error(t, err)
	assert.Equal(t, "--eg-dsn is only valid with --source erogamescape", err.Error())

	err = ValidateOpts(Opts{DSN: "host=x", Source: "dlsite"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --source")

	err = ValidateOpts(Opts{DSN: "host=x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--source is required")
}

func TestFreshEnoughThreshold(t *testing.T) {
	assert.False(t, FreshEnough(0, 0))
	assert.False(t, FreshEnough(969, 1000), "96.9% must refuse")
	assert.True(t, FreshEnough(970, 1000), "97% must pass")
	assert.False(t, FreshEnough(96, 100))
	assert.True(t, FreshEnough(97, 100))
}

func TestSQLBuilders(t *testing.T) {
	vn := ExistsSQL("src_vndb.vn", "id", false)
	assert.Equal(t, "EXISTS (SELECT 1 FROM src_vndb.vn m WHERE m.id = catalog_external_ref.external_id)", vn)
	assert.NotContains(t, vn, "::text")

	aid := ExistsSQL("src_vndb.staff_alias", "aid", true)
	assert.Equal(t, "EXISTS (SELECT 1 FROM src_vndb.staff_alias m WHERE m.aid::text = catalog_external_ref.external_id)", aid)

	sub := ExistsSQL("src_bangumi.subject", "id", true)
	assert.Contains(t, sub, "m.id::text")

	work := Lane{Source: SourceVNDB, Entity: EntityWork, Table: "src_vndb.vn", IDColumn: "id"}
	assert.Equal(t, vn, LiveExistsSQL(work))

	eg := Lane{Source: SourceEG, Entity: EntityWork, Table: "games", IDColumn: "id", NumericID: true, Freshness: true}
	live := LiveExistsSQL(eg)
	assert.Equal(t, "EXISTS (SELECT 1 FROM "+AliveTempTable+" m WHERE m.id = catalog_external_ref.external_id)", live)
	assert.NotContains(t, live, "games")

	mark := MarkSQL(vn)
	assert.Contains(t, mark, "dead_at = now()")
	assert.Contains(t, mark, "AND NOT ("+vn+")")
	assert.NotContains(t, mark[:strings.Index(mark, "RETURNING")], "link_kind")
	assert.Contains(t, ClearSQL(vn), "dead_at = NULL")
	assert.Contains(t, ClearSQL(vn), "AND ("+vn+")")
	assert.Equal(t, "SELECT count(*) FROM src_vndb.vn", FloorSQL("src_vndb.vn"))
	assert.Contains(t, FreshnessSQL("games"), "interval '48 hours'")
	assert.Contains(t, AliveSQL("games", "id", true), "id::text")
	assert.Contains(t, CreateAliveTempSQL(), "ON COMMIT DROP")
}
