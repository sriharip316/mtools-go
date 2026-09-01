package load

import (
	mathrand "math/rand"
	"sync"
	"time"
)

// OpType represents a CRUD operation type in the simulation.
type OpType int

const (
	OpCreate OpType = iota
	OpRead
	OpUpdate
	OpDelete
)

func (op OpType) String() string {
	switch op {
	case OpCreate:
		return "create"
	case OpRead:
		return "read"
	case OpUpdate:
		return "update"
	case OpDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// CollectionSampler selects a collection according to its configured weight.
type CollectionSampler struct {
	mu          sync.Mutex
	rng         *mathrand.Rand
	collections []CollectionConfig
	cumWeights  []float64
	totalWeight float64
}

// NewCollectionSampler creates a new weighted collection sampler.
func NewCollectionSampler(colls []CollectionConfig) *CollectionSampler {
	s := &CollectionSampler{
		rng:         mathrand.New(mathrand.NewSource(time.Now().UnixNano())),
		collections: colls,
		cumWeights:  make([]float64, len(colls)),
	}

	total := 0.0
	for i, c := range colls {
		w := c.Weight
		if w <= 0 {
			w = 1.0
		}
		total += w
		s.cumWeights[i] = total
	}
	s.totalWeight = total

	return s
}

// Sample selects a CollectionConfig based on its configured weight.
func (s *CollectionSampler) Sample() *CollectionConfig {
	if len(s.collections) == 0 {
		return nil
	}
	if len(s.collections) == 1 {
		return &s.collections[0]
	}

	s.mu.Lock()
	r := s.rng.Float64() * s.totalWeight
	s.mu.Unlock()

	for i, cw := range s.cumWeights {
		if r <= cw {
			return &s.collections[i]
		}
	}

	return &s.collections[len(s.collections)-1]
}

// CRUDSampler selects a CRUD operation type (Create, Read, Update, Delete) based on configured weights.
type CRUDSampler struct {
	mu          sync.Mutex
	rng         *mathrand.Rand
	weights     [4]float64
	cumWeights  [4]float64
	totalWeight float64
}

// NewCRUDSampler creates a new CRUD operation sampler for a collection.
func NewCRUDSampler(weights CRUDWeights) *CRUDSampler {
	s := &CRUDSampler{
		rng: mathrand.New(mathrand.NewSource(time.Now().UnixNano())),
	}

	c := weights.Create
	r := weights.Read
	u := weights.Update
	d := weights.Delete

	if c < 0 {
		c = 0
	}
	if r < 0 {
		r = 0
	}
	if u < 0 {
		u = 0
	}
	if d < 0 {
		d = 0
	}

	if c+r+u+d == 0 {
		c, r, u, d = 20, 50, 25, 5
	}

	s.weights[0] = c
	s.weights[1] = r
	s.weights[2] = u
	s.weights[3] = d

	total := 0.0
	for i, w := range s.weights {
		total += w
		s.cumWeights[i] = total
	}
	s.totalWeight = total

	return s
}

// Sample returns a CRUD OpType (OpCreate, OpRead, OpUpdate, OpDelete).
func (s *CRUDSampler) Sample() OpType {
	s.mu.Lock()
	r := s.rng.Float64() * s.totalWeight
	s.mu.Unlock()

	if r <= s.cumWeights[0] {
		return OpCreate
	}
	if r <= s.cumWeights[1] {
		return OpRead
	}
	if r <= s.cumWeights[2] {
		return OpUpdate
	}
	return OpDelete
}
