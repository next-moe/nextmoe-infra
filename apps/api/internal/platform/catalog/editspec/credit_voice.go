package editspec

import (
	"fmt"
	"strings"

	"api/internal/platform/catalog/model"
)

// creditedVoiceSourceIDs name the cast as the game credits it.
var creditedVoiceSourceIDs = []int16{model.SourceVNDB, model.SourceErogameScape, model.SourceUser, model.SourceCurated}

// VNDB writes a voice note only to tell versions apart. These are the notes
// that still mean "the original release", so a voice under one of them covers
// the work as Bangumi describes it; "PS2", "English" or "Remake" does not.
var originalVoiceNotes = []string{
	"japanese", "jp", "japanese voice", "japanese version", "japanese edition",
	"pc", "pc ver.", "pc ver", "pc version", "windows version", "original pc", "original game",
	"uncredited", "partially voiced", "partial", "partially, uncredited",
	"full voice", "all releases",
}

// ShownCreditSQL is the public read-path predicate on a catalog_credit alias:
// not suppressed, and not a Bangumi voice outranked by a credited one.
//
// Bangumi names a voice actress by her main name; VNDB and ErogameScape keep
// the name the game credits, which on adult titles is usually a separate
// stage name. Work 2452 showed 桜咲千依 (Bangumi) beside 藤咲ウサ (VNDB) as two
// voices of one character, and 32,739 Bangumi rows on prod did the same. When a
// credited source voices the character on the original release, the Bangumi
// row stays in the table but is not shown on any public face, including the
// main name's own credit list.
func ShownCreditSQL(alias string) string {
	return NotSuppressedCreditSQL(alias) + " AND " + notOutvoicedSQL(alias)
}

// ov.id <> alias.id changes no answer (the two rows differ in source), but it
// keeps the planner from rewriting the probe as a hashed subplan: on a credit
// name with 4,600 credits it hashed every credited voice row on prod and took
// 830 ms, against 9 ms probing idx_catalog_credit_credited_voice.
func notOutvoicedSQL(alias string) string {
	return fmt.Sprintf(`NOT (%[1]s.source_id = %[2]d AND %[1]s.character_id IS NOT NULL AND EXISTS (
			SELECT 1 FROM catalog_credit ov
			WHERE ov.work_id = %[1]s.work_id AND ov.character_id = %[1]s.character_id AND ov.role_id = %[1]s.role_id
			  AND ov.id <> %[1]s.id
			  AND ov.source_id IN (%[3]s)
			  AND (btrim(ov.note) = '' OR lower(btrim(ov.note)) IN (%[4]s))
			  AND %[5]s))`,
		alias, model.SourceBangumi, int16List(creditedVoiceSourceIDs), quotedList(originalVoiceNotes),
		NotSuppressedCreditSQL("ov"))
}

// VoiceNoteSQL is the version note of a voice credit. Bangumi's importer files
// the character's appearance kind (主角/配角/客串) in note, which is not a
// version and must not be shown as one.
func VoiceNoteSQL(alias string) string {
	return fmt.Sprintf(`CASE WHEN %[1]s.source_id = %[2]d THEN '' ELSE btrim(%[1]s.note) END`, alias, model.SourceBangumi)
}

func int16List(ids []int16) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprint(id)
	}
	return strings.Join(parts, ", ")
}

func quotedList(ss []string) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return strings.Join(parts, ", ")
}
