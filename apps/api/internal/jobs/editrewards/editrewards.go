package editrewards

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"api/internal/platform/editing"
	ledgerModel "api/internal/platform/ledger/model"
	"api/internal/platform/ledger/service"

	"gorm.io/gorm"
)

const (
	SourceApp = "nextmoe-editor"

	KeyPrefix = "catalog_edit_merged:"
	RefPrefix = "catalog_edit:"

	forumSiteDomain = "www.kungal.com"
	lookupChunk     = 1000
)

// The forum's moemoepoint.Key prepends "kungal:", so its merge awards sit in the
// ledger as kungal:galgame_edit_merged:<id>. The first cut matched the bare
// galgame_edit_merged:<id>, found none of them, and would have paid every forum
// merge inside the window a second time.
const forumKeyPrefix = "kungal:galgame_edit_merged:"

type Opts struct {
	Amount int64
	Since  time.Time
}

type Report struct {
	Candidates     int
	Awarded        int
	AlreadyAwarded int
	ForumAwarded   int
	ProposerGone   int
	Failed         int
}

type candidate struct {
	ID           int64
	ProposerUID  int64
	DecidedByUID int64
	EntityFamily string
	EntityID     int64
}

func Run(ctx context.Context, catalogDB, coreDB *gorm.DB, opts Opts) (Report, error) {
	var rep Report
	if opts.Amount <= 0 {
		return rep, nil
	}

	var cands []candidate
	if err := catalogDB.WithContext(ctx).Raw(`
		SELECT p.id, p.proposer_uid, p.decided_by_uid, p.entity_family, p.entity_id
		FROM edit_proposal p
		WHERE p.status = ? AND p.decided_at >= ?
		  AND p.proposer_uid > 0
		  AND p.decided_by_uid > 0 AND p.decided_by_uid <> p.proposer_uid
		  AND NOT EXISTS (
		    SELECT 1 FROM edit_revision r WHERE r.proposal_id = p.id AND r.action = ?
		  )
		ORDER BY p.id`,
		editing.StatusMerged, opts.Since, editing.ActionReverted).Scan(&cands).Error; err != nil {
		return rep, fmt.Errorf("editrewards: load merged proposals: %w", err)
	}
	rep.Candidates = len(cands)
	if len(cands) == 0 {
		return rep, nil
	}

	ours, err := transferKeys(ctx, coreDB, keysOf(cands, KeyPrefix),
		`source_app = ?`, SourceApp)
	if err != nil {
		return rep, err
	}
	forum, err := transferKeys(ctx, coreDB, keysOf(cands, forumKeyPrefix),
		`source_app IN (SELECT c.id FROM oauth_clients c JOIN sites s ON s.id = c.site_id WHERE s.domain = ?)`,
		forumSiteDomain)
	if err != nil {
		return rep, err
	}
	live, err := liveUsers(ctx, coreDB, cands)
	if err != nil {
		return rep, err
	}

	ledger := service.New(coreDB)
	for _, c := range cands {
		id := strconv.FormatInt(c.ID, 10)
		switch {
		case ours[KeyPrefix+id]:
			rep.AlreadyAwarded++
		case forum[forumKeyPrefix+id]:
			rep.ForumAwarded++
		case !live[c.ProposerUID]:
			rep.ProposerGone++
		default:
			if _, err := ledger.Award(ctx, service.Award{
				UserID:         uint(c.ProposerUID),
				Delta:          opts.Amount,
				Reason:         ledgerModel.ReasonContentApproved,
				SourceApp:      SourceApp,
				Ref:            RefPrefix + id,
				ActorUserID:    uint(c.DecidedByUID),
				IdempotencyKey: KeyPrefix + id,
				Note:           fmt.Sprintf("%s #%d", c.EntityFamily, c.EntityID),
			}); err != nil {
				rep.Failed++
				slog.Warn("editrewards: award failed", "proposal_id", c.ID, "user_id", c.ProposerUID, "err", err)
				continue
			}
			rep.Awarded++
		}
	}
	if rep.Failed > 0 {
		return rep, fmt.Errorf("editrewards: %d of %d awards failed", rep.Failed, rep.Candidates)
	}
	return rep, nil
}

func keysOf(cands []candidate, prefix string) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = prefix + strconv.FormatInt(c.ID, 10)
	}
	return out
}

func transferKeys(ctx context.Context, db *gorm.DB, keys []string, sourceCond string, sourceArg any) (map[string]bool, error) {
	out := make(map[string]bool)
	for start := 0; start < len(keys); start += lookupChunk {
		chunk := keys[start:min(start+lookupChunk, len(keys))]
		var found []string
		if err := db.WithContext(ctx).Raw(
			`SELECT idempotency_key FROM ledger_transfers WHERE `+sourceCond+` AND idempotency_key IN ?`,
			sourceArg, chunk).Scan(&found).Error; err != nil {
			return nil, fmt.Errorf("editrewards: look up ledger transfers: %w", err)
		}
		for _, k := range found {
			out[k] = true
		}
	}
	return out, nil
}

func liveUsers(ctx context.Context, db *gorm.DB, cands []candidate) (map[int64]bool, error) {
	seen := make(map[int64]bool, len(cands))
	ids := make([]int64, 0, len(cands))
	for _, c := range cands {
		if !seen[c.ProposerUID] {
			seen[c.ProposerUID] = true
			ids = append(ids, c.ProposerUID)
		}
	}
	out := make(map[int64]bool, len(ids))
	for start := 0; start < len(ids); start += lookupChunk {
		chunk := ids[start:min(start+lookupChunk, len(ids))]
		var found []int64
		if err := db.WithContext(ctx).Raw(
			`SELECT id FROM users WHERE id IN ? AND deleted_at IS NULL`, chunk).Scan(&found).Error; err != nil {
			return nil, fmt.Errorf("editrewards: look up proposers: %w", err)
		}
		for _, id := range found {
			out[id] = true
		}
	}
	return out, nil
}
