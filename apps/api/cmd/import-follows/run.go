package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type options struct {
	Source     string
	Apply      bool
	PruneStale bool
	Batch      int
}

type report struct {
	Source         string
	SourceEdges    int
	SelfSkipped    int
	AlreadyPresent int
	ToInsert       int
	Inserted       int
	Raced          int
	Stale          int
	Pruned         int
	Applied        bool
}

func (r report) log() {
	slog.Info("import-follows",
		"source", r.Source,
		"source_edges", r.SourceEdges,
		"self_skipped", r.SelfSkipped,
		"already_present", r.AlreadyPresent,
		"to_insert", r.ToInsert,
		"inserted", r.Inserted,
		"raced", r.Raced,
		"stale", r.Stale,
		"pruned", r.Pruned,
		"applied", r.Applied)
}

type sourceEdge struct {
	FollowerID int64
	FolloweeID int64
	CreatedAt  *time.Time
}

type centralRow struct {
	FollowerID int64
	FolloweeID int64
	OriginSite string
	Imported   bool
}

func run(ctx context.Context, community, source *gorm.DB, opt options) (report, error) {
	if opt.Source != "moyu" && opt.Source != "letmoe" {
		return report{}, fmt.Errorf("source must be moyu or letmoe")
	}
	if opt.Batch < 1 {
		opt.Batch = 1
	}
	importedAt := time.Now().UTC()
	rep := report{Source: opt.Source, Applied: opt.Apply}

	src, err := readSource(source.WithContext(ctx), opt.Source)
	if err != nil {
		return rep, err
	}
	rep.SourceEdges = len(src)

	central, err := readCentral(community.WithContext(ctx))
	if err != nil {
		return rep, err
	}

	sourceSet := make(map[[2]int64]struct{}, len(src))
	toInsert := make([]sourceEdge, 0, len(src))
	for _, e := range src {
		if e.FollowerID == e.FolloweeID {
			rep.SelfSkipped++
			continue
		}
		key := [2]int64{e.FollowerID, e.FolloweeID}
		sourceSet[key] = struct{}{}
		if _, ok := central[key]; ok {
			rep.AlreadyPresent++
			continue
		}
		toInsert = append(toInsert, e)
	}
	rep.ToInsert = len(toInsert)

	var stale [][2]int64
	for key, row := range central {
		if row.OriginSite == opt.Source && row.Imported {
			if _, ok := sourceSet[key]; !ok {
				stale = append(stale, key)
			}
		}
	}
	sort.Slice(stale, func(i, j int) bool {
		if stale[i][0] != stale[j][0] {
			return stale[i][0] < stale[j][0]
		}
		return stale[i][1] < stale[j][1]
	})
	rep.Stale = len(stale)
	if !opt.Apply && len(stale) > 0 {
		n := len(stale)
		if n > 20 {
			n = 20
		}
		slog.Info("stale pairs (first 20)", "pairs", stale[:n])
	}
	if !opt.Apply {
		return rep, nil
	}

	err = community.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := 0; i < len(toInsert); i += opt.Batch {
			end := i + opt.Batch
			if end > len(toInsert) {
				end = len(toInsert)
			}
			n, err := insertFollowBatch(tx, toInsert[i:end], opt.Source, importedAt)
			if err != nil {
				return err
			}
			rep.Inserted += n
		}
		rep.Raced = rep.ToInsert - rep.Inserted
		if !opt.PruneStale {
			return nil
		}
		for i := 0; i < len(stale); i += opt.Batch {
			end := i + opt.Batch
			if end > len(stale) {
				end = len(stale)
			}
			n, err := deleteStale(tx, opt.Source, stale[i:end])
			if err != nil {
				return err
			}
			rep.Pruned += n
		}
		return nil
	})
	return rep, err
}

func readSource(db *gorm.DB, source string) ([]sourceEdge, error) {
	var q string
	switch source {
	case "moyu":
		q = `SELECT follower_id, following_id AS followee_id, NULL::timestamptz AS created_at FROM user_follow_relation ORDER BY id`
	case "letmoe":
		q = `SELECT follower_id, followee_id, created AS created_at FROM follow ORDER BY created, id`
	default:
		return nil, fmt.Errorf("source must be moyu or letmoe")
	}
	var rows []sourceEdge
	err := db.Raw(q).Scan(&rows).Error
	return rows, err
}

func readCentral(db *gorm.DB) (map[[2]int64]centralRow, error) {
	var rows []centralRow
	if err := db.Raw(`
		SELECT follower_id, followee_id, origin_site, (imported_at IS NOT NULL) AS imported
		  FROM community_user_follow`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[[2]int64]centralRow, len(rows))
	for _, r := range rows {
		out[[2]int64{r.FollowerID, r.FolloweeID}] = r
	}
	return out, nil
}

func insertFollowBatch(tx *gorm.DB, rows []sourceEdge, origin string, importedAt time.Time) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	var b strings.Builder
	args := make([]any, 0, len(rows)*5)
	b.WriteString(`INSERT INTO community_user_follow (follower_id, followee_id, origin_site, created_at, imported_at) VALUES `)
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("(?,?,?,?,?)")
		args = append(args, r.FollowerID, r.FolloweeID, origin, r.CreatedAt, importedAt)
	}
	b.WriteString(` ON CONFLICT (follower_id, followee_id) DO NOTHING RETURNING id`)
	var ids []int64
	if err := tx.Raw(b.String(), args...).Scan(&ids).Error; err != nil {
		return 0, err
	}
	return len(ids), nil
}

func deleteStale(tx *gorm.DB, origin string, pairs [][2]int64) (int, error) {
	if len(pairs) == 0 {
		return 0, nil
	}
	var b strings.Builder
	args := make([]any, 0, 1+len(pairs)*2)
	args = append(args, origin)
	b.WriteString(`DELETE FROM community_user_follow WHERE origin_site = ? AND imported_at IS NOT NULL AND (follower_id, followee_id) IN (`)
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("(?,?)")
		args = append(args, p[0], p[1])
	}
	b.WriteByte(')')
	res := tx.Exec(b.String(), args...)
	return int(res.RowsAffected), res.Error
}
