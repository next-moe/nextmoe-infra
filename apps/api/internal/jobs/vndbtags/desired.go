package vndbtags

import "strings"

// Without the column tags GORM scans vid into nothing (it names the VID field
// v_id), every vote lands on the empty vid, and the first DB run planned to
// delete every VNDB tag on every work.
type Vote struct {
	Tag     string `gorm:"column:tag"`
	VID     string `gorm:"column:vid"`
	Score   int16  `gorm:"column:score"`
	Spoiler *int16 `gorm:"column:spoiler"`
	Ignore  bool   `gorm:"column:ignore"`
	Lie     *bool  `gorm:"column:lie"`
}

type TagMeta struct {
	Name         string
	Alias        string
	Cat          string
	DefaultSpoil int16
}

type Desired struct {
	Spoiler int16
	Count   int
	Ero     bool
}

type agg struct {
	spoiler int16
	count   int
	ero     bool
}

func desiredTags(votes []Vote, tags map[string]TagMeta, tagMap map[string]string) map[string]Desired {
	grouped := map[string][]Vote{}
	for _, v := range votes {
		if v.Ignore {
			continue
		}
		grouped[v.Tag] = append(grouped[v.Tag], v)
	}
	merged := map[string]*agg{}
	for id, vs := range grouped {
		if len(vs) == 0 {
			continue
		}
		meta, ok := tags[id]
		if !ok {
			continue
		}
		var sum int64
		ns := 0
		var sSum float64
		nl, nt := 0, 0
		for _, v := range vs {
			sum += int64(v.Score)
			if v.Spoiler != nil {
				ns++
				sSum += float64(*v.Spoiler)
			}
			if v.Lie != nil {
				nl++
				if *v.Lie {
					nt++
				}
			}
		}
		rating := float64(sum) / float64(len(vs))
		lie := nt > 0 && nt*2 >= nl
		if rating < 1.0 || lie {
			continue
		}
		spoiler := meta.DefaultSpoil
		if ns > 0 {
			s := sSum / float64(ns)
			switch {
			case s > 1.3:
				spoiler = 2
			case s > 0.4:
				spoiler = 1
			default:
				spoiler = 0
			}
		}
		name := resolveName(meta, tagMap)
		count := len(vs)
		ero := meta.Cat == "ero"
		if cur, ok := merged[name]; ok {
			if spoiler < cur.spoiler {
				cur.spoiler = spoiler
			}
			if count > cur.count {
				cur.count = count
			}
			cur.ero = cur.ero || ero
			continue
		}
		merged[name] = &agg{spoiler: spoiler, count: count, ero: ero}
	}
	out := make(map[string]Desired, len(merged))
	for name, a := range merged {
		out[name] = Desired{Spoiler: a.spoiler, Count: a.count, Ero: a.ero}
	}
	return out
}

func resolveName(meta TagMeta, tagMap map[string]string) string {
	if mapped, ok := tagMap[meta.Name]; ok {
		return mapped
	}
	for _, alias := range strings.Split(meta.Alias, "\n") {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		if mapped, ok := tagMap[alias]; ok {
			return mapped
		}
	}
	return meta.Name
}
