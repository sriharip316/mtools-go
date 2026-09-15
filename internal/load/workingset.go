package load

import (
	"math/rand"
	"sync"
	"time"
)

const defaultMaxWorkingSetSize = 100000

// WorkingSet maintains a thread-safe in-memory pool of active document IDs per collection.
type WorkingSet struct {
	mu      sync.RWMutex
	maxSize int
	pools   map[string]*idPool
	rng     *rand.Rand
	rngMu   sync.Mutex
}

type idPool struct {
	items []any
	index map[any]int
}

// NewWorkingSet creates a new WorkingSet with default or specified capacity.
func NewWorkingSet(maxSize int) *WorkingSet {
	if maxSize <= 0 {
		maxSize = defaultMaxWorkingSetSize
	}
	return &WorkingSet{
		maxSize: maxSize,
		pools:   make(map[string]*idPool),
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Add inserts an ID into the collection's active working set.
func (ws *WorkingSet) Add(coll string, id any) {
	if id == nil {
		return
	}

	ws.mu.Lock()
	defer ws.mu.Unlock()

	p, ok := ws.pools[coll]
	if !ok {
		p = &idPool{
			items: make([]any, 0, 1024),
			index: make(map[any]int),
		}
		ws.pools[coll] = p
	}

	if _, exists := p.index[id]; exists {
		return
	}

	if len(p.items) >= ws.maxSize {
		// Evict a random item to maintain bounded memory
		ws.rngMu.Lock()
		evictIdx := ws.rng.Intn(len(p.items))
		ws.rngMu.Unlock()

		evictedID := p.items[evictIdx]
		delete(p.index, evictedID)

		p.items[evictIdx] = id
		p.index[id] = evictIdx
		return
	}

	p.items = append(p.items, id)
	p.index[id] = len(p.items) - 1
}

// Sample randomly selects an ID from the collection's active working set.
func (ws *WorkingSet) Sample(coll string) (any, bool) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	p, ok := ws.pools[coll]
	if !ok || len(p.items) == 0 {
		return nil, false
	}

	ws.rngMu.Lock()
	idx := ws.rng.Intn(len(p.items))
	ws.rngMu.Unlock()

	return p.items[idx], true
}

// Remove removes an ID from the collection's active working set (e.g. after a Delete operation).
func (ws *WorkingSet) Remove(coll string, id any) bool {
	if id == nil {
		return false
	}

	ws.mu.Lock()
	defer ws.mu.Unlock()

	p, ok := ws.pools[coll]
	if !ok {
		return false
	}

	idx, exists := p.index[id]
	if !exists {
		return false
	}

	lastIdx := len(p.items) - 1
	if idx != lastIdx {
		lastItem := p.items[lastIdx]
		p.items[idx] = lastItem
		p.index[lastItem] = idx
	}

	p.items = p.items[:lastIdx]
	delete(p.index, id)

	return true
}

// Size returns the count of active IDs in the pool for a collection.
func (ws *WorkingSet) Size(coll string) int {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	p, ok := ws.pools[coll]
	if !ok {
		return 0
	}
	return len(p.items)
}
