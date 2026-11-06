package main

import (
	"context"
	"fmt"
	"io"
	"sort"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const seedChunk = 1000

func seedStatusFor(b bucket) int16 {
	if b == bucketAuto {
		return model.CandidateStatusPending
	}
	return model.CandidateStatusNeedsManual
}

func runSeed(ctx context.Context, db *gorm.DB, w io.Writer, actor int64, run bool) error {
	c, err := buildCensus(ctx, db)
	if err != nil {
		return err
	}
	_, err = seedFromCensus(ctx, db, w, c, actor, run)
	return err
}

// seedFromCensus returns how many of the rows it actually inserted landed in
// needs_manual — the queue nobody is watching unless something says so. A
// dry run inserts nothing and therefore reports zero, not the planned count.
func seedFromCensus(ctx context.Context, db *gorm.DB, w io.Writer, c *census, actor int64, run bool) (int, error) {
	planned := map[bucket][]model.CatalogMatchCandidate{}
	for i, r := range c.rows {
		b := c.verdicts[i]
		if b == bucketOutOfScope {
			continue
		}
		reason := model.CandidateReasonNameNormEqual
		if r.SharedNames == 0 {
			reason = model.CandidateReasonSharedExternalID
		}
		planned[b] = append(planned[b], model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: r.A, BID: r.B,
			Reason: reason, Status: seedStatusFor(b),
		})
	}

	keys := make([]bucket, 0, len(planned))
	for b := range planned {
		keys = append(keys, b)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	total, inserted, needsManual := 0, 0, 0
	for _, b := range keys {
		rows := planned[b]
		total += len(rows)
		if !run {
			fmt.Fprintf(w, "  %-16s %6d -> %s\n", b, len(rows), statusName(seedStatusFor(b)))
			continue
		}
		got, err := insertCandidates(ctx, db, rows)
		if err != nil {
			return needsManual, err
		}
		inserted += got
		if seedStatusFor(b) == model.CandidateStatusNeedsManual {
			needsManual += got
		}
		fmt.Fprintf(w, "  %-16s %6d -> %-12s inserted=%d already_present=%d\n",
			b, len(rows), statusName(seedStatusFor(b)), got, len(rows)-got)
	}

	mode := "DRY-RUN (pass -run to file candidates)"
	if run {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s [seed] actor=%d in_scope=%d inserted=%d already_present=%d\n",
		mode, actor, total, inserted, total-inserted)
	return needsManual, nil
}

// insertCandidates never overwrites an existing row: a candidate an operator
// already decided must survive every later detector pass, and the seed has no
// way to tell a machine-filed row from a hand-decided one.
func insertCandidates(ctx context.Context, db *gorm.DB, rows []model.CatalogMatchCandidate) (int, error) {
	written := 0
	for start := 0; start < len(rows); start += seedChunk {
		end := min(start+seedChunk, len(rows))
		res := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(rows[start:end])
		if res.Error != nil {
			return written, res.Error
		}
		written += int(res.RowsAffected)
	}
	return written, nil
}

func statusName(status int16) string {
	switch status {
	case model.CandidateStatusPending:
		return "pending"
	case model.CandidateStatusAccepted:
		return "accepted"
	case model.CandidateStatusRejected:
		return "rejected"
	case model.CandidateStatusDeferred:
		return "deferred"
	case model.CandidateStatusNeedsManual:
		return "needs_manual"
	}
	return fmt.Sprintf("status%d", status)
}
