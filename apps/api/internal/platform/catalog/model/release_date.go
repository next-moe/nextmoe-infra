package model

import "fmt"

func IsFullReleaseKind(kind int16) bool {
	return kind == ReleaseKindDefault || kind == ReleaseKindDigital || kind == ReleaseKindPhysical
}

func FullReleaseKindSQL(alias string) string {
	return fmt.Sprintf("%s.kind IN (%d, %d, %d)", alias, ReleaseKindDefault, ReleaseKindDigital, ReleaseKindPhysical)
}

// ReleaseDatesWorkSQL is true for a release row whose date, if it has one,
// counts toward its work's date under WorkReleaseOrdSQL. The TBA bucket is
// "no dated row that counts"; written as the aggregate it was an OR of
// subplans the planner costed at 1.3M and ran 8x slower.
func ReleaseDatesWorkSQL(alias string) string {
	sib := alias + "_full"
	return "(" + FullReleaseKindSQL(alias) + " OR NOT EXISTS (SELECT 1 FROM catalog_release " + sib +
		" WHERE " + sib + ".work_id = " + alias + ".work_id AND " + sib + ".deleted_at IS NULL AND " + FullReleaseKindSQL(sib) + "))"
}

// ReleaseOrdSQL is a release row's date as YYYYMMDD with the unknown parts 0,
// and NULL when the row is undated.
func ReleaseOrdSQL(alias string) string {
	return alias + ".released_y::int * 10000 + coalesce(" + alias + ".released_m,0)::int * 100 + coalesce(" + alias + ".released_d,0)::int"
}

// WorkReleaseOrdSQL is a work's release date, as an aggregate over its live
// catalog_release rows grouped by work. The rows must include the undated
// ones: a full release that exists but has no date leaves the work undated.
// A trial or a patch dates the work only when it has no full release row at
// all. Taking the earliest row of any kind put work 5769 in September's
// calendar on its 体験版 (2026-09-04) while its 通常版 ships 2026-10-30.
//
// dated, when not empty, further limits the rows the date is taken from.
func WorkReleaseOrdSQL(alias, dated string) string {
	full := FullReleaseKindSQL(alias)
	fullDated, anyDated := full, "true"
	if dated != "" {
		fullDated, anyDated = full+" AND "+dated, dated
	}
	ord := ReleaseOrdSQL(alias)
	return "CASE WHEN bool_or(" + full + ") THEN min(" + ord + ") FILTER (WHERE " + fullDated + ") " +
		"ELSE min(" + ord + ") FILTER (WHERE " + anyDated + ") END"
}
