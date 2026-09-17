package importer

import (
	"math/bits"
	"math/rand"
	"strings"
	"unicode"

	"api/internal/platform/catalog/service"
)

func tallySignals(st *BgmGatedStats, p, t, x bool) {
	if p {
		st.SigP++
	}
	if t {
		st.SigT++
	}
	if x {
		st.SigX++
	}
	switch {
	case p && t && x:
		st.All3++
	}
	if p && t {
		st.PT++
	}
	if p && x {
		st.PX++
	}
	if t && x {
		st.TX++
	}
	if p && !t && !x {
		st.POnly++
	}
	if t && !p && !x {
		st.TOnly++
	}
	if x && !p && !t {
		st.XOnly++
	}
}

func collide(r poolRow, wt map[string]wtNorm) (BgmGatedCollision, bool) {
	n, w, ok := firstCorpusHit(wt, r.NameNorm, r.NameCNNorm)
	if !ok {
		return BgmGatedCollision{}, false
	}
	return BgmGatedCollision{
		SubjectID: r.ID, Name: r.Name, NameCN: r.NameCN,
		CollidedNorm: n, WorkID: w.workID, WorkTitle: w.title,
	}, true
}

func firstCorpusHit(wt map[string]wtNorm, norms ...string) (string, wtNorm, bool) {
	for _, n := range norms {
		for _, key := range gateKeys(n) {
			if w, hit := wt[key]; hit {
				return n, w, true
			}
		}
	}
	return "", wtNorm{}, false
}

// gateKeys lists the comparison keys a norm is filed and looked up under: the
// space-folded key the mint guard also uses, and a punctuation-blind key. One
// map holds both; a hit on either means the two titles differ only in runes
// the loose key drops.
func gateKeys(norm string) []string {
	var keys []string
	if k, ok := foldedGateKey(norm); ok {
		keys = append(keys, k)
	}
	if k := looseGateKey(norm); service.WorkDupeNormEligible(k) && (len(keys) == 0 || keys[0] != k) {
		keys = append(keys, k)
	}
	return keys
}

// looseGateKey keeps only letters and digits. The gate is allowed to be wider
// than service.WorkTitleFoldSQL, never narrower: a false hit costs a quarantine
// that the work-pair judge releases, a miss mints a live twin. On 2026-09-16 a
// 30-work canary of this gate minted 9 twins that differed from existing works
// only in 〜 against ～ inside the title or in －/-/− around a subtitle.
func looseGateKey(norm string) string {
	var b strings.Builder
	b.Grow(len(norm))
	for _, r := range norm {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// foldedGateKey reports the space-folded comparison key for a source norm, and
// whether it is long enough to compare on at all. Both length gates matter: the
// raw one keeps the pre-fold behaviour, the folded one stops "A B C" from
// becoming a 3-rune key that collides by genre rather than identity.
func foldedGateKey(norm string) (string, bool) {
	if !service.WorkDupeNormEligible(norm) {
		return "", false
	}
	folded := foldSpace(norm)
	if !service.WorkDupeNormEligible(folded) {
		return "", false
	}
	return folded, true
}

// foldSpace mirrors service.WorkTitleFoldSQL in Go; it must strip at least the
// runes that one strips, or the gate misses a duplicate the mint guard sees.
// That includes the trailing wave-dash run: bangumi ships a subtitle without the
// closing delimiter the other sources keep, and while only the SQL side stripped
// it this gate happily minted a live twin of a work it was built to catch.
func foldSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), "~～〜")
}

func dropIntraCollisions(cands []candidate, st *BgmGatedStats) []candidate {
	subjectsPerNorm := make(map[string]map[int64]struct{})
	note := func(norm string, id int64) {
		for _, key := range gateKeys(norm) {
			if subjectsPerNorm[key] == nil {
				subjectsPerNorm[key] = make(map[int64]struct{})
			}
			subjectsPerNorm[key][id] = struct{}{}
		}
	}
	for _, c := range cands {
		note(c.row.NameNorm, c.row.ID)
		note(c.row.NameCNNorm, c.row.ID)
	}
	dupNorm := func(norm string) bool {
		for _, key := range gateKeys(norm) {
			if len(subjectsPerNorm[key]) > 1 {
				return true
			}
		}
		return false
	}
	out := cands[:0]
	for _, c := range cands {
		if dupNorm(c.row.NameNorm) || dupNorm(c.row.NameCNNorm) {
			st.SkippedIntraCollision++
			continue
		}
		out = append(out, c)
	}
	return out
}

func pickRandomSample(cands []candidate) []BgmGatedSample {
	idx := make([]int, len(cands))
	for i := range idx {
		idx[i] = i
	}
	rng := rand.New(rand.NewSource(bgmGatedSampleSeed))
	rng.Shuffle(len(idx), func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
	n := min(bgmGatedSampleN, len(idx))
	out := make([]BgmGatedSample, 0, n)
	for _, i := range idx[:n] {
		c := cands[i]
		out = append(out, BgmGatedSample{SubjectID: c.row.ID, Name: c.row.Name, NameCN: c.row.NameCN, Signals: c.signals})
	}
	return out
}

func signalString(p, t, x bool) string {
	var parts []string
	if p {
		parts = append(parts, "P")
	}
	if t {
		parts = append(parts, "T")
	}
	if x {
		parts = append(parts, "X")
	}
	out := ""
	for i, s := range parts {
		if i > 0 {
			out += "+"
		}
		out += s
	}
	return out
}

func corpusCount(mask uint8) int { return bits.OnesCount8(mask) }

func pureASCIIHits(r poolRow, nMask, cnMask uint8) bool {
	hit := false
	if nMask != 0 {
		if hasCJK(r.NameNorm) {
			return false
		}
		hit = true
	}
	if cnMask != 0 {
		if hasCJK(r.NameCNNorm) {
			return false
		}
		hit = true
	}
	return hit
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			return true
		}
	}
	return false
}

func bgmSampleOf(r poolRow, signals string) BgmGatedSample {
	return BgmGatedSample{SubjectID: r.ID, Name: r.Name, NameCN: r.NameCN, Signals: signals}
}

func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
