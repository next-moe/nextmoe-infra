package srcvndb

import (
	"os"
	"testing"

	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCopyUnescape(t *testing.T) {
	v, isNull := copyUnescape(`\N`)
	assert.True(t, isNull)
	assert.Equal(t, "", v)

	v, isNull = copyUnescape("plain")
	assert.False(t, isNull)
	assert.Equal(t, "plain", v)

	v, isNull = copyUnescape(`Line1\nLine2\tX\\Y`)
	assert.False(t, isNull)
	assert.Equal(t, "Line1\nLine2\tX\\Y", v)
}

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("catalog/srcvndb")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("catalog/srcvndb", "cannot connect to test database: %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		dbtest.SkipMainf("catalog/srcvndb", "ensure src_vndb schema failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func TestIngestFixtureAndIdempotency(t *testing.T) {
	report, err := Run(testDB, "testdata", "")
	require.NoError(t, err)

	assert.Equal(t, int64(2), report.PerFile["vn"].Rows)
	assert.Equal(t, int64(3), report.PerFile["vn_titles"].Rows)
	assert.Equal(t, int64(2), report.PerFile["chars"].Rows)
	assert.Equal(t, int64(3), report.PerFile["chars_names"].Rows)
	assert.Equal(t, int64(3), report.PerFile["chars_vns"].Rows)
	assert.Equal(t, int64(3), report.PerFile["images"].Rows)
	assert.Equal(t, int64(1), report.PerFile["images"].Skipped, "the screenshot row is dropped")

	count := func(table string) int64 {
		var n int64
		require.NoError(t, testDB.Table(table).Count(&n).Error)
		return n
	}
	assert.Equal(t, int64(2), count("src_vndb.vn"))
	assert.Equal(t, int64(2), count("src_vndb.chars"))
	assert.Equal(t, int64(3), count("src_vndb.images"), "portrait and cover rows loaded")

	var v1 VN
	require.NoError(t, testDB.First(&v1, "id = ?", "v1").Error)
	assert.Equal(t, "ja", v1.OLang)
	assert.Equal(t, "cv1", v1.Image)
	assert.Equal(t, "A test blurb about a cat god.", v1.Description)
	var v2 VN
	require.NoError(t, testDB.First(&v2, "id = ?", "v2").Error)
	assert.Equal(t, "", v2.Image, `\N cover id → empty`)
	assert.Equal(t, "Second\nline description.", v2.Description, "escaped newline decoded")

	var t1 VNTitle
	require.NoError(t, testDB.First(&t1, "id = ? AND lang = ?", "v1", "ja").Error)
	assert.Equal(t, "猫神さまと", t1.Title)
	assert.True(t, t1.Official)
	assert.Equal(t, "Neko-gami-sama to", t1.Latin)
	var t2 VNTitle
	require.NoError(t, testDB.First(&t2, "id = ? AND lang = ?", "v1", "en").Error)
	assert.Equal(t, "", t2.Latin, `\N latin → empty`)

	var c2 Char
	require.NoError(t, testDB.First(&c2, "id = ?", "c2").Error)
	assert.Equal(t, "c1", c2.Main, "instance_of base")
	assert.EqualValues(t, 2, c2.MainSpoil)
	assert.Equal(t, "", c2.Image, `\N image → empty`)

	var c1 Char
	require.NoError(t, testDB.First(&c1, "id = ?", "c1").Error)
	assert.Equal(t, "ch1", c1.Image)
	assert.Equal(t, "Line1\nLine2", c1.Description, "escaped newline decoded")

	var ch2 Image
	require.NoError(t, testDB.First(&ch2, "id = ?", "ch2").Error)
	assert.EqualValues(t, 150, ch2.SexualAvg)
	assert.EqualValues(t, 200, ch2.ViolenceAvg)

	var enName CharName
	require.NoError(t, testDB.Where("id = ? AND lang = ?", "c1", "en").First(&enName).Error)
	assert.Equal(t, "", enName.Latin, `\N latin → empty`)
	var maxSpoil int16
	require.NoError(t, testDB.Raw(`SELECT max(spoil) FROM src_vndb.chars_vns`).Scan(&maxSpoil).Error)
	assert.EqualValues(t, 2, maxSpoil)

	for table, want := range map[string]int64{
		"src_vndb.vn_relations":        2,
		"src_vndb.staff":               2,
		"src_vndb.staff_alias":         3,
		"src_vndb.vn_staff":            3,
		"src_vndb.vn_seiyuu":           2,
		"src_vndb.traits":              3,
		"src_vndb.traits_parents":      2,
		"src_vndb.chars_traits":        3,
		"src_vndb.tags":                2,
		"src_vndb.tags_parents":        1,
		"src_vndb.tags_vn":             3,
		"src_vndb.producers":           2,
		"src_vndb.releases":            2,
		"src_vndb.releases_vn":         2,
		"src_vndb.releases_producers":  2,
		"src_vndb.releases_platforms":  3,
		"src_vndb.extlinks":            3,
		"src_vndb.releases_extlinks":   3,
		"src_vndb.releases_titles":     2,
		"src_vndb.vn_extlinks":         3,
		"src_vndb.producers_extlinks":  2,
		"src_vndb.staff_extlinks":      1,
		"src_vndb.producers_relations": 2,
	} {
		assert.Equal(t, want, count(table), table)
	}

	assert.Equal(t, "a", c1.BloodT)
	assert.EqualValues(t, 88, c1.SBust)
	assert.EqualValues(t, 920, c1.Birthday, "mmdd birthday")
	assert.EqualValues(t, 156, c1.Height)
	assert.Nil(t, c1.Weight, `\N weight → NULL`)
	require.NotNil(t, c1.Age)
	assert.EqualValues(t, 17, *c1.Age)
	assert.Equal(t, "d", c2.CupSize)
	assert.Equal(t, "f", c2.SpoilSex)
	require.NotNil(t, c2.Weight)
	assert.EqualValues(t, 43, *c2.Weight)
	assert.Nil(t, c2.Age, `\N age → NULL`)

	var s1 Staff
	require.NoError(t, testDB.First(&s1, "id = ?", "s1").Error)
	assert.Equal(t, "p42", s1.Prod)
	assert.Equal(t, "Scenario writer.\nBorn 1972.", s1.Description)
	var s2 Staff
	require.NoError(t, testDB.First(&s2, "id = ?", "s2").Error)
	assert.Equal(t, "", s2.Prod, `\N prod → empty`)
	assert.EqualValues(t, 3, s2.Main, "primary alias aid")

	var alias2 StaffAlias
	require.NoError(t, testDB.First(&alias2, "aid = ?", 2).Error)
	assert.Equal(t, "s1", alias2.ID)
	assert.Equal(t, "玄", alias2.Name)
	assert.Equal(t, "", alias2.Latin)

	var scenario, art VNStaff
	require.NoError(t, testDB.First(&scenario, "role = ?", "scenario").Error)
	assert.Nil(t, scenario.EID, `\N eid → NULL (base edition)`)
	require.NoError(t, testDB.First(&art, "role = ?", "art").Error)
	require.NotNil(t, art.EID)
	assert.EqualValues(t, 0, *art.EID, "edition 0 is a real edition")
	assert.Equal(t, "cover only", art.Note)

	var i1, i3 Trait
	require.NoError(t, testDB.First(&i1, "id = ?", "i1").Error)
	assert.Equal(t, "", i1.GID, "root trait")
	assert.EqualValues(t, 1, i1.GOrder)
	require.NoError(t, testDB.First(&i3, "id = ?", "i3").Error)
	assert.True(t, i3.Sexual)
	assert.EqualValues(t, 2, i3.DefaultSpoil)
	var ct CharTrait
	require.NoError(t, testDB.Where("id = ? AND tid = ?", "c1", "i3").First(&ct).Error)
	assert.EqualValues(t, 2, ct.Spoil)
	assert.True(t, ct.Lie)

	var g1 Tag
	require.NoError(t, testDB.First(&g1, "id = ?", "g1").Error)
	assert.Equal(t, "This game features fantasy.\nSecond line.", g1.Description)
	var down TagVN
	require.NoError(t, testDB.First(&down, "vote = ?", -3).Error)
	assert.Equal(t, "u93", down.UID)
	assert.Nil(t, down.Spoiler, `\N spoiler → NULL`)
	assert.Nil(t, down.Lie)
	var anon TagVN
	require.NoError(t, testDB.First(&anon, "tag = ?", "g2").Error)
	assert.Equal(t, "", anon.UID, `\N uid → empty (anonymized)`)
	require.NotNil(t, anon.Spoiler)
	assert.EqualValues(t, 0, *anon.Spoiler, "explicit spoiler 0 survives")
	require.NotNil(t, anon.Lie)
	assert.True(t, *anon.Lie)
	assert.True(t, anon.Ignore)
	assert.Equal(t, "escaped\tnote", anon.Notes, "escaped tab decoded")

	var r1, r2 Release
	require.NoError(t, testDB.First(&r1, "id = ?", "r1").Error)
	assert.EqualValues(t, 4946569500413, r1.GTIN)
	assert.Nil(t, r1.MinAge, `\N minage → NULL (unknown)`)
	assert.Nil(t, r1.AniBg)
	assert.Nil(t, r1.Uncensored)
	assert.True(t, r1.HasEro)
	require.NoError(t, testDB.First(&r2, "id = ?", "r2").Error)
	require.NotNil(t, r2.MinAge)
	assert.EqualValues(t, 0, *r2.MinAge, "explicit all-ages rating survives")
	require.NotNil(t, r2.AniBg)
	assert.False(t, *r2.AniBg)
	require.NotNil(t, r2.AniFace)
	assert.True(t, *r2.AniFace)
	require.NotNil(t, r2.Uncensored)
	assert.True(t, *r2.Uncensored)
	assert.Equal(t, 99999999, r2.Released, "TBA sentinel")
	assert.Equal(t, "Note\nline", r2.Notes)
	assert.Equal(t, "Ren'Py", r2.Engine)

	var rt ReleaseTitle
	require.NoError(t, testDB.Where("id = ? AND lang = ?", "r2", "en").First(&rt).Error)
	assert.Equal(t, "", rt.Title)
	assert.Equal(t, "Latin Only Title", rt.Latin)
	assert.True(t, rt.MTL)

	var p2 Producer
	require.NoError(t, testDB.First(&p2, "id = ?", "p2").Error)
	assert.Equal(t, "", p2.Latin)
	assert.Equal(t, "in", p2.Type)
	var rel VNRelation
	require.NoError(t, testDB.Where("id = ? AND vid = ?", "v100", "v101").First(&rel).Error)
	assert.Equal(t, "seq", rel.Relation)
	assert.True(t, rel.Official)

	var vlink VNExtlink
	require.NoError(t, testDB.Where("id = ? AND link = ?", "v1", 3).First(&vlink).Error)
	var v1Links int64
	require.NoError(t, testDB.Model(&VNExtlink{}).Where("id = ?", "v1").Count(&v1Links).Error)
	assert.EqualValues(t, 2, v1Links, "one vn carries two links")
	var plink ProducerExtlink
	require.NoError(t, testDB.Where("id = ? AND link = ?", "p2", 2).First(&plink).Error)
	var slink StaffExtlink
	require.NoError(t, testDB.Where("id = ? AND link = ?", "s1", 3).First(&slink).Error)

	var sub, par ProducerRelation
	require.NoError(t, testDB.Where("id = ? AND pid = ?", "p1", "p2").First(&sub).Error)
	assert.Equal(t, "sub", sub.Relation)
	require.NoError(t, testDB.Where("id = ? AND pid = ?", "p2", "p1").First(&par).Error)
	assert.Equal(t, "par", par.Relation, "mirrored direction carries the inverse code")

	_, err = Run(testDB, "testdata", "")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count("src_vndb.chars"))
	assert.Equal(t, int64(3), count("src_vndb.chars_vns"))
	assert.Equal(t, int64(2), count("src_vndb.releases"))
	assert.Equal(t, int64(3), count("src_vndb.tags_vn"))
	assert.Equal(t, int64(3), count("src_vndb.vn_staff"))
	assert.Equal(t, int64(3), count("src_vndb.vn_extlinks"))
	assert.Equal(t, int64(2), count("src_vndb.producers_relations"))

	_, err = Run(testDB, "testdata", "chars")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count("src_vndb.chars"))
	_, err = Run(testDB, "testdata", "releases")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count("src_vndb.releases"))
}
