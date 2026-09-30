package symbolicate

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

var identRe = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

var privateSuffixRe = regexp.MustCompile(`@[0-9]+$`)

func ParseObfuscationMap(r io.Reader) (map[string]string, error) {
	var arr []string
	if err := json.NewDecoder(r).Decode(&arr); err != nil {
		return nil, err
	}
	return ObfuscationPairs(arr), nil
}

// Flutter writes [original, obfuscated, original, obfuscated, ...]; phase 2b
// first read the pairs the other way round and resolved nothing. Library
// private names carry an @<library hash> suffix in the map that runtime type
// names do not print.
func ObfuscationPairs(arr []string) map[string]string {
	out := make(map[string]string, len(arr)/2)
	for i := 0; i+1 < len(arr); i += 2 {
		orig := privateSuffixRe.ReplaceAllString(arr[i], "")
		obf := privateSuffixRe.ReplaceAllString(arr[i+1], "")
		if obf == "" || orig == "" {
			continue
		}
		out[obf] = orig
	}
	return out
}

func DeobfuscateType(s string, m map[string]string) string {
	if len(m) == 0 || s == "" {
		return s
	}
	return identRe.ReplaceAllStringFunc(s, func(tok string) string {
		if orig, ok := m[tok]; ok && orig != "" {
			return orig
		}
		return tok
	})
}

func DeobfuscateMessage(s string, m map[string]string) string {
	if len(m) == 0 || s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] != '\'' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i + 1
		for j < len(s) && s[j] != '\'' {
			j++
		}
		if j >= len(s) {
			b.WriteString(s[i:])
			break
		}
		inner := s[i+1 : j]
		b.WriteByte('\'')
		b.WriteString(DeobfuscateType(inner, m))
		b.WriteByte('\'')
		i = j + 1
	}
	return b.String()
}
