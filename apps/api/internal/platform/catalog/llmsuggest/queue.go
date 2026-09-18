package llmsuggest

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	matchedByVNDBReleaseBackfill = "rule:vndb-release-backfill"
	matchedByEGDMM               = "rule:eg-dmm"
	matchedByEGSteam             = "rule:eg-steam"
	matchedByHLTBSteam           = "rule:hltb-steam"
	matchedByBgmTitleOnly        = "rule:bgm-title-only"
	matchedByTitleYearStrict     = "rule:title-year-strict"
	matchedByCurated             = "curated"

	sourceKeyVNDB    = "vndb"
	sourceKeyBangumi = "bangumi"
	sourceKeyDLsite  = "dlsite"
	sourceKeyEG      = "erogamescape"
	sourceKeySteam   = "steam"
	sourceKeyDMM     = "dmm"
	sourceKeyHLTB    = "howlongtobeat"
)

// Every clause below answers something measured on the 693 pairs v1 left
// undecided on 2026-09-15, and none of it is a guess about what might help.
//
//   - 579 of 693 have a release year on exactly one side and 570 a label on
//     exactly one side, and v1's reasons read those as "conflicting year" and
//     "different labels". An omission is not a disagreement.
//   - 243 differ in olang. The catalog models a localisation as more titles on
//     one work, not as a second work: 11,065 live works hold both a non-machine
//     ja and zh title, and 605 of 5,426 executed merges joined two sides whose
//     olang differed.
//   - works_holding is new evidence, not a restatement. 131 of these pairs share
//     an identifier no other live work holds, while twitter:frontwingint is held
//     by 43 works. v1 was shown neither number and wrote "no shared refs" about
//     pairs sharing an exclusive product page.
//
// The last clause of v1 -- answer "unsure" when evidence is thin -- was the exit
// 359 of them took while the reason field had already argued its way to a
// conclusion. Closing an exit without giving the model the missing facts only
// buys confident hallucination, so it is narrowed here, not removed.
//
// The narrowing is symmetric on purpose. Requiring a named discriminator for
// DIFFERENT while letting SAME rest on nothing would have aimed the whole
// change at one verdict: 500 of the 693 hold anchors in registries that do not
// overlap, so no contradiction is findable and the apply-stage ref screen
// cannot veto anything there. Same-titled Western VNs -- Alone, Again, Memoria,
// Stay With Me -- are real distinct works that land in exactly that shape.
//
// A v3 that named the one missing category -- parts of a release -- was written,
// measured and reverted. It added "one part of a multi-part release is
// DIFFERENT" plus a rule that an extra title segment decides by its type.
// Replayed over the 680 stored dossiers it caught 3 more series pairs and
// promoted 314 of the 502 pairs v2 had refused into accepts at conf>=0.9:
// "Blackish House" and "Blackish House ←sideZ" at 0.95, reasoned as a side-story
// edition of one work. Naming the discriminators turned their absence into
// corroboration -- 112 of those 314 reasons cite finding no part marker, and
// pairs v2 left at unsure/0.60 came back same/1.00. Do not hand this judge a
// checklist of discriminators unless the empty result also has a verdict.
const workPairSystem = "You are a meticulous visual-novel catalog deduplication expert. " +
	"Given two catalog work records and their evidence dossiers, decide whether they describe the SAME work. " +
	"SAME means the two records describe the same work. This catalog models editions, ports, re-releases and translated localisations of one work as ONE work carrying several titles, so a Japanese original and its Chinese or English localisation are the SAME work even when olang, title language and release year differ; a later year on one side is normal for a localisation or a re-release. " +
	"DIFFERENT covers sequels, prequels, fandiscs, remakes, spin-offs, and separate works that merely share a title. Say which discriminator you found: a DIFFERENT answer must rest on something both dossiers state, never on a field one of them simply omits. " +
	"A value present on one side and absent on the other is MISSING, not conflicting; an absent year, label or ref is not evidence of anything. " +
	"shared_refs lists identifiers both records hold, and works_holding is how many live works in the whole catalog hold that identifier. works_holding=2 means the identifier belongs to this pair alone and is strong evidence of SAME; a large works_holding marks a studio account or brand root that every title of a publisher carries and is worth nothing. A shared identifier never outranks a discriminator you can name, because entries in one series do share a product page. " +
	"An identical vndb or bangumi id on both sides is near-conclusive for SAME; two different ids from that same registry are near-conclusive for DIFFERENT. " +
	"Answer SAME only on a positive agreement you can point at -- a shared identifier with a small works_holding, titles that match once language is set aside, or fields that agree -- and never on the mere absence of a discriminator; answer \"unsure\" when you have neither that agreement nor a discriminator. Keep the reason to one short clause."

