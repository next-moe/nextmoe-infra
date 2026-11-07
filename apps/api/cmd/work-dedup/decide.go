package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

type pairDecision struct {
	aID, bID       int64
	action         string
	source, target int64
	evidence       string
}

var decideActions = map[string]string{"merge": "accept", "reject": "reject", "defer": "defer"}

// runDecide applies a reviewed decision file to work candidates through
// DecideCandidate, the path a person takes in the admin queue: a merge files an
// open proposal, a reject records the rejection and releases quarantine, a
// defer parks the pair.
func runDecide(ctx context.Context, db *gorm.DB, w io.Writer, queues *service.AdminQueueService,
	actor int64, note, path string, run bool) error {
	if err := guardPairsNote(note); err != nil {
		return err
	}
	rows, err := loadDecisions(path)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	failed := 0
	for _, d := range rows {
		lo, hi := min(d.aID, d.bID), max(d.aID, d.bID)
		var status *int16
		if err := db.WithContext(ctx).Raw(`SELECT status FROM catalog_match_candidate
			WHERE entity_type = ? AND a_id = ? AND b_id = ?`, model.EntityTypeWork, lo, hi).Scan(&status).Error; err != nil {
			return err
		}
		if status == nil || !slices.Contains([]int16{model.CandidateStatusPending, model.CandidateStatusDeferred,
			model.CandidateStatusNeedsManual}, *status) {
			counts["skipped_not_decidable"]++
			fmt.Fprintf(w, "SKIP (%d, %d) candidate status %v\n", lo, hi, derefStatus(status))
			continue
		}
		if !run {
			counts["would_"+d.action]++
			fmt.Fprintf(w, "PLAN %s (%d, %d) %d -> %d  %s\n", d.action, lo, hi, d.source, d.target, d.evidence)
			continue
		}
		_, err := queues.DecideCandidate(ctx, service.CandidateDecision{
			EntityType: model.EntityTypeWork, AID: lo, BID: hi, Action: decideActions[d.action],
			SourceID: d.source, TargetID: d.target, DecidedBy: actor, Note: note + " | " + d.evidence,
		})
		if err != nil {
			failed++
			fmt.Fprintf(w, "  (%d, %d) %s ERROR %v\n", lo, hi, d.action, err)
			continue
		}
		counts[d.action]++
	}
	mode := "DRY-RUN (pass -run to decide)"
	if run {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s [decide] note=%s rows=%d counts=%v failed=%d\n", mode, note, len(rows), counts, failed)
	if failed > 0 {
		return fmt.Errorf("decide: %d decision(s) failed", failed)
	}
	return nil
}

func derefStatus(s *int16) string {
	if s == nil {
		return "missing"
	}
	return strconv.Itoa(int(*s))
}

func loadDecisions(path string) ([]pairDecision, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	var out []pairDecision
	seen := map[[2]int64]int{}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		raw := sc.Text()
		if t := strings.TrimSpace(raw); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		f := strings.Split(raw, "\t")
		if len(f) != 6 {
			return nil, fmt.Errorf("%s:%d: want 6 tab-separated fields (a, b, action, source, target, evidence), got %d", path, line, len(f))
		}
		var ids [4]int64
		for i, idx := range []int{0, 1, 3, 4} {
			v := strings.TrimSpace(f[idx])
			if v == "" {
				continue
			}
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("%s:%d: field %d: %q is not an id", path, line, idx+1, v)
			}
			ids[i] = n
		}
		d := pairDecision{aID: ids[0], bID: ids[1], action: strings.TrimSpace(f[2]),
			source: ids[2], target: ids[3], evidence: strings.TrimSpace(f[5])}
		if d.aID <= 0 || d.bID <= 0 || d.aID == d.bID {
			return nil, fmt.Errorf("%s:%d: a and b must be two work ids", path, line)
		}
		if _, ok := decideActions[d.action]; !ok {
			return nil, fmt.Errorf("%s:%d: action %q is not merge, reject or defer", path, line, d.action)
		}
		if d.action == "merge" {
			pair := []int64{d.aID, d.bID}
			if d.source == d.target || !slices.Contains(pair, d.source) || !slices.Contains(pair, d.target) {
				return nil, fmt.Errorf("%s:%d: merge needs source and target to be the pair", path, line)
			}
		}
		if d.evidence == "" {
			return nil, fmt.Errorf("%s:%d: evidence is empty", path, line)
		}
		key := [2]int64{min(d.aID, d.bID), max(d.aID, d.bID)}
		if prev, ok := seen[key]; ok {
			return nil, fmt.Errorf("%s:%d: pair already decided on line %d", path, line, prev)
		}
		seen[key] = line
		out = append(out, d)
	}
	return out, sc.Err()
}
