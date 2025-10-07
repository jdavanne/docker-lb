package main

import (
	"errors"
	"fmt"
	"math/rand/v2"
)

var (
	ErrNoBackends = errors.New("no backends available")
)

// BackendSelector defines the interface for backend selection algorithms
type BackendSelector interface {
	Select(pool *BackendPool, sourceIP string, affinity *AffinityMap) (*Backend, error)
	Name() string
}

// RandomSelector selects backends randomly
type RandomSelector struct{}

func (s *RandomSelector) Name() string {
	return "random"
}

func (s *RandomSelector) Select(pool *BackendPool, sourceIP string, affinity *AffinityMap) (*Backend, error) {
	// Check IP affinity first if enabled
	if affinity != nil && sourceIP != "" {
		if backendIP, found := affinity.Get(sourceIP); found {
			if backend := pool.GetBackend(backendIP); backend != nil {
				return backend, nil
			}
		}
	}

	backends := pool.GetBackends()
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}

	// Random selection
	n := rand.IntN(len(backends))
	selected := backends[n]

	// Update affinity if enabled
	if affinity != nil && sourceIP != "" {
		affinity.Set(sourceIP, selected.IP)
	}

	return selected, nil
}

// RoundRobinSelector selects backends in round-robin order
type RoundRobinSelector struct{}

func (s *RoundRobinSelector) Name() string {
	return "round-robin"
}

func (s *RoundRobinSelector) Select(pool *BackendPool, sourceIP string, affinity *AffinityMap) (*Backend, error) {
	// Check IP affinity first if enabled
	if affinity != nil && sourceIP != "" {
		if backendIP, found := affinity.Get(sourceIP); found {
			if backend := pool.GetBackend(backendIP); backend != nil {
				return backend, nil
			}
		}
	}

	backends := pool.GetBackends()
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}

	// Round-robin selection
	idx := pool.GetRoundRobinIndex() % uint64(len(backends))
	selected := backends[idx]

	// Update affinity if enabled
	if affinity != nil && sourceIP != "" {
		affinity.Set(sourceIP, selected.IP)
	}

	return selected, nil
}

// LeastConnectionSelector selects the backend with the fewest active connections
type LeastConnectionSelector struct{}

func (s *LeastConnectionSelector) Name() string {
	return "least-connection"
}

func (s *LeastConnectionSelector) Select(pool *BackendPool, sourceIP string, affinity *AffinityMap) (*Backend, error) {
	// Check IP affinity first if enabled
	if affinity != nil && sourceIP != "" {
		if backendIP, found := affinity.Get(sourceIP); found {
			if backend := pool.GetBackend(backendIP); backend != nil {
				return backend, nil
			}
		}
	}

	backends := pool.GetBackends()
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}

	// Find all backends with minimum connections
	minConns := int64(1<<63 - 1) // max int64
	var candidates []*Backend

	// First pass: find minimum connection count
	for _, backend := range backends {
		conns := backend.ActiveConns.Load()
		if conns < minConns {
			minConns = conns
		}
	}

	// Second pass: collect all backends with minimum connections
	for _, backend := range backends {
		if backend.ActiveConns.Load() == minConns {
			candidates = append(candidates, backend)
		}
	}

	// Randomly select from candidates with same minimum connections
	selected := candidates[rand.IntN(len(candidates))]

	// Update affinity if enabled
	if affinity != nil && sourceIP != "" {
		affinity.Set(sourceIP, selected.IP)
	}

	return selected, nil
}

// WeightedRandomSelector selects backends using weighted random selection
// By default, uses implicit weights from connection counts (inverse weighting)
// If explicit weights are configured, uses those instead
type WeightedRandomSelector struct {
	useExplicitWeights bool
}

func NewWeightedRandomSelector(useExplicitWeights bool) *WeightedRandomSelector {
	return &WeightedRandomSelector{
		useExplicitWeights: useExplicitWeights,
	}
}

func (s *WeightedRandomSelector) Name() string {
	return "weighted-random"
}

func (s *WeightedRandomSelector) Select(pool *BackendPool, sourceIP string, affinity *AffinityMap) (*Backend, error) {
	// Check IP affinity first if enabled
	if affinity != nil && sourceIP != "" {
		if backendIP, found := affinity.Get(sourceIP); found {
			if backend := pool.GetBackend(backendIP); backend != nil {
				return backend, nil
			}
		}
	}

	backends := pool.GetBackends()
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}

	// Calculate effective weights
	weights := make([]int, len(backends))
	totalWeight := 0

	if s.useExplicitWeights {
		// Use configured weights only (ignore connections)
		for i, b := range backends {
			weights[i] = b.Weight
			totalWeight += b.Weight
		}
	} else {
		// Use implicit weights from connection counts
		maxConns := int64(0)
		for _, b := range backends {
			if conns := b.ActiveConns.Load(); conns > maxConns {
				maxConns = conns
			}
		}

		for i, b := range backends {
			activeConns := b.ActiveConns.Load()
			// Inverse weighting: fewer connections = higher weight
			implicitWeight := int(maxConns - activeConns + 1)
			weights[i] = implicitWeight * b.Weight // multiply by explicit weight (default 1)
			totalWeight += weights[i]
		}
	}

	// Weighted random selection
	if totalWeight == 0 {
		// Fallback to random if all weights are 0
		n := rand.IntN(len(backends))
		selected := backends[n]
		if affinity != nil && sourceIP != "" {
			affinity.Set(sourceIP, selected.IP)
		}
		return selected, nil
	}

	r := rand.IntN(totalWeight)
	var selected *Backend
	for i, w := range weights {
		r -= w
		if r < 0 {
			selected = backends[i]
			break
		}
	}

	// Fallback to last backend if somehow we didn't select
	if selected == nil {
		selected = backends[len(backends)-1]
	}

	// Update affinity if enabled
	if affinity != nil && sourceIP != "" {
		affinity.Set(sourceIP, selected.IP)
	}

	return selected, nil
}

// NewSelector creates a backend selector based on the algorithm name
func NewSelector(algorithm string, hasExplicitWeights bool) (BackendSelector, error) {
	switch algorithm {
	case "random":
		return &RandomSelector{}, nil
	case "round-robin":
		return &RoundRobinSelector{}, nil
	case "least-connection":
		return &LeastConnectionSelector{}, nil
	case "weighted-random":
		return NewWeightedRandomSelector(hasExplicitWeights), nil
	default:
		return nil, fmt.Errorf("unknown load balancing algorithm: %s", algorithm)
	}
}
