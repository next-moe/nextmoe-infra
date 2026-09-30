// Package newsmoderate is the moderation gate 月幕 asked for: "你要是拿过去直接
// 入库了,可能给广告哥一起保存了". It scores pending news items with trust's Tier0
// word list and the AI gateway.
//
// The direction here is the OPPOSITE of every other moderation caller in this
// repository. The AI gateway and trust's scan pipeline both fail OPEN — a budget
// denial, an upstream error or a truncated reply all come back as
// flagged=false — which is right for UGC, where a forum post must not vanish
// because a token bucket ran dry. This is third-party content going out under
// our name, so the charter's rule is 不确定不发, and the consequence is stated
// once here and enforced throughout:
//
//	a degraded verdict NEVER advances an item, and never counts as "scored".
//
// Nothing in this package may set status=published. Publishing is a human act,
// or an importer's standing release for a source the user ruled needs no review
// (月幕 since 2026-09-29, 批评 since 2026-09-05) — those rows never wait here.
package newsmoderate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/news/model"
	"api/pkg/config"

	"gorm.io/gorm"
)

type Opts struct {
	Apply bool
	Limit int
	Gap   time.Duration
	DSN   string
}

// Run is the entry point for both the scheduled job and cmd/moderate-news; it
// owns the kun_news connection for the length of one pass.
func Run(ctx context.Context, cfg *config.Config, opts Opts) (Stats, error) {
	c := cfg.NewsModeration
	if c.ClientID == "" || c.ClientSecret == "" {
		return Stats{}, fmt.Errorf("news client not configured (set KUN_NEWS_IMAGE_CLIENT_ID / KUN_NEWS_IMAGE_CLIENT_SECRET)")
	}
	db, closeDB, err := openNewsDB(cfg, opts.DSN)
	if err != nil {
		return Stats{}, err
	}
	defer closeDB()

	client := NewClient(c.TrustBaseURL, c.AIBaseURL, c.ClientID, c.ClientSecret)
	return New(db, client, opts).Run(ctx)
}

func openNewsDB(cfg *config.Config, dsn string) (*gorm.DB, func(), error) {
	if dsn != "" {
		db, err := database.OpenJobWith(dsn, &gorm.Config{})
		if err != nil {
			return nil, nil, fmt.Errorf("news db (--dsn): %w", err)
		}
		return db, func() {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}, nil
	}
	conn, err := database.NewPostgresDB(cfg.NewsDatabase)
	if err != nil {
		return nil, nil, fmt.Errorf("news db: %w", err)
	}
	return conn.DB(), func() { _ = conn.Close() }, nil
}

type Stats struct {
	Candidates   int `json:"candidates"`
	NeedScoring  int `json:"need_scoring"`
	Scored       int `json:"scored"`
	AutoRejected int `json:"auto_rejected"`
	Degraded     int `json:"degraded"`
	Exhausted    int `json:"exhausted"`
	Failed       int `json:"failed"`
}

// moderationText is what both graders see. The title is included because a
// 广告哥 post is very often nothing but a title.
func moderationText(it model.NewsItem) string {
	return strings.TrimSpace(it.Title + "\n" + it.Preview + "\n" + it.Body)
}

type scorer interface {
	Tier0(ctx context.Context, text string) (Tier0Verdict, error)
	Score(ctx context.Context, text string) (ScoreVerdict, error)
}

type Runner struct {
	db     *gorm.DB
	client scorer
	opts   Opts
}

func New(db *gorm.DB, client scorer, opts Opts) *Runner {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	return &Runner{db: db, client: client, opts: opts}
}

func (r *Runner) Run(ctx context.Context) (Stats, error) {
	var st Stats

	// The whole pending set is loaded and fingerprinted in Go rather than
	// filtered in SQL. Keeping the fingerprint's definition in exactly one place
	// is worth more than the query: this table tops out in the low thousands
	// (00-workflow §2 sizes the steady state at ~2,000 rows), and a digest
	// expression duplicated into SQL is a second definition that can drift.
	var items []model.NewsItem
	if err := r.db.WithContext(ctx).
		Where("status = ? AND dead_at IS NULL", model.StatusPending).
		Order("id").Find(&items).Error; err != nil {
		return st, err
	}
	st.Candidates = len(items)
	if len(items) == 0 {
		return st, nil
	}

	history, err := r.verdictHistory(ctx, items)
	if err != nil {
		return st, err
	}

	var due []model.NewsItem
	for _, it := range items {
		fp := it.Fingerprint()
		h := history[it.ID][fp]
		if h.settled {
			continue
		}
		if h.attempts >= model.ModerationAttemptCeiling {
			st.Exhausted++
			continue
		}
		due = append(due, it)
	}
	st.NeedScoring = len(due)
	if len(due) > r.opts.Limit {
		due = due[:r.opts.Limit]
	}

	for i, it := range due {
		if i > 0 && r.opts.Gap > 0 {
			select {
			case <-ctx.Done():
				return st, ctx.Err()
			case <-time.After(r.opts.Gap):
			}
		}
		if err := r.one(ctx, it, &st); err != nil {
			st.Failed++
			slog.Warn("news moderation: item failed", "item", it.ID, "err", err)
		}
	}
	return st, nil
}

