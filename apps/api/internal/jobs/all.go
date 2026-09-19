package jobs

import (
	"context"
	"log/slog"
	"time"

	"api/internal/jobs/newsmoderate"
	"api/internal/jobs/storestats"
	"api/internal/jobs/ymgalnews"
	"api/pkg/config"
)

func RegisterAll(r *Registry) {
	r.Register(Job{
		Name: "image-gc",
		Desc: "image_service TTL 生命周期（冷候选/软删/物删）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return RunImageGC(ctx, cfg, DefaultImageGCOpts())
		},
	})

	r.Register(Job{
		Name: "galgame-image-refping",
		Desc: "galgame banner reference-ping，防 image_service TTL 回收",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return RunGalgameImageRefping(ctx, cfg, DefaultGalgameImageRefpingOpts())
		},
	})

	r.Register(Job{
		Name: "catalog-image-refping",
		Desc: "catalog 角色立绘 reference-ping，防 image_service TTL 回收（site=catalog）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return RunCatalogImageRefping(ctx, cfg, DefaultCatalogImageRefpingOpts())
		},
	})

	r.Register(Job{
		Name: "news-image-refping",
		Desc: "情报 banner 与文中图 reference-ping，防 image_service TTL 回收（site=news）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return RunNewsImageRefping(ctx, cfg, DefaultNewsImageRefpingOpts())
		},
	})

	r.Register(Job{
		Name: "user-avatar-refping",
		Desc: "用户头像 reference-ping，防 image_service TTL 回收",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return RunUserAvatarRefping(ctx, cfg, DefaultUserAvatarRefpingOpts())
		},
	})

	r.Register(Job{
		Name: JobImageRefAudit,
		Desc: "catalog 图片引用对账（引用指向已删字节即告警，赶在 30 天物删前抢救）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return RunImageRefAudit(ctx, cfg, DefaultImageRefAuditOpts())
		},
	})

	r.Register(Job{
		Name: "artifact-gc",
		Desc: "artifact 生命周期（孤儿上传回收 + 软删物理回收）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return RunArtifactGC(ctx, cfg, DefaultArtifactGCOpts())
		},
	})

	r.Register(Job{
		Name: "prune-developer-usage",
		Desc: "developer_api_usage 留存修剪（删除 day < 今天−400 天 的计量汇总行）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return RunPruneDeveloperUsage(ctx, cfg, DefaultPruneDeveloperUsageOpts())
		},
	})

	// 苍麟 asked for a polite cadence in his own words — 「搞个十分钟一次的定时任务
	// 应该问题不大」 — and this poll spends exactly one request per lane per tick
	// (page 1 only), which stays well inside what he allowed.
	//
	// NOTE: the cadence default lives in keys/jobs.go. The first run happens
	// one interval after boot, not at boot.
	r.Register(Job{
		Name: "ymgal-news-poll",
		Desc: "月幕情报/专栏增量轮询（每 lane 只取第 1 页）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return runYmgalNews(ctx, cfg, ymgalnews.Opts{
				Lanes: []string{ymgalnews.LaneNews, ymgalnews.LaneColumn},
				Pages: 1, Apply: true, Gap: time.Second,
			})
		},
	})

	// The deep sweep is what can see a deletion at all: the poll only ever looks
	// at page 1, so an ad 苍麟 removed from further back would never come up. It
	// reports dead candidates and deliberately does not act on them.
	r.Register(Job{
		Name: "ymgal-news-sweep",
		Desc: "月幕深巡（前 5 页）+ 上游消失候选报告（只报不打）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return runYmgalNews(ctx, cfg, ymgalnews.Opts{
				Lanes: []string{ymgalnews.LaneNews, ymgalnews.LaneColumn},
				Pages: 5, Apply: true, Gap: 2 * time.Second,
			})
		},
	})

	r.Register(Job{
		Name: "store-stats-sync",
		Desc: "DLsite 分销短链点击同步（JST 日桶，最近 3 日窗口；首轮从最早铸链日全量补齐）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return RunStoreStatsSync(ctx, cfg, storestats.DefaultOpts())
		},
	})

	// The hourly sync only re-reads three days, so a recount on the redirector
	// (its bot rule changed on 2026-09-19) would never reach older days.
	r.Register(Job{
		Name: "store-stats-resync",
		Desc: "DLsite 分销短链点击全量重拉（上月 1 日至今；短链服务重算历史后由它改正本地缓存）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return RunStoreStatsSync(ctx, cfg, storestats.ResyncOpts(time.Now()))
		},
	})

	// Faster than ingestion (10 min) so a freshly ingested item is gradeable by
	// the time a moderator opens the queue. Nothing here can publish: the gate
	// only ever auto-REJECTS, and only on a deterministic Tier0 word match.
	r.Register(Job{
		Name: "news-moderate",
		Desc: "情报审核评分（Tier0 + AI 顾问；降级不推进，只有人能发布）",
		Run: func(ctx context.Context, cfg *config.Config) (Summary, error) {
			return runNewsModerate(ctx, cfg, newsmoderate.Opts{
				Apply: true, Limit: 50, Gap: 200 * time.Millisecond,
			})
		},
	})
}

// runNewsModerate soft-skips an unconfigured deployment for the same reason
// runYmgalNews does. The credential is the news OAuth client, and it must carry
// catalog_site='news' — trust and the AI gateway both derive the caller's site
// from it and answer 403 without one.
func runNewsModerate(ctx context.Context, cfg *config.Config, opts newsmoderate.Opts) (Summary, error) {
	c := cfg.NewsModeration
	if c.ClientID == "" || c.ClientSecret == "" {
		slog.Warn("news moderation: client not configured — skipping (set KUN_NEWS_IMAGE_CLIENT_ID/SECRET)")
		return Summary{"skipped": "news client not configured"}, nil
	}
	st, err := newsmoderate.Run(ctx, cfg, opts)
	if err != nil {
		return nil, err
	}
	return Summary{
		"candidates": st.Candidates, "need_scoring": st.NeedScoring, "scored": st.Scored,
		"auto_rejected": st.AutoRejected, "degraded": st.Degraded,
		"exhausted": st.Exhausted, "failed": st.Failed,
	}, nil
}

// runYmgalNews soft-skips when the upstream client is unconfigured, so a
// deployment without the credentials logs and moves on instead of failing a job
// every ten minutes.
func runYmgalNews(ctx context.Context, cfg *config.Config, opts ymgalnews.Opts) (Summary, error) {
	if cfg.Ymgal.ClientID == "" || cfg.Ymgal.ClientSecret == "" {
		slog.Warn("ymgal news: client not configured — skipping (set KUN_YMGAL_CLIENT_ID/SECRET)")
		return Summary{"skipped": "ymgal client not configured"}, nil
	}
	return ymgalnews.Run(ctx, cfg, opts)
}
