package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/agent/tools"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

const (
	siteToolListTTL   = 5 * time.Minute
	siteToolFailedTTL = time.Minute
)

type SiteTools struct {
	db   *gorm.DB
	http *http.Client
	now  func() time.Time

	mu    sync.Mutex
	lists map[int64]siteToolList
}

type siteToolList struct {
	tools []*mcp.Tool
	err   error
	at    time.Time
	stamp time.Time
}

func NewSiteTools(db *gorm.DB, client *http.Client) *SiteTools {
	return &SiteTools{db: db, http: client, now: time.Now, lists: map[int64]siteToolList{}}
}

func (st *SiteTools) server(t model.SiteTool, token string) tools.MCPServer {
	if t.Auth != model.SiteToolAuthUser {
		token = ""
	}
	return tools.MCPServer{Name: t.Name, URL: t.URL, CardPrefix: t.CardPrefix, Token: token,
		Timeout: time.Duration(t.TimeoutMs) * time.Millisecond, HTTP: st.http}
}

func (st *SiteTools) For(ctx context.Context, siteID int64, token string) []tools.Tool {
	var rows []model.SiteTool
	if err := st.db.WithContext(ctx).Where("site_id = ? AND enabled", siteID).Order("id").Find(&rows).Error; err != nil {
		slog.Warn("agent: load site tools", "site", siteID, "err", err)
		return nil
	}
	var out []tools.Tool
	for _, row := range rows {
		if row.Auth == model.SiteToolAuthUser && token == "" {
			continue
		}
		listed, err := st.list(ctx, row, token)
		if err != nil {
			continue
		}
		var allowed []string
		_ = json.Unmarshal(row.Allowed, &allowed)
		server := st.server(row, token)
		for _, t := range listed {
			if len(allowed) == 0 || slices.Contains(allowed, t.Name) {
				out = append(out, tools.NewMCPTool(server, t))
			}
		}
	}
	return out
}

func (st *SiteTools) list(ctx context.Context, row model.SiteTool, token string) ([]*mcp.Tool, error) {
	now := st.now()
	st.mu.Lock()
	cached, ok := st.lists[row.ID]
	st.mu.Unlock()
	if ok && cached.stamp.Equal(row.UpdatedAt) {
		ttl := siteToolListTTL
		if cached.err != nil {
			ttl = siteToolFailedTTL
		}
		if now.Sub(cached.at) < ttl {
			return cached.tools, cached.err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	listed, err := st.server(row, token).ListTools(ctx)
	if err != nil {
		slog.Warn("agent: list site tools", "site", row.SiteID, "server", row.Name, "err", err)
	}
	st.mu.Lock()
	st.lists[row.ID] = siteToolList{tools: listed, err: err, at: now, stamp: row.UpdatedAt}
	st.mu.Unlock()
	return listed, err
}

func (st *SiteTools) Probe(ctx context.Context, row model.SiteTool, token string) ([]*mcp.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return st.server(row, token).ListTools(ctx)
}

func (st *SiteTools) List(ctx context.Context, siteID int64) ([]model.SiteTool, error) {
	q := st.db.WithContext(ctx).Order("site_id, id")
	if siteID > 0 {
		q = q.Where("site_id = ?", siteID)
	}
	var rows []model.SiteTool
	return rows, q.Find(&rows).Error
}

func (st *SiteTools) Get(ctx context.Context, id int64) (*model.SiteTool, error) {
	var row model.SiteTool
	err := st.db.WithContext(ctx).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (st *SiteTools) Save(ctx context.Context, row *model.SiteTool) error {
	if row.ID == 0 {
		return st.db.WithContext(ctx).Create(row).Error
	}
	return st.db.WithContext(ctx).Save(row).Error
}

func (st *SiteTools) Delete(ctx context.Context, id int64) error {
	res := st.db.WithContext(ctx).Delete(&model.SiteTool{}, id)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}