type fpHistory struct {
	attempts int
	// settled = a verdict for this exact text that the grader actually produced.
	// A degraded verdict is deliberately not settled: treating one as scored is
	// how a five-minute outage would silently mark a whole queue clean.
	settled bool
}

func (r *Runner) verdictHistory(ctx context.Context, items []model.NewsItem) (map[int64]map[string]fpHistory, error) {
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	var rows []model.NewsModerationVerdict
	if err := r.db.WithContext(ctx).
		Where("item_id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[int64]map[string]fpHistory{}
	for _, v := range rows {
		byFP := out[v.ItemID]
		if byFP == nil {
			byFP = map[string]fpHistory{}
			out[v.ItemID] = byFP
		}
		h := byFP[v.ContentFingerprint]
		h.attempts++
		if !v.Degraded {
			h.settled = true
		}
		byFP[v.ContentFingerprint] = h
	}
	return out, nil
}

func (r *Runner) one(ctx context.Context, it model.NewsItem, st *Stats) error {
	text := moderationText(it)
	verdict := model.NewsModerationVerdict{
		ItemID:             it.ID,
		ContentFingerprint: it.Fingerprint(),
	}

	t0, err := r.client.Tier0(ctx, text)
	if err != nil {
		verdict.Degraded = true
		verdict.DegradedReason = model.DegradedTransport
		verdict.Tier0Decision = model.Tier0Allow
		slog.Warn("news moderation: tier0 unreachable — item stays pending", "item", it.ID, "err", err)
		st.Degraded++
		return r.persist(ctx, it, verdict, "")
	}
	verdict.Tier0Decision = t0.Decision
	verdict.Tier0Matched = jsonOrNil(t0.Matched)

	// A banned term is a decision, not a signal: stop here rather than spend an
	// AI call confirming what a deterministic word list already settled.
	if t0.Decision == model.Tier0Deny {
		st.AutoRejected++
		st.Scored++
		return r.persist(ctx, it, verdict, "tier0:"+strings.Join(t0.Matched, ","))
	}

	score, err := r.client.Score(ctx, text)
	switch {
	case err != nil:
		verdict.Degraded = true
		verdict.DegradedReason = model.DegradedTransport
		slog.Warn("news moderation: gateway unreachable — item stays pending", "item", it.ID, "err", err)
		st.Degraded++
	case score.Degraded:
		verdict.Degraded = true
		verdict.DegradedReason = model.DegradedUpstream
		verdict.AIChannel = score.Channel
		st.Degraded++
	default:
		flagged := score.Flagged
		verdict.AIFlagged = &flagged
		verdict.AIScore = score.Score
		verdict.AICategories = jsonOrNil(score.Categories)
		verdict.AIChannel = score.Channel
		st.Scored++
	}
	return r.persist(ctx, it, verdict, "")
}

// persist writes the verdict and, for a Tier0 deny, the auto-reject decision
// that goes with it. autoRejectReason being empty means the item is left exactly
// where it was — which is the outcome for every degraded and every clean verdict
// alike, because only a human publishes.
func (r *Runner) persist(ctx context.Context, it model.NewsItem, v model.NewsModerationVerdict, autoRejectReason string) error {
	if !r.opts.Apply {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&v).Error; err != nil {
			return err
		}
		if autoRejectReason == "" {
			return nil
		}
		d := model.NewsModerationDecision{
			ItemID:     it.ID,
			ActorUID:   model.SystemActorUID,
			FromStatus: it.Status,
			ToStatus:   model.StatusRejected,
			Reason:     autoRejectReason,
		}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		return tx.Model(&model.NewsItem{}).Where("id = ?", it.ID).
			Updates(map[string]any{"status": model.StatusRejected, "updated_at": time.Now()}).Error
	})
}

func jsonOrNil(v []string) []byte {
	if len(v) == 0 {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

func (s Stats) String() string {
	return fmt.Sprintf("candidates=%d need_scoring=%d scored=%d auto_rejected=%d degraded=%d exhausted=%d failed=%d",
		s.Candidates, s.NeedScoring, s.Scored, s.AutoRejected, s.Degraded, s.Exhausted, s.Failed)
}
