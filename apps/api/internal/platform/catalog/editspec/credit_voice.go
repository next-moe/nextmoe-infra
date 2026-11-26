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
// not suppressed, and not a voice outranked by a more credited one. VNDB and
// the human lanes outrank ErogameScape, which outranks Bangumi.
//
// Bangumi names a voice actress by her main name; VNDB and ErogameScape keep
// the name she works under on adult titles, usually a separate stage name. Work 2452 showed 桜咲千依 (Bangumi) beside 藤咲ウサ (VNDB) as two
// voices of one character, and 32,739 Bangumi rows on prod did the same. When a
// credited source voices the character on the original release, the Bangumi
// row stays in the table but is not shown on any public face, including the
// main name's own credit list.
//
// ErogameScape names the actress's main stage name, often with her other
// names in brackets, where VNDB keeps the one this release credits: the same
// work then showed 美月 beside 美月(声優) and 木ノ下 とまり beside
// 木ノ下とまり(木の下とまり), 978 such pairs on prod, 809 of them one person or
// one name. ErogameScape's row yields to VNDB's the way Bangumi's yields to
// both.
func ShownCreditSQL(alias string) string {
	return NotSuppressedCreditSQL(alias) + " AND " + notOutvoicedSQL(alias)
}

// ov.id <> alias.id changes no answer (the two rows differ in source), but it
// keeps the planner from rewriting the probe as a hashed subplan: on a credit
// name with 4,600 credits it hashed every credited voice row on prod and took
// 830 ms, against 9 ms probing idx_catalog_credit_credited_voice.
func notOutvoicedSQL(alias string) string {
	return fmt.Sprintf(`NOT (%[1]s.source_id IN (%[2]d, %[6]d) AND %[1]s.character_id IS NOT NULL AND EXISTS (
			SELECT 1 FROM catalog_credit ov
			WHERE ov.work_id = %[1]s.work_id AND ov.character_id = %[1]s.character_id AND ov.role_id = %[1]s.role_id
			  AND ov.id <> %[1]s.id
			  AND ov.source_id IN (%[3]s) AND (ov.source_id <> %[6]d OR %[1]s.source_id = %[2]d)
			  AND (btrim(ov.note) = '' OR lower(btrim(ov.note)) IN (%[4]s))
			  AND %[5]s))`,
		alias, model.SourceBangumi, int16List(creditedVoiceSourceIDs), quotedList(originalVoiceNotes),
		NotSuppressedCreditSQL("ov"), model.SourceErogameScape)
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
