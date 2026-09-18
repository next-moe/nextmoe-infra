package llmsuggest

import (
	"sort"
	"strings"
	"unicode/utf8"

	"api/internal/platform/catalog/titlekey"

	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

// titlekey.Strip also removes リマスター / リメイク / 完全版 / HDリマスター /
// 総集編 / 完結編 / 前編 / 後編 / 上巻 / 下巻. VNDB keeps several of those as
// separate works: on 2026-09-17 a judge called Ever17 REMASTER the same work
// as Ever17 at 0.90; VNDB lists the remaster as its own VN, v19373 against v17.
var remakeVolumePhrases = []string{
	"リマスター",
	"リメイク",
	"完全版",
	"HDリマスター",
	"総集編",
	"完結編",
	"前編",
	"後編",
	"上巻",
	"下巻",
}

func attachStrippedKeyEvidence(rows []QueueVerdict, out map[int64]pairEvidence, corpus []workNameRow, display map[int64]string) {
	holders := indexStrippedHolders(corpus)
	for _, r := range rows {
		key := StrippedKey(display[r.AID], display[r.BID], holders)
		if key == "" {
			continue
		}
		ev := out[r.ID]
		ev.StrippedName = key
		ev.StrippedHolders = len(holders[key])
		out[r.ID] = ev
	}
}

func LoadStrippedHolders(db *gorm.DB) (map[string]map[int64]struct{}, error) {
	corpus, err := loadWorkNameCorpus(db)
	if err != nil {
		return nil, err
	}
	return indexStrippedHolders(corpus), nil
}

func indexStrippedHolders(corpus []workNameRow) map[string]map[int64]struct{} {
	holders := map[string]map[int64]struct{}{}
	for _, row := range corpus {
		for _, k := range titlekey.Keys(row.Raw) {
			addKeyHolder(holders, k, row.WorkID)
		}
	}
	return holders
}

func StrippedKey(displayA, displayB string, holders map[string]map[int64]struct{}) string {
	if strippedEditionConflict(displayA, displayB) {
		return ""
	}
	inB := map[string]struct{}{}
	for _, k := range titlekey.Keys(displayB) {
		inB[k] = struct{}{}
	}
	var counting, shared []string
	for _, k := range titlekey.Keys(displayA) {
		if _, ok := inB[k]; !ok {
			continue
		}
		if utf8.RuneCountInString(k) < looseNameMinRunes {
			continue
		}
		shared = append(shared, k)
		if len(holders[k]) == exclusiveNameHolders {
			counting = append(counting, k)
		}
	}
	if len(counting) > 0 {
		return longestKey(counting)
	}
	if len(shared) == 0 {
		return ""
	}
	return longestKey(shared)
}

func longestKey(keys []string) string {
	sort.Slice(keys, func(i, j int) bool {
		ra, rb := utf8.RuneCountInString(keys[i]), utf8.RuneCountInString(keys[j])
		if ra != rb {
			return ra > rb
		}
		return keys[i] < keys[j]
	})
	return keys[0]
}

func strippedEditionConflict(a, b string) bool {
	na := strings.ToLower(norm.NFKC.String(a))
	nb := strings.ToLower(norm.NFKC.String(b))
	for _, p := range remakeVolumePhrases {
		pl := strings.ToLower(p)
		if strings.Contains(na, pl) != strings.Contains(nb, pl) {
			return true
		}
	}
	return false
}
