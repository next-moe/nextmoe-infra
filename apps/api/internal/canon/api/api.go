// Package api is the v3 catalog read face over canon (D5, D20): no envelope,
// problem+json errors, decimal-string ids, opaque cursors and an ids= lane.
package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	"api/internal/canon/ent"
	"api/internal/canon/takedown"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const maxBatch = 100

type API struct {
	client *ent.Client
}

func New(client *ent.Client) (http.Handler, huma.API) {
	mux := http.NewServeMux()
	h := humago.New(mux, huma.DefaultConfig("NextMoe Catalog v3", "0.1.0"))
	a := &API{client: client}
	a.works(h)
	a.releases(h)
	a.people(h)
	a.xrefs(h)
	return mux, h
}

type IDInput struct {
	ID int64 `path:"id" minimum:"1"`
}

// gone answers 404 for a missing id and 410 for a whole-entity takedown.
func (a *API) gone(ctx context.Context, entity string, n int64, err error) error {
	if ent.IsNotFound(err) {
		return huma.Error404NotFound(entity + " " + id(n) + " does not exist")
	}
	if err != nil {
		return err
	}
	td, err := takedown.Gone(ctx, a.client, entity, n)
	if err != nil {
		return err
	}
	if td != nil {
		return huma.Error410Gone(entity + " " + id(n) + " was taken down: " + td.Reason)
	}
	return nil
}

func (a *API) goneSet(ctx context.Context, entity string, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := a.client.QueryContext(ctx, `SELECT entity_id FROM canon.takedown WHERE entity = $1 AND scope = 'entity'
		AND restored_at IS NULL AND entity_id = ANY($2)`, entity, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

func parseIDs(s string) ([]int64, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) > maxBatch {
		return nil, huma.Error400BadRequest("ids= takes at most " + strconv.Itoa(maxBatch) + " ids")
	}
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || n <= 0 {
			return nil, huma.Error400BadRequest("ids= must be positive decimal ids")
		}
		out = append(out, n)
	}
	return out, nil
}

func encodeCursor(last int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte("id:" + id(last)))
}

func decodeCursor(c string) (int64, error) {
	if c == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	s, ok := strings.CutPrefix(string(b), "id:")
	n, perr := strconv.ParseInt(s, 10, 64)
	if err != nil || !ok || perr != nil {
		return 0, huma.Error400BadRequest("cursor is not one this API issued")
	}
	return n, nil
}
