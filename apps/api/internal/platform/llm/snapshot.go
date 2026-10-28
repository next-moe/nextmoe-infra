package llm

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"slices"
	"strconv"
	"time"

	"gorm.io/gorm"
)

type snapshot struct {
	providers map[int64]*Provider
	models    map[int64]*Model
	routes    map[int64][]*Route
	scenes    map[string]*Scene
	prices    map[int64]*PriceRevision
	keys      map[int64]string
	headers   map[int64]map[string]string
	loadedAt  time.Time
}

func loadSnapshot(ctx context.Context, db *gorm.DB, kek []byte) (*snapshot, error) {
	db = db.WithContext(ctx)
	var (
		providers []*Provider
		models    []*Model
		routes    []*Route
		scenes    []*Scene
		prices    []*PriceRevision
	)
	for _, q := range []struct {
		dst  any
		name string
	}{{&providers, "providers"}, {&models, "models"}, {&routes, "routes"}, {&scenes, "scenes"}} {
		if err := db.Find(q.dst).Error; err != nil {
			return nil, fmt.Errorf("llm snapshot %s: %w", q.name, err)
		}
	}
	if err := db.Where("id IN (SELECT price_id FROM llm_route WHERE price_id IS NOT NULL)").Find(&prices).Error; err != nil {
		return nil, fmt.Errorf("llm snapshot prices: %w", err)
	}
	s := &snapshot{
		providers: map[int64]*Provider{}, models: map[int64]*Model{}, routes: map[int64][]*Route{},
		scenes: map[string]*Scene{}, prices: map[int64]*PriceRevision{},
		keys: map[int64]string{}, headers: map[int64]map[string]string{}, loadedAt: time.Now(),
	}
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		key, err := providerKey(p, kek)
		if err != nil {
			return nil, err
		}
		s.providers[p.ID], s.keys[p.ID] = p, key
		var h map[string]string
		if len(p.Headers) > 0 {
			if err := json.Unmarshal(p.Headers, &h); err != nil {
				return nil, fmt.Errorf("llm provider %s headers: %w", p.Name, err)
			}
		}
		s.headers[p.ID] = h
	}
	for _, m := range models {
		if m.Enabled {
			s.models[m.ID] = m
		}
	}
	for _, r := range routes {
		if r.Status == RouteActive && s.providers[r.ProviderID] != nil && s.models[r.ModelID] != nil {
			s.routes[r.ModelID] = append(s.routes[r.ModelID], r)
		}
	}
	for _, sc := range scenes {
		s.scenes[sc.Key] = sc
	}
	for _, p := range prices {
		s.prices[p.ID] = p
	}
	return s, nil
}

func providerKey(p *Provider, kek []byte) (string, error) {
	if p.KeyEnv != "" {
		return os.Getenv(p.KeyEnv), nil
	}
	if len(p.KeyCipher) == 0 {
		return "", nil
	}
	key, err := openKey(kek, p.KeyCipher)
	if err != nil {
		return "", fmt.Errorf("llm provider %s key: %w", p.Name, err)
	}
	return key, nil
}

type candidate struct {
	route    *Route
	model    *Model
	provider *Provider
	forced   bool
}

func (s *snapshot) plan(sc *Scene, affinity string, h *health) []candidate {
	chain := []int64{sc.ModelID}
	var fallbacks []int64
	if len(sc.FallbackModelIDs) > 0 && json.Unmarshal(sc.FallbackModelIDs, &fallbacks) == nil {
		chain = append(chain, fallbacks...)
	}
	var out, blocked []candidate
	var until []time.Time
	for _, mid := range chain {
		m := s.models[mid]
		if m == nil {
			continue
		}
		routes := slices.Clone(s.routes[mid])
		slices.SortStableFunc(routes, func(a, b *Route) int { return cmp.Compare(b.Priority, a.Priority) })
		for start := 0; start < len(routes); {
			end := start
			for end < len(routes) && routes[end].Priority == routes[start].Priority {
				end++
			}
			for _, r := range rendezvous(routes[start:end], affinity) {
				c := candidate{route: r, model: m, provider: s.providers[r.ProviderID]}
				if t, ok := h.blockedUntil(r.ID); ok {
					blocked, until = append(blocked, c), append(until, t)
					continue
				}
				out = append(out, c)
			}
			start = end
		}
	}
	if len(out) == 0 && len(blocked) > 0 {
		best := 0
		for i := range blocked {
			if until[i].Before(until[best]) {
				best = i
			}
		}
		blocked[best].forced = true
		out = append(out, blocked[best])
	}
	return out
}

func rendezvous(routes []*Route, key string) []*Route {
	if key == "" {
		key = strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	type scored struct {
		r *Route
		s float64
	}
	sc := make([]scored, len(routes))
	for i, r := range routes {
		h := fnv.New64a()
		_, _ = h.Write([]byte(key))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(strconv.FormatInt(r.ID, 10)))
		u := (float64(mix(h.Sum64())>>11) + 0.5) / (1 << 53)
		w := float64(max(r.Weight, 1))
		sc[i] = scored{r, -w / math.Log(u)}
	}
	slices.SortStableFunc(sc, func(a, b scored) int { return cmp.Compare(b.s, a.s) })
	out := make([]*Route, len(sc))
	for i := range sc {
		out[i] = sc[i].r
	}
	return out
}

func mix(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	return x ^ x>>31
}
