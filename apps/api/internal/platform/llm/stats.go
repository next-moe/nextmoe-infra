package llm

import (
	"math/rand/v2"
	"sync"
)

const (
	latencyAlpha   = 0.2
	exploreLatency = 0.1
)

type routeStats struct {
	mu          sync.Mutex
	inflight    map[int64]int
	latency     map[int64]float64
	exploreRate float64
}

func newRouteStats() *routeStats {
	return &routeStats{inflight: map[int64]int{}, latency: map[int64]float64{}, exploreRate: exploreLatency}
}

func (s *routeStats) begin(id int64) func() {
	s.mu.Lock()
	s.inflight[id]++
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.inflight[id]--
		s.mu.Unlock()
	}
}

func (s *routeStats) observe(id int64, ms float64) {
	if ms <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.latency[id]; ok {
		s.latency[id] = prev + latencyAlpha*(ms-prev)
		return
	}
	s.latency[id] = ms
}

func (s *routeStats) inflightOf(rs []*Route) map[int64]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]int, len(rs))
	for _, r := range rs {
		out[r.ID] = s.inflight[r.ID]
	}
	return out
}

// latencyOf ranks a route nobody has timed yet first, so a new route gets
// measured instead of starving behind the ones that already have numbers.
func (s *routeStats) latencyOf(rs []*Route) map[int64]float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]float64, len(rs))
	for _, r := range rs {
		out[r.ID] = s.latency[r.ID]
	}
	return out
}

func (s *routeStats) latencyMs(id int64) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latency[id]
}

func (s *routeStats) explore() bool { return rand.Float64() < s.exploreRate }
