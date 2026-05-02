package main

import (
	"sync"
	"sync/atomic"
)

// Pool defines the interface for backend pool operations used by selectors and forwarders
type Pool interface {
	GetBackend(key string) *Backend
	GetBackends() []*Backend
	OnConnect(backend *Backend)
	OnDisconnect(backend *Backend)
	AddBytes(backend *Backend, bytes int64)
	CheckIP(key string) bool
	GetRoundRobinIndex() uint64
}

// MultiPool aggregates multiple BackendPools into a single Pool
type MultiPool struct {
	pools           []*BackendPool
	roundRobinIndex atomic.Uint64
	mu              sync.RWMutex
}

// NewMultiPool creates a MultiPool from multiple BackendPools
func NewMultiPool(pools []*BackendPool) *MultiPool {
	return &MultiPool{
		pools: pools,
	}
}

func (mp *MultiPool) GetBackend(key string) *Backend {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	for _, pool := range mp.pools {
		pool.mu.RLock()
		for _, b := range pool.backendList {
			if b.IP == key || b.IP+":"+b.Port == key {
				pool.mu.RUnlock()
				return b
			}
		}
		pool.mu.RUnlock()
	}
	return nil
}

func (mp *MultiPool) GetBackends() []*Backend {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	var all []*Backend
	for _, pool := range mp.pools {
		all = append(all, pool.GetBackends()...)
	}
	return all
}

func (mp *MultiPool) OnConnect(backend *Backend) {
	backend.ActiveConns.Add(1)
	backend.TotalConns.Add(1)
}

func (mp *MultiPool) OnDisconnect(backend *Backend) {
	backend.ActiveConns.Add(-1)
}

func (mp *MultiPool) AddBytes(backend *Backend, bytes int64) {
	backend.TotalBytes.Add(uint64(bytes))
}

func (mp *MultiPool) CheckIP(key string) bool {
	return mp.GetBackend(key) != nil
}

func (mp *MultiPool) GetRoundRobinIndex() uint64 {
	return mp.roundRobinIndex.Add(1)
}
