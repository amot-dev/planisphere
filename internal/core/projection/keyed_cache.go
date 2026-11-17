package projection

import (
	"errors"
	"sync"
)

// KeyedCache holds a generation-guarded map of cached values and implements Projection.
type KeyedCache[K comparable, V any] struct {
	name     string
	triggers Triggers

	mu         sync.Mutex
	entries    map[K]V
	generation uint64
}

// NewKeyedCache creates a keyed projection cache and registers it on bus when non-nil.
func NewKeyedCache[K comparable, V any](
	bus *Bus,
	name string,
	triggers Triggers,
) *KeyedCache[K, V] {
	cache := &KeyedCache[K, V]{
		name:     name,
		triggers: triggers,
		entries:  make(map[K]V),
	}
	if bus != nil {
		bus.Register(cache)
	}
	return cache
}

// Name returns the projection's diagnostic name.
func (cache *KeyedCache[K, V]) Name() string {
	return cache.name
}

// Triggers returns the journal and app events that invalidate this cache.
func (cache *KeyedCache[K, V]) Triggers() Triggers {
	return cache.triggers
}

// Invalidate drops all cached entries and increments the generation counter so any
// concurrent in-flight load cannot write stale results back.
func (cache *KeyedCache[K, V]) Invalidate() {
	cache.mu.Lock()
	cache.entries = make(map[K]V)
	cache.generation++
	cache.mu.Unlock()
}

// Len returns the number of entries currently cached.
func (cache *KeyedCache[K, V]) Len() int {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return len(cache.entries)
}

// Generation returns the monotonically increasing invalidation counter.
func (cache *KeyedCache[K, V]) Generation() uint64 {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.generation
}

// GetOrLoad returns the cached value for key or computes it via loadFn outside the mutex.
func (cache *KeyedCache[K, V]) GetOrLoad(key K, loadFn func() (V, error)) (V, error) {
	cache.mu.Lock()
	if cachedValue, exists := cache.entries[key]; exists {
		cache.mu.Unlock()
		return cachedValue, nil
	}
	capturedGeneration := cache.generation
	cache.mu.Unlock()

	if loadFn == nil {
		var zeroValue V
		return zeroValue, errors.New("keyed projection cache loader is nil")
	}
	loadedValue, loadError := loadFn()
	if loadError != nil {
		return loadedValue, loadError
	}

	cache.mu.Lock()
	if cache.generation == capturedGeneration {
		if existingValue, exists := cache.entries[key]; exists {
			loadedValue = existingValue
		} else {
			cache.entries[key] = loadedValue
		}
	}
	cache.mu.Unlock()
	return loadedValue, nil
}

// GetManyOrLoad resolves cached entries for keys and batch-loads only missing keys
// via loadMissingFn outside the mutex.
func (cache *KeyedCache[K, V]) GetManyOrLoad(
	keys []K,
	loadMissingFn func(missing []K) (map[K]V, error),
) (map[K]V, error) {
	if len(keys) == 0 {
		return map[K]V{}, nil
	}

	results := make(map[K]V, len(keys))
	var missingKeys []K
	seenMissing := make(map[K]bool, len(keys))

	cache.mu.Lock()
	capturedGeneration := cache.generation
	for _, key := range keys {
		if cachedValue, exists := cache.entries[key]; exists {
			results[key] = cachedValue
		} else if !seenMissing[key] {
			seenMissing[key] = true
			missingKeys = append(missingKeys, key)
		}
	}
	cache.mu.Unlock()

	if len(missingKeys) == 0 {
		return results, nil
	}
	if loadMissingFn == nil {
		return nil, errors.New("keyed projection batch loader is nil")
	}

	loadedEntries, loadError := loadMissingFn(missingKeys)
	if loadError != nil {
		return nil, loadError
	}

	cache.mu.Lock()
	if cache.generation == capturedGeneration {
		for key, value := range loadedEntries {
			cache.entries[key] = value
		}
	}
	cache.mu.Unlock()

	for key, value := range loadedEntries {
		results[key] = value
	}
	return results, nil
}

