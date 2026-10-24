package textsafety

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type reportIntro struct {
	WorkID     int64    `gorm:"column:work_id" json:"-"`
	Lang       string   `gorm:"column:lang" json:"lang"`
	Intro      string   `gorm:"column:intro" json:"intro"`
	Mode       *int16   `gorm:"column:sfw_mode" json:"-"`
	SFWIntro   string   `gorm:"column:sfw_intro" json:"sfw_intro,omitempty"`
	Verdict    *int16   `gorm:"column:verdict" json:"-"`
	Confidence *float32 `gorm:"column:confidence" json:"confidence,omitempty"`
	ModeKey    string   `gorm:"-" json:"mode"`
	ClassKey   string   `gorm:"-" json:"class,omitempty"`
}

// Report writes the shadow numbers of an apply and review samples, without
// writing to the database.
func Report(ctx context.Context, db *gorm.DB, dir string, n int) error {
	p, err := planApply(ctx, db)
	if err != nil {
		return err
	}
	var intros []reportIntro
	if err := db.WithContext(ctx).Raw(`
		SELECT i.work_id, i.lang, i.intro, s.sfw_mode, coalesce(s.sfw_intro, '') AS sfw_intro, s.verdict, s.confidence
		FROM catalog_work_intro i
		JOIN catalog_work w ON w.id = i.work_id
		LEFT JOIN catalog_work_intro_safety s ON s.intro_hash = md5(i.intro)
		WHERE `+galgameLiveSQL+` ORDER BY i.work_id, i.lang`, model.WorkStatusLive).Scan(&intros).Error; err != nil {
		return fmt.Errorf("load intros: %w", err)
	}
	byWork := map[int64][]reportIntro{}
	for i := range intros {
		in := &intros[i]
		in.ModeKey = "unjudged"
		if in.Mode != nil {
			in.ModeKey = classKey(sfwModes, *in.Mode)
			in.ClassKey = classKey(introClasses, *in.Verdict)
		}
		byWork[in.WorkID] = append(byWork[in.WorkID], *in)
	}

	modes := map[string]int{}
	introsByMode := map[string][]reportIntro{}
	sfwWorks, sfwWorksNoIntro := 0, 0
	for id, w := range p.works {
		if w.nsfw() {
			continue
		}
		sfwWorks++
		shown := 0
		for _, in := range byWork[id] {
			modes[in.ModeKey]++
			introsByMode[in.ModeKey] = append(introsByMode[in.ModeKey], in)
			if in.ModeKey != "dropped" && in.ModeKey != "unjudged" {
				shown++
			}
		}
		if shown == 0 {
			sfwWorksNoIntro++
		}
	}

	var toSFW, toNSFW []map[string]any
	for _, c := range p.changes {
		row := map[string]any{
			"id": c.after.ID, "names": c.titles.Names, "title_class": classKey(titleClasses, c.verdict.Verdict),
			"title_confidence": c.verdict.Confidence, "claimed": c.after.claimed(),
			"r18": c.after.ContentRating == model.ContentRatingR18, "intros": byWork[c.after.ID],
		}
		switch {
		case c.before.nsfw() && !c.after.nsfw():
			toSFW = append(toSFW, row)
		case !c.before.nsfw() && c.after.nsfw():
			toNSFW = append(toNSFW, row)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(7, 11))
	if err := writeSample(filepath.Join(dir, "to_sfw.jsonl"), toSFW, n, rng); err != nil {
		return err
	}
	if err := writeSample(filepath.Join(dir, "to_nsfw.jsonl"), toNSFW, n, rng); err != nil {
		return err
	}
	for _, m := range []string{"redacted", "rewritten", "dropped"} {
		if err := writeSample(filepath.Join(dir, m+".jsonl"), introsByMode[m], n, rng); err != nil {
			return err
		}
	}

	var b strings.Builder
	st := p.stats
	fmt.Fprintf(&b, "galgame live works: %d (title verdicts for %d)\n", st.Works, st.Judged)
	fmt.Fprintf(&b, "nsfw shelf: %d -> %d (to sfw %d, to nsfw %d)\n", st.ShelfNSFWBefore, st.ShelfNSFWAfter, st.ToSFW, st.ToNSFW)
	fmt.Fprintf(&b, "writes: title_nsfw %d, display_nsfw (claimed, non-human) %d\n", st.TitleChanged, st.DisplayChanged)
	fmt.Fprintf(&b, "sfw shelf after: %d works, %d of them with no intro to show\n", sfwWorks, sfwWorksNoIntro)
	keys := make([]string, 0, len(modes))
	for k := range modes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  sfw-shelf intros %-9s %d\n", k, modes[k])
	}
	fmt.Print(b.String())
	return os.WriteFile(filepath.Join(dir, "summary.txt"), []byte(b.String()), 0o644)
}

func writeSample[T any](path string, rows []T, n int, rng *rand.Rand) error {
	rng.Shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, r := range rows[:min(n, len(rows))] {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}
