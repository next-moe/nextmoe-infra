package editspec

import (
	"fmt"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"
)

const curatedSourceID int16 = 12

const userSourceID int16 = 1

const curatedMatchedBy = "curated"

// HumanSourceIDs is provenance.HumanSources() in catalog_source id form;
// TestHumanSourceIDsAreTheHumanProvenanceKeys pins the two against the seed.
func HumanSourceIDs() []int16 { return []int16{userSourceID, curatedSourceID} }

// CuratedLaneSQL and NotCuratedLaneSQL let the jobs and the merge machine name
// the human lane without each keeping its own copy of the id.
func CuratedLaneSQL(sourceColumn string) string {
	return fmt.Sprintf("%s = %d", sourceColumn, curatedSourceID)
}

func NotCuratedLaneSQL(sourceColumn string) string {
	return fmt.Sprintf("%s IS DISTINCT FROM %d", sourceColumn, curatedSourceID)
}

// HumanLaneFirstSQL orders the human lane ahead of the importers within one
// per-lang intro fold. It exists because curated (12) sorts AFTER every upstream
// importer id: a hand-written ja intro was collated behind the bangumi row for
// the same language and silently dropped by the one-row-per-lang fold, so the
// editor saved it, read it back, and it never rendered (6 live work rows in
// 2026-08).
//
// The `provenance = 0` gate is the whole point, and it was nearly shipped
// without: the term applies ONLY inside the source tier, so it can never elect a
// machine row. Two mistakes were made here in a row.
//
//  1. Placing the term before `provenance` let a MACHINE-TRANSLATED curated row
//     beat an upstream ORIGINAL, inverting the separate and correct "source
//     outranks machine" axis. The bug was human text hidden by upstream human
//     text; it was never "curated wins everything".
//  2. Moving it after `provenance` fixed that but still let it decide INSIDE the
//     machine tier, on the argument that a curated translation belongs with the
//     curated original it was made from. Production said otherwise. Across the
//     5,806 works it would have moved, the curated lane holds en/prov0 5,805,
//     zh-Hans/prov1 5,806, ja/prov0 2 — the zh-Hans machine row is translated
//     from our `en` row, while 5,804 of those works render bangumi's Japanese
//     ORIGINAL on the ja face either way. Preferring ours would have paired a
//     Japanese original with a translation of a different English text: EN→ZH
//     instead of JA→ZH, and usually a translation of a translation. THE LOCAL
//     SNAPSHOT SHOWS 4 OF THE 5,806 — this one cannot be judged on a dev copy.
//
// The gate also hands the machine tier back to its own rule: `derived` (source
// 18) is again the only preference among machine rows, per the wave-178 panel.
//
// Kept syntactically after `provenance` so the ordering reads tier-then-lane;
// with the gate the two positions are equivalent.
func HumanLaneFirstSQL(sourceColumn, provenanceColumn string) string {
	return fmt.Sprintf("(%s IN (%d, %d) AND %s = %d) DESC",
		sourceColumn, userSourceID, curatedSourceID,
		provenanceColumn, catmodel.IntroProvenanceSource)
}

// IntroFoldOrderSQL is the per-lang order an intro face keeps the first row of,
// and the order the MT jobs elect the row they translate by. The jobs used to
// take the lowest source_id instead: ~1,000 claimed works rendered a curated ja
// beside a zh-Hans translated from a DLsite promo (work 3993, 2026-10).
func IntroFoldOrderSQL(sourceColumn, provenanceColumn string) string {
	return provenanceColumn + ", " + HumanLaneFirstSQL(sourceColumn, provenanceColumn) + ", " + sourceColumn
}

