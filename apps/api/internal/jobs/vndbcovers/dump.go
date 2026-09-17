package vndbcovers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const vndbImageBase = "https://t.vndb.org/"

func vndbImageRel(id string) (string, bool) {
	if len(id) < 3 {
		return "", false
	}
	prefix, num := id[:2], id[2:]
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 || strings.ToLower(prefix) != prefix {
		return "", false
	}
	return fmt.Sprintf("%s/%02d/%d.jpg", prefix, n%100, n), true
}

// vn.image is only the cover an editor pinned: on 2026-09-17 it was empty for
// all 994 cover candidates the dump knew, while c_image, the cover VNDB serves
// (falling back to a release image), named one for 162 of them. The dump's
// ratings are averages scaled by 100; the API reports the same average as 0-2.
func loadDumpImages(ctx context.Context, db *gorm.DB, vids []string) (map[string]*vnImage, int, error) {
	out := make(map[string]*vnImage, len(vids))
	unrated := 0
	for chunk := range slices.Chunk(vids, 5000) {
		var rows []struct {
			ID       string `gorm:"column:id"`
			Image    string `gorm:"column:image"`
			Known    bool   `gorm:"column:known"`
			Width    int    `gorm:"column:width"`
			Height   int    `gorm:"column:height"`
			Votes    int    `gorm:"column:votes"`
			Sexual   int    `gorm:"column:sexual"`
			Violence int    `gorm:"column:violence"`
		}
		if err := db.WithContext(ctx).Raw(`
			SELECT v.id, btrim(v.c_image) AS image, i.id IS NOT NULL AS known,
			       coalesce(i.width, 0) AS width, coalesce(i.height, 0) AS height,
			       coalesce(i.c_votecount, 0) AS votes,
			       coalesce(i.c_sexual_avg, 0) AS sexual, coalesce(i.c_violence_avg, 0) AS violence
			FROM src_vndb.vn v
			LEFT JOIN src_vndb.images i ON i.id = btrim(v.c_image)
			WHERE v.id IN ?`, chunk).Scan(&rows).Error; err != nil {
			return nil, 0, fmt.Errorf("read vndb dump images: %w", err)
		}
		for _, r := range rows {
			rel, ok := vndbImageRel(r.Image)
			switch {
			case r.Image == "":
				out[r.ID] = nil
			case !ok || !r.Known:
			case r.Votes == 0:
				unrated++
			default:
				out[r.ID] = &vnImage{
					URL:      vndbImageBase + rel,
					Dims:     []int{r.Width, r.Height},
					Sexual:   float64(r.Sexual) / 100,
					Violence: float64(r.Violence) / 100,
				}
			}
		}
	}
	return out, unrated, nil
}

func mirrorRel(src string) (string, bool) {
	rel, ok := strings.CutPrefix(src, vndbImageBase)
	return rel, ok && rel != ""
}

func writeFilesOut(path, imageDir string, plan []planRow) (int, error) {
	var missing []string
	for _, row := range plan {
		if !row.actionable() {
			continue
		}
		rel, ok := mirrorRel(row.Img.URL)
		if !ok {
			continue
		}
		if st, err := os.Stat(filepath.Join(imageDir, filepath.FromSlash(rel))); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			continue
		}
		missing = append(missing, rel)
	}
	slices.Sort(missing)
	missing = slices.Compact(missing)
	var b strings.Builder
	for _, rel := range missing {
		b.WriteString(rel)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return 0, err
	}
	return len(missing), nil
}