// The dossier used to carry bare enum codes and the judge decided on the
// meanings it invented for them: "catalog entity is a work (type 3)" -- 3 is
// Label -- and "external record is a character (type 2)" for a bangumi person
// record, where 2 is company. Naming the categories in the prompt was not
// enough, because nothing said which code was which.
const refSystem = "You are a meticulous visual-novel catalog linking expert. " +
	"Decide whether the external source record and the catalog entity denote the same work, label, character, or person. " +
	"SAME means they are the same identity; DIFFERENT means they are distinct even if names or titles look similar. " +
	"entity_type and every type field are spelled out in the dossier -- use those words and do not reinterpret them; " +
	"a Label is a publisher, and a source record for a company is the right kind of record to match one. " +
	"The rule named in matched_by already compared the two names, so a name match on its own is where you start, " +
	"not the evidence: decide on what each side publishes -- works, sample_works, already_linked -- and answer SAME " +
	"with high confidence when those agree. " +
	"Answer \"unsure\" when evidence is thin. Keep the reason to one short clause."

func goldQueue(q string) string { return q + "-gold" }

func isGoldQueue(q string) bool { return strings.HasSuffix(q, "-gold") }

func isLiveQueue(q string) bool {
	switch q {
	case QueueCreditName, QueueWorkPair, QueueRef:
		return true
	default:
		return false
	}
}

func familiesWant(families string) (chain, llm bool) {
	switch families {
	case FamiliesLLM:
		return false, true
	case FamiliesChain:
		return true, false
	default:
		return true, true
	}
}

func dryLimit(limit int) int {
	if limit <= 0 {
		return 5
	}
	return limit
}

func creditNameHash(aID, bID int64, aName, bName string) string {
	return hashInput("queue-creditname", strconv.FormatInt(aID, 10), strconv.FormatInt(bID, 10), aName, bName)
}

func workPairHash(aID, bID int64) string {
	return hashInput("queue-workpair", strconv.FormatInt(aID, 10), strconv.FormatInt(bID, 10))
}

func refInputHash(entityType int16, entityID int64, sourceID int16, externalID string) string {
	return hashInput("queue-refs",
		strconv.FormatInt(int64(entityType), 10),
		strconv.FormatInt(entityID, 10),
		strconv.FormatInt(int64(sourceID), 10),
		externalID)
}

func applyNote(id int64, conf float64) string {
	return fmt.Sprintf("llm:queue-adjudicator %d conf=%.2f", id, conf)
}

func persistQueueVerdict(db *gorm.DB, row *QueueVerdict) {
	err := upsertJudgement(db, row, "queue_verdict",
		[]string{"queue", "input_hash", "model", "prompt_version"},
		[]string{"lane", "entity_type", "a_id", "b_id", "entity_id", "source_id", "external_id",
			"verdict", "reason", "confidence", "evidence", "error", "created_at"})
	if err != nil {
		slog.Error("persist queue verdict", "error", err, "queue", row.Queue, "hash", row.InputHash)
	}
}