// HumanLaneFirstNoProvenanceSQL is HumanLaneFirstSQL for the tables with no 0/1
// provenance axis (catalog_tag_intro, catalog_series_intro, catalog_credit):
// there is no machine tier to keep the human lane out of, so the gate has
// nothing to gate. Using it on a table that HAS the column would resurrect the
// 5,806-row mistake documented above.
//
// On the intro tables the missing column is not an oversight to backfill:
// neither has a machine writer today, and R1b's rule is that the axis follows
// the first one. catalog_credit has no MT axis at all.
// NULLS LAST is load-bearing on catalog_credit, whose source_id is nullable:
// `x IN (...)` over a NULL is NULL, and DESC defaults to NULLS FIRST, so the
// sourceless rows sorted AHEAD of the human lane this term exists to promote —
// the lane was inverted for exactly the rows it was written for. It is a no-op
// wherever the column is NOT NULL.
func HumanLaneFirstNoProvenanceSQL(sourceColumn string) string {
	return fmt.Sprintf("(%s IN (%d, %d)) DESC NULLS LAST", sourceColumn, userSourceID, curatedSourceID)
}

// HumanFieldProvenanceSQL is provenance.IsHuman in SQL, read off a
// field_provenance column for one stamped column: the head of that column's
// array is whoever last wrote it. The source list comes from the provenance
// package rather than a literal so the merge machine and the editor cannot
// disagree about what counts as a person.
func HumanFieldProvenanceSQL(provenanceColumn, column string) string {
	sources := provenance.HumanSources()
	quoted := make([]string, 0, len(sources))
	for _, s := range sources {
		quoted = append(quoted, "'"+s+"'")
	}
	return fmt.Sprintf("(%s -> '%s' -> 0 ->> 'source') IN (%s)",
		provenanceColumn, column, strings.Join(quoted, ", "))
}

const (
	maxListElements = editing.DefaultMaxElements
	maxHashRunes    = 128
	maxCaptionRunes = 500
	maxURLRunes     = 2000
	maxIntroRunes   = 50000
)

func asArray(v any, what string) ([]any, error) {
	return asArrayN(v, what, maxListElements)
}

func asArrayN(v any, what string, max int) ([]any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array of %s", what)
	}
	if len(arr) > max {
		return nil, fmt.Errorf("must contain at most %d elements", max)
	}
	return arr, nil
}

func asObject(el any, index int, allowed ...string) (map[string]any, error) {
	obj, ok := el.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("element %d: must be an object", index)
	}
	for key := range obj {
		found := false
		for _, a := range allowed {
			if key == a {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("element %d: unknown key %q", index, key)
		}
	}
	return obj, nil
}

func objInt(obj map[string]any, key string, index int, required bool) (int64, error) {
	raw, present := obj[key]
	if !present {
		if required {
			return 0, fmt.Errorf("element %d: %s is required", index, key)
		}
		return 0, nil
	}
	switch n := raw.(type) {
	case float64:
		if n != float64(int64(n)) {
			return 0, fmt.Errorf("element %d: %s must be an integer", index, key)
		}
		return int64(n), nil
	case int64:
		return n, nil
	}
	return 0, fmt.Errorf("element %d: %s must be an integer", index, key)
}

func objString(obj map[string]any, key string, index int, required bool, maxRunes int) (string, error) {
	raw, present := obj[key]
	if !present {
		if required {
			return "", fmt.Errorf("element %d: %s is required", index, key)
		}
		return "", nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("element %d: %s must be a string", index, key)
	}
	if required && strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("element %d: %s must not be empty", index, key)
	}
	if len([]rune(s)) > maxRunes {
		return "", fmt.Errorf("element %d: %s must be at most %d characters", index, key, maxRunes)
	}
	return s, nil
}

func objBool(obj map[string]any, key string, index int) (bool, error) {
	raw, present := obj[key]
	if !present {
		return false, nil
	}
	b, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("element %d: %s must be a boolean", index, key)
	}
	return b, nil
}

func asBool(v any) (any, error) {
	b, ok := v.(bool)
	if !ok {
		return nil, fmt.Errorf("must be a boolean")
	}
	return b, nil
}

func validateBool(v any) error {
	_, err := asBool(v)
	return err
}
