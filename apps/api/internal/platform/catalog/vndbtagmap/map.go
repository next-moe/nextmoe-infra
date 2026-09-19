package vndbtagmap

import (
	_ "embed"
	"regexp"
	"strings"
	"sync"
)

//go:embed tagMap.ts
var tagMapTS []byte

var (
	tagMapWrapRegex = regexp.MustCompile(`:[ \t]*\r?\n[ \t]*'`)
	tagMapLineRegex = regexp.MustCompile(`^\s*(?:'([^']+)'|"([^"]+)"|([\pL\pN_][\pL\pN_\s./()\-]*[\pL\pN_]|[\pL\pN_]))\s*:\s*'([^']+)'`)
)

func Parse(data []byte) map[string]string {
	joined := tagMapWrapRegex.ReplaceAllString(string(data), ": '")
	result := make(map[string]string)
	for _, line := range strings.Split(joined, "\n") {
		m := tagMapLineRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		if key == "" {
			key = m[2]
		}
		if key == "" {
			key = m[3]
		}
		if value := m[4]; key != "" && value != "" {
			result[key] = value
		}
	}
	return result
}

var (
	embeddedOnce sync.Once
	embedded     map[string]string
)

func Embedded() map[string]string {
	embeddedOnce.Do(func() {
		embedded = Parse(tagMapTS)
	})
	return embedded
}
