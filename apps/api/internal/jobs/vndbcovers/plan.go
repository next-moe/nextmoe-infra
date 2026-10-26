package vndbcovers

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const userAgent = "nextmoe-infra/backfill-vndb-covers (+https://www.kungal.com)"

const maxRatingLevel = 2

func idFilter(ids []string) any {
	if len(ids) == 1 {
		return []any{"id", "=", ids[0]}
	}
	out := make([]any, 0, len(ids)+1)
	out = append(out, "or")
	for _, id := range ids {
		out = append(out, []any{"id", "=", id})
	}
	return out
}

func parseVNResponse(r io.Reader) (*vnResponse, error) {
	var out vnResponse
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode vndb response: %w", err)
	}
	return &out, nil
}

func RatingLevel(v float64) int16 {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	lvl := int16(math.Floor(v + 0.5))
	if lvl > maxRatingLevel {
		return maxRatingLevel
	}
	return lvl
}

func portrait(dims []int) bool {
	if len(dims) != 2 {
		return false
	}
	w, h := dims[0], dims[1]
	return w > 0 && h > 0 && h > w
}

func shapeLabel(dims []int) string {
	if len(dims) != 2 || dims[0] <= 0 || dims[1] <= 0 {
		return "unknown"
	}
	if portrait(dims) {
		return "portrait"
	}
	return "landscape"
}

type planRow struct {
	WorkID int64
	VNDBID string
	Img    *vnImage
	Reason string
}

func (p planRow) actionable() bool { return p.Img != nil && strings.TrimSpace(p.Img.URL) != "" }

func buildPlan(cands []candidate, images map[string]*vnImage, stats *Stats) []planRow {
	plan := make([]planRow, 0, len(cands))
	for _, c := range cands {
		row := planRow{WorkID: c.WorkID, VNDBID: c.VNDBID}
		img, known := images[c.VNDBID]
		switch {
		case !known:
			row.Reason = "vn-unknown"
		case img == nil || strings.TrimSpace(img.URL) == "":
			row.Reason = "no-image"
		default:
			row.Img = img
		}
		if !row.actionable() {
			stats.NoImage++
			plan = append(plan, row)
			continue
		}
		if portrait(row.Img.Dims) {
			stats.Portrait++
		} else {
			stats.Landscape++
		}
		stats.Planned++
		plan = append(plan, row)
	}
	return plan
}

func actionable(plan []planRow, limit int) []planRow {
	out := make([]planRow, 0, len(plan))
	for _, row := range plan {
		if !row.actionable() {
			continue
		}
		out = append(out, row)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

func anchorIDs(cands []candidate) []string {
	seen := make(map[string]bool, len(cands))
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		id := strings.TrimSpace(c.VNDBID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func ParseIDs(raw string) ([]int64, error) {
	var out []int64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("--ids: %q is not a work id", part)
		}
		out = append(out, id)
	}
	return out, nil
}
