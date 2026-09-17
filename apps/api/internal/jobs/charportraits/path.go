package charportraits

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const chPrefix = "ch"

func chRelPath(id string) (string, error) {
	num := strings.TrimPrefix(id, chPrefix)
	n, err := strconv.Atoi(num)
	if err != nil {
		return "", fmt.Errorf("bad character image id %q", id)
	}
	return fmt.Sprintf("ch/%02d/%s.jpg", n%100, num), nil
}

type mirrorForecast struct {
	hasHash, present, badID int
	missing                 []string
}

func forecastMirror(cands []candidate, dir string) mirrorForecast {
	var f mirrorForecast
	seen := map[string]bool{}
	for _, c := range cands {
		if c.ImageHash != nil && *c.ImageHash != "" {
			f.hasHash++
			continue
		}
		rel, err := chRelPath(c.ImageID)
		if err != nil {
			f.badID++
			continue
		}
		if fileExists(filepath.Join(dir, filepath.FromSlash(rel))) {
			f.present++
			continue
		}
		if !seen[rel] {
			seen[rel] = true
			f.missing = append(f.missing, rel)
		}
	}
	slices.Sort(f.missing)
	return f
}

func (f mirrorForecast) missingList() string {
	var b strings.Builder
	for _, rel := range f.missing {
		b.WriteString(rel)
		b.WriteByte('\n')
	}
	return b.String()
}
