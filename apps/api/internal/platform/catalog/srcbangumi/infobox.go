package srcbangumi

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"api/internal/platform/catalog/bangumiwiki"

	"golang.org/x/text/unicode/norm"
)

func DecodeInfobox(raw []byte) (bangumiwiki.Infobox, bool) {
	var box bangumiwiki.Infobox
	if len(raw) == 0 || json.Unmarshal(raw, &box) != nil {
		return bangumiwiki.Infobox{}, false
	}
	return box, true
}

func FieldValues(f bangumiwiki.Field) []string {
	var out []string
	if v := strings.TrimSpace(f.Value); v != "" {
		out = append(out, v)
	}
	for _, it := range f.Items {
		if v := strings.TrimSpace(it.Value); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func InfoboxValues(raw []byte, key string) []string {
	box, ok := DecodeInfobox(raw)
	if !ok {
		return nil
	}
	var out []string
	for _, f := range box.Fields {
		if strings.TrimSpace(f.Key) == key {
			out = append(out, FieldValues(f)...)
		}
	}
	return out
}

var (
	steamAppURL   = regexp.MustCompile(`(?i)(?:store\.steampowered\.com|steamcommunity\.com|steamdb\.info)/app/([0-9]+)`)
	steamBareID   = regexp.MustCompile(`^([0-9]{2,9})(?:$|[\s(（])`)
	steamFieldKey = regexp.MustCompile(`(?i)^steam`)
)

func SteamAppIDs(raw []byte) []string {
	box, ok := DecodeInfobox(raw)
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	for _, f := range box.Fields {
		steamKey := steamFieldKey.MatchString(norm.NFKC.String(strings.TrimSpace(f.Key)))
		for _, v := range FieldValues(f) {
			for _, m := range steamAppURL.FindAllStringSubmatch(v, -1) {
				seen[m[1]] = struct{}{}
			}
			if steamKey {
				if m := steamBareID.FindStringSubmatch(v); m != nil {
					seen[m[1]] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
