package widgets

import "time"

// IconLoader starts loading key and calls finish on the main thread with the
// result; ok is false when there is no icon.
type IconLoader[V any] func(key string, finish func(value V, ok bool))

// How long a missing icon waits before another load is tried. A backend file
// removed externally can be recovered on a later read.
const iconRetryDelay = 10 * time.Second

type iconEntry[V any] struct {
	key   string
	value V
}

// IconCache keeps the most recently used icons and merges concurrent loads of
// one key. All methods run on the main thread.
type IconCache[V any] struct {
	maxSize    int
	loader     IconLoader[V]
	now        func() time.Time
	order      []iconEntry[V] // oldest first
	pending    map[string]bool
	missing    map[string]time.Time
	generation uint64
}

// NewIconCache returns an empty cache of at most maxSize icons.
func NewIconCache[V any](maxSize int, loader IconLoader[V]) *IconCache[V] {
	return &IconCache[V]{
		maxSize: maxSize,
		loader:  loader,
		now:     time.Now,
		pending: map[string]bool{},
		missing: map[string]time.Time{},
	}
}

func (cache *IconCache[V]) index(key string) int {
	for index := range cache.order {
		if cache.order[index].key == key {
			return index
		}
	}
	return -1
}

// Get returns a cached icon and marks it recently used. For an unknown key it
// starts a load, unless one is running or the key was recently found missing.
func (cache *IconCache[V]) Get(key string) (value V, ok bool) {
	if index := cache.index(key); index >= 0 {
		entry := cache.order[index]
		cache.order = append(append(cache.order[:index:index], cache.order[index+1:]...), entry)
		return entry.value, true
	}
	cache.startLoad(key)
	return value, false
}

func (cache *IconCache[V]) startLoad(key string) {
	if cache.pending[key] {
		return
	}
	if at, wasMissing := cache.missing[key]; wasMissing && cache.now().Sub(at) < iconRetryDelay {
		return
	}
	cache.pending[key] = true
	generation := cache.generation
	cache.loader(key, func(value V, ok bool) {
		if generation != cache.generation {
			return
		}
		delete(cache.pending, key)
		if !ok {
			cache.missing[key] = cache.now()
			return
		}
		delete(cache.missing, key)
		cache.set(key, value)
	})
}

func (cache *IconCache[V]) set(key string, value V) {
	if index := cache.index(key); index >= 0 {
		cache.order = append(cache.order[:index], cache.order[index+1:]...)
	}
	cache.order = append(cache.order, iconEntry[V]{key: key, value: value})
	if overflow := len(cache.order) - cache.maxSize; overflow > 0 {
		cache.order = append(cache.order[:0:0], cache.order[overflow:]...)
	}
}

// Delete forgets one icon, so its next Get loads it again.
func (cache *IconCache[V]) Delete(key string) {
	if index := cache.index(key); index >= 0 {
		cache.order = append(cache.order[:index], cache.order[index+1:]...)
	}
	delete(cache.missing, key)
}

// Clear forgets every icon and discards loads still running.
func (cache *IconCache[V]) Clear() {
	cache.generation++
	cache.order = nil
	cache.pending = map[string]bool{}
	cache.missing = map[string]time.Time{}
}

// Len returns how many icons are cached.
func (cache *IconCache[V]) Len() int { return len(cache.order) }
