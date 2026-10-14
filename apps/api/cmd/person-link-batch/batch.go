package main

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

const (
	ruleA1 = "A1"
	ruleA2 = "A2"
	ruleA3 = "A3"
	ruleA4 = "A4"

	ruleSetShared = "shared"
	ruleSetAlias  = "alias"
)

type candidateRow struct {
	AID     int64  `gorm:"column:a_id"`
	BID     int64  `gorm:"column:b_id"`
	AName   string `gorm:"column:an"`
	BName   string `gorm:"column:bn"`
	APerson *int64 `gorm:"column:ap"`
	BPerson *int64 `gorm:"column:bp"`
}

func loadCandidates(db *gorm.DB, reason int16) ([]candidateRow, error) {
	var rows []candidateRow
	err := db.Raw(`
		SELECT c.a_id, c.b_id,
		       normalize(a.name, NFKC) AS an, normalize(b.name, NFKC) AS bn,
		       a.person_id AS ap, b.person_id AS bp
		FROM catalog_match_candidate c
		JOIN catalog_credit_name a ON a.id = c.a_id
		JOIN catalog_credit_name b ON b.id = c.b_id
		WHERE c.entity_type = ? AND c.reason = ? AND c.status = ?
		ORDER BY c.a_id, c.b_id`,
		model.EntityTypeCreditName, reason, model.CandidateStatusPending,
	).Scan(&rows).Error
	return rows, err
}

func reasonForRuleSet(rs string) int16 {
	if rs == ruleSetAlias {
		return model.CandidateReasonAliasDeclared
	}
	return model.CandidateReasonSharedExternalID
}

func classify(anfkc, bnfkc string) string {
	af, bf := foldName(anfkc), foldName(bnfkc)
	if af != "" && af == bf {
		return ruleA1
	}
	if a2Match(af, bnfkc) || a2Match(bf, anfkc) {
		return ruleA2
	}
	return ""
}

func a2Match(target, ownerNFKC string) bool {
	return target != "" && slices.Contains(parenAliases(ownerNFKC), target)
}

func foldName(nfkc string) string {
	return strings.ToLower(removeSpaces(stripParens(nfkc)))
}

func stripParens(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func removeSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func parenAliases(nfkc string) []string {
	var items []string
	depth := 0
	var cur strings.Builder
	flush := func() {
		for _, part := range strings.FieldsFunc(cur.String(), func(r rune) bool { return r == '、' || r == ',' }) {
			if f := strings.ToLower(removeSpaces(part)); f != "" {
				items = append(items, f)
			}
		}
		cur.Reset()
	}
	for _, r := range nfkc {
		switch r {
		case '(':
			if depth == 0 {
				cur.Reset()
			}
			depth++
		case ')':
			if depth > 0 {
				depth--
				if depth == 0 {
					flush()
				}
			}
		default:
			if depth > 0 {
				cur.WriteRune(r)
			}
		}
	}
	return items
}

type batchStats struct {
	A1Hits, A2Hits, A3Hits, A4Hits int
	LinkedCreated                  int
	LinkedAttached                 int
	NeedsManual                    int
	Already                        int
	Errors                         int
	Unmatched                      int
}

func (st *batchStats) hit(rule string) {
	switch rule {
	case ruleA1:
		st.A1Hits++
	case ruleA2:
		st.A2Hits++
	case ruleA3:
		st.A3Hits++
	case ruleA4:
		st.A4Hits++
	}
}

func run(ctx context.Context, db *gorm.DB, w io.Writer, actor int64, apply bool, ruleSet string) (batchStats, error) {
	rows, err := loadCandidates(db, reasonForRuleSet(ruleSet))
	if err != nil {
		return batchStats{}, err
	}
	classify, err := buildClassifier(db, ruleSet, rows)
	if err != nil {
		return batchStats{}, err
	}
	queues := adminQueue(db)

	var st batchStats
	for _, r := range rows {
		rule := classify(r)
		if rule == "" {
			st.Unmatched++
			continue
		}
		st.hit(rule)

		if !apply {
			action := predictOutcome(r.APerson, r.BPerson)
			tallyPredicted(&st, action)
			fmt.Fprintf(w, "[%s] %-3s  %q ↔ %q\n", action, rule, r.AName, r.BName)
			continue
		}

		outcome, err := queues.DecideCandidate(ctx, service.CandidateDecision{
			EntityType: model.EntityTypeCreditName, AID: r.AID, BID: r.BID,
			Action: "accept", DecidedBy: actor,
		})
		switch {
		case err == nil && outcome.Link != nil && outcome.Link.NeedsManual:
			st.NeedsManual++
		case err == nil && outcome.Link != nil && outcome.Link.Created:
			st.LinkedCreated++
		case err == nil && outcome.Link != nil:
			st.LinkedAttached++
		case stderrors.Is(err, service.ErrProposalState):
			st.Already++
		default:
			st.Errors++
			fmt.Fprintf(w, "  ! candidate (%d,%d): %v\n", r.AID, r.BID, err)
		}
	}

	mode := "DRY-RUN (nothing written; pass --run to link)"
	if apply {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s [%s] — A1=%d A2=%d A3=%d A4=%d | created=%d attached=%d needs_manual=%d already=%d errors=%d | unmatched=%d of %d\n",
		mode, ruleSet, st.A1Hits, st.A2Hits, st.A3Hits, st.A4Hits, st.LinkedCreated, st.LinkedAttached, st.NeedsManual, st.Already, st.Errors,
		st.Unmatched, len(rows))
	return st, nil
}

func buildClassifier(db *gorm.DB, ruleSet string, rows []candidateRow) (func(candidateRow) string, error) {
	if ruleSet != ruleSetAlias {
		return func(r candidateRow) string { return classify(r.AName, r.BName) }, nil
	}
	return aliasClassifier(db, rows)
}

func predictOutcome(a, b *int64) string {
	switch {
	case a != nil && b != nil:
		if *a == *b {
			return "already"
		}
		return "needs_manual"
	case a != nil || b != nil:
		return "attach"
	default:
		return "create"
	}
}

func tallyPredicted(st *batchStats, action string) {
	switch action {
	case "create":
		st.LinkedCreated++
	case "attach":
		st.LinkedAttached++
	case "needs_manual":
		st.NeedsManual++
	case "already":
		st.Already++
	}
}

func adminQueue(db *gorm.DB) *service.AdminQueueService {
	resolve := service.NewResolveService(repository.NewRedirectRepository(db))
	merge := service.NewMergeService(db, resolve)
	return service.NewAdminQueueService(db, merge)
}
