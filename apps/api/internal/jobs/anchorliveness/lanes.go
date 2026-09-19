package anchorliveness

import "api/internal/platform/catalog/model"

const (
	SourceVNDB    = "vndb"
	SourceBangumi = "bangumi"
	SourceEG      = "erogamescape"

	EntityWork       = "work"
	EntityRelease    = "release"
	EntityCharacter  = "character"
	EntityPerson     = "person"
	EntityCreditName = "credit_name"
	EntityLabel      = "label"
)

var EntityKeys = []string{
	EntityWork, EntityRelease, EntityCharacter, EntityPerson, EntityCreditName, EntityLabel,
}

type Lane struct {
	Source    string
	Entity    string
	Type      int16
	Table     string
	IDColumn  string
	NumericID bool
	Floor     int64
	Freshness bool
}

func (ln Lane) touchesWork() bool {
	return ln.Type == model.EntityTypeWork || ln.Type == model.EntityTypeRelease
}

func AllLanes() []Lane {
	return []Lane{
		{
			Source: SourceVNDB, Entity: EntityWork, Type: model.EntityTypeWork,
			Table: "src_vndb.vn", IDColumn: "id",
			// a partially loaded mirror would have marked all ~64k work anchors dead in one transaction
			Floor: 50_000,
		},
		{
			Source: SourceVNDB, Entity: EntityRelease, Type: model.EntityTypeRelease,
			Table: "src_vndb.releases", IDColumn: "id", Floor: 100_000,
		},
		{
			Source: SourceVNDB, Entity: EntityCharacter, Type: model.EntityTypeCharacter,
			Table: "src_vndb.chars", IDColumn: "id", Floor: 100_000,
		},
		{
			Source: SourceVNDB, Entity: EntityPerson, Type: model.EntityTypePerson,
			Table: "src_vndb.staff", IDColumn: "id", Floor: 20_000,
		},
		{
			Source: SourceVNDB, Entity: EntityCreditName, Type: model.EntityTypeCreditName,
			Table: "src_vndb.staff_alias", IDColumn: "aid", NumericID: true, Floor: 20_000,
		},
		{
			Source: SourceVNDB, Entity: EntityLabel, Type: model.EntityTypeLabel,
			Table: "src_vndb.producers", IDColumn: "id", Floor: 10_000,
		},
		{
			Source: SourceBangumi, Entity: EntityWork, Type: model.EntityTypeWork,
			Table: "src_bangumi.subject", IDColumn: "id", NumericID: true, Floor: 500_000,
		},
		{
			Source: SourceBangumi, Entity: EntityCharacter, Type: model.EntityTypeCharacter,
			Table: "src_bangumi.character", IDColumn: "id", NumericID: true, Floor: 100_000,
		},
		{
			Source: SourceBangumi, Entity: EntityPerson, Type: model.EntityTypePerson,
			Table: "src_bangumi.person", IDColumn: "id", NumericID: true, Floor: 50_000,
		},
		{
			Source: SourceBangumi, Entity: EntityCreditName, Type: model.EntityTypeCreditName,
			Table: "src_bangumi.person", IDColumn: "id", NumericID: true, Floor: 50_000,
		},
		{
			Source: SourceBangumi, Entity: EntityLabel, Type: model.EntityTypeLabel,
			Table: "src_bangumi.person", IDColumn: "id", NumericID: true, Floor: 50_000,
		},
		{
			Source: SourceEG, Entity: EntityPerson, Type: model.EntityTypePerson,
			Table: "creaters", IDColumn: "id", NumericID: true, Floor: 30_000, Freshness: true,
		},
		{
			Source: SourceEG, Entity: EntityCreditName, Type: model.EntityTypeCreditName,
			Table: "creaters", IDColumn: "id", NumericID: true, Floor: 30_000, Freshness: true,
		},
		{
			Source: SourceEG, Entity: EntityLabel, Type: model.EntityTypeLabel,
			Table: "brands", IDColumn: "id", NumericID: true, Floor: 5_000, Freshness: true,
		},
		{
			Source: SourceEG, Entity: EntityCharacter, Type: model.EntityTypeCharacter,
			Table: "characters", IDColumn: "id", NumericID: true, Floor: 20_000, Freshness: true,
		},
		{
			Source: SourceEG, Entity: EntityWork, Type: model.EntityTypeWork,
			Table: "games", IDColumn: "id", NumericID: true, Floor: 30_000, Freshness: true,
		},
	}
}

func LanesFor(source string) []Lane {
	out := make([]Lane, 0, 6)
	for _, ln := range AllLanes() {
		if ln.Source == source {
			out = append(out, ln)
		}
	}
	return out
}

func SummaryKeys() []string {
	keys := []string{"source", "apply"}
	for _, e := range EntityKeys {
		keys = append(keys, e+"_refs", e+"_to_mark", e+"_to_clear")
	}
	return append(keys, "marked_total", "cleared_total", "lanes_refused", "errors")
}
