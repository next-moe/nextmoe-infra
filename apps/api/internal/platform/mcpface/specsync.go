package mcpface

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const DefaultSpecRefresh = 5 * time.Minute

// SpecSync keeps a server's tool set equal to the upstream OpenAPI document.
//
// The tool set used to be a startup snapshot. On 2026-08-25 production listed 49
// tools against a spec carrying 54: the five newest operations were live and
// answered over HTTP, and the MCP face simply did not offer them, because the
// container had last started before catalog grew them. Nothing errored. The
// documented remedy was "restart the container after a v2 deploy", and on
// 2026-09-13 that remedy turned out not to be reachable from the deploy UI: a
// redeploy runs `docker compose up -d`, which is idempotent, so an unchanged
// image leaves the container Running and reports success.
type SpecSync struct {
	srv *mcp.Server
	up  *Upstream

	mu    sync.Mutex
	sum   [sha256.Size]byte
	names []string
}

// Apply installs the tool set the document describes and reports whether the
// document moved. A document that fails to parse leaves the tool set in force
// untouched: a spec that cannot be read is not evidence that catalog has no
// operations.
func (s *SpecSync) Apply(raw []byte) (bool, error) {
	sum := sha256.Sum256(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.names != nil && sum == s.sum {
		return false, nil
	}
	names, err := applySpecTools(s.srv, s.up, raw)
	if err != nil {
		return false, err
	}
	// AddTool replaces by name; it does not sweep. A retired operation stays
	// callable until it is removed by name.
	var gone []string
	for _, old := range s.names {
		if !slices.Contains(names, old) {
			gone = append(gone, old)
		}
	}
	if len(gone) > 0 {
		s.srv.RemoveTools(gone...)
	}
	s.sum, s.names = sum, names
	return true, nil
}

func (s *SpecSync) Tools() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.names)
}

// Run re-reads the document on every tick until ctx is done.
func (s *SpecSync) Run(ctx context.Context, every time.Duration, fetch func(context.Context) ([]byte, error)) {
	if every <= 0 || fetch == nil {
		slog.Info("mcp: spec refresh disabled; the tool set is a startup snapshot")
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		raw, err := fetch(ctx)
		if err != nil {
			slog.Warn("mcp: spec fetch failed; the tool set in force is unchanged", "error", err)
			continue
		}
		changed, err := s.Apply(raw)
		if err != nil {
			slog.Warn("mcp: spec did not parse; the tool set in force is unchanged", "error", err)
			continue
		}
		if changed {
			slog.Info("mcp: tool set reloaded from upstream spec", "tools", len(s.Tools()))
		}
	}
}
