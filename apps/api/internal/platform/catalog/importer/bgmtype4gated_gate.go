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
		folded, ok := foldedGateKey(n)
		if !ok {
			continue
		}
		if w, hit := wt[folded]; hit {
			return n, w, true
		}
	}
	return "", wtNorm{}, false
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

// foldSpace mirrors service.WorkTitleFoldSQL in Go; the two must strip the same
// runes or the mint guard and the import gate disagree about what a duplicate is.
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
		folded, ok := foldedGateKey(norm)
		if !ok {
			return
		}
		if subjectsPerNorm[folded] == nil {
			subjectsPerNorm[folded] = make(map[int64]struct{})
		}
		subjectsPerNorm[folded][id] = struct{}{}
	}
	for _, c := range cands {
		note(c.row.NameNorm, c.row.ID)
		note(c.row.NameCNNorm, c.row.ID)
	}
	dupNorm := func(norm string) bool {
		folded, ok := foldedGateKey(norm)
		return ok && len(subjectsPerNorm[folded]) > 1
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