// upsertJudgement overwrites a stored FAILURE with the answer a retry produced.
// A plain insert loses that retry to the unique key after the model call has
// already been paid for, and the caller only logs it -- which is how the 429
// storm of 2026-08/09 would have survived its own fix. The DO UPDATE is guarded
// on the stored row still being a failure, so a verdict that has already been
// judged is never re-written, and neither is one the apply step has stamped.
func upsertJudgement(db *gorm.DB, row any, table string, conflict, update []string) error {
	cols := make([]clause.Column, len(conflict))
	for i, c := range conflict {
		cols[i] = clause.Column{Name: c}
	}
	// The guard names the table: bare "error" in a DO UPDATE ... WHERE is
	// ambiguous against excluded and Postgres rejects the statement outright.
	stored := clause.Column{Table: table, Name: "error"}
	return db.Clauses(clause.OnConflict{
		Columns:   cols,
		Where:     clause.Where{Exprs: []clause.Expression{clause.Neq{Column: stored, Value: ""}}},
		DoUpdates: clause.AssignmentColumns(update),
	}).Create(row).Error
}

func evidenceJSON(v any) datatypes.JSON {
	b, err := json.Marshal(v)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(b)
}

func capN[T any](s []T, n int) []T {
	if n > 0 && len(s) > n {
		return s[:n]
	}
	return s
}

func chunkBy[T any](in []T, size int) [][]T {
	if size < 1 {
		size = 500
	}
	var out [][]T
	for len(in) > size {
		out = append(out, in[:size])
		in = in[size:]
	}
	if len(in) > 0 {
		out = append(out, in)
	}
	return out
}

type tally struct {
	mu sync.Mutex
	n  map[string]int
}

func (t *tally) add(key string, d int) {
	t.mu.Lock()
	if t.n == nil {
		t.n = map[string]int{}
	}
	t.n[key] += d
	t.mu.Unlock()
}

func (t *tally) snapshot() map[string]int {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[string]int{}
	for k, v := range t.n {
		out[k] = v
	}
	return out
}

type sourceReg struct {
	idByKey map[string]int16
	keyByID map[int16]string
}

func loadSourceReg(db *gorm.DB) (sourceReg, error) {
	var rows []struct {
		ID  int16  `gorm:"column:id"`
		Key string `gorm:"column:key"`
	}
	if err := db.Raw(`SELECT id, key FROM catalog_source`).Scan(&rows).Error; err != nil {
		return sourceReg{}, err
	}
	r := sourceReg{idByKey: map[string]int16{}, keyByID: map[int16]string{}}
	for _, row := range rows {
		r.idByKey[row.Key] = row.ID
		r.keyByID[row.ID] = row.Key
	}
	return r, nil
}

func (r sourceReg) id(key string) int16 { return r.idByKey[key] }

func (r sourceReg) key(id int16) string { return r.keyByID[id] }

func siteClaimed(site *string) bool {
	return site != nil && strings.TrimSpace(*site) != ""
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func chainFamily(matchedBy string) bool {
	switch matchedBy {
	case matchedByVNDBReleaseBackfill, matchedByEGDMM, matchedByEGSteam, matchedByHLTBSteam:
		return true
	default:
		return false
	}
}

// A ref the LLM lane can judge is one whose entity has a dossier builder. This
// used to be a whitelist of two matched_by rules for works, which refused 128
// work refs the builders already covered -- bgm-type4-gated, wiki-bid-typed,
// eg-vndb-rosetta and four more -- and reported them as skipped_unknown_family,
// a counter that reads as "nothing here to judge". The rule that proposed a
// link says nothing about whether the evidence for checking it exists.
//
// Release stays out on purpose: it has no dossier builder, so the 348 queued
// vndb release imports would be judged on a work-level record that cannot
// decide which release it is.
func llmFamily(entityType int16) bool {
	switch entityType {
	case model.EntityTypeWork, model.EntityTypeLabel, model.EntityTypeCharacter, model.EntityTypeCreditName:
		return true
	default:
		return false
	}
}

func normVNDBID(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "v") {
		return s
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return s
		}
	}
	return "v" + s
}
