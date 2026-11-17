package projection

import (
	"errors"
	"sync"
)

// Cache holds a single generation-guarded cached value and implements Projection.
type Cache[V any] struct {
	name     string
	triggers Triggers
	loadFn   func() (V, error)

	mu         sync.Mutex
	cached     V
	valid      bool
	generation uint64
}

// NewCache creates a single-value projection cache and registers it on bus when non-nil.
func NewCache[V any](
	bus *Bus,
	name string,
	triggers Triggers,
	loadFn func() (V, error),
) *Cache[V] {
	cache := &Cache[V]{
		name:     name,
		triggers: triggers,
		loadFn:   loadFn,
	}
	if bus != nil {
		bus.Register(cache)
	}
	return cache
}

// Name returns the projection's diagnostic name.
func (cache *Cache[V]) Name() string {
	return cache.name
}

// Triggers returns the journal and app events that invalidate this cache.
func (cache *Cache[V]) Triggers() Triggers {
	return cache.triggers
}

// Invalidate clears the cached value and increments the generation counter so any
// concurrent in-flight load cannot write stale results back.
func (cache *Cache[V]) Invalidate() {
	cache.mu.Lock()
	var zeroValue V
	cache.cached = zeroValue
	cache.valid = false
	cache.generation++
	cache.mu.Unlock()
}

// IsValid reports whether a computed value is currently cached.
func (cache *Cache[V]) IsValid() bool {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.valid
}

// Generation returns the monotonically increasing invalidation counter.
func (cache *Cache[V]) Generation() uint64 {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.generation
}

// Get returns the cached value or computes it via loadFn outside the mutex.
func (cache *Cache[V]) Get() (V, error) {
	cache.mu.Lock()
	if cache.valid {
		cachedValue := cache.cached
		cache.mu.Unlock()
		return cachedValue, nil
	}
	capturedGeneration := cache.generation
	loadFunc := cache.loadFn
	cache.mu.Unlock()

	if loadFunc == nil {
		var zeroValue V
		return zeroValue, errors.New("projection cache loader is nil")
	}
	loadedValue, loadError := loadFunc()
	if loadError != nil {
		return loadedValue, loadError
	}

	cache.mu.Lock()
	if !cache.valid && cache.generation == capturedGeneration {
		cache.cached = loadedValue
		cache.valid = true
	} else if cache.valid {
		loadedValue = cache.cached
	}
	cache.mu.Unlock()
	return loadedValue, nil
}

