package historyservice

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
)

// historyIndexCacheCapacity bounds how many saved histories keep their entry
// index in memory: the open history plus a few recently exported ones.
const historyIndexCacheCapacity = 4

// historyIndexCache keeps the entry indexes of the most recently used saved
// histories. It is not safe for concurrent use.
type historyIndexCache struct {
	// items is ordered from least to most recently used.
	items []cachedHistoryIndex
}

type cachedHistoryIndex struct {
	key   string
	index *historyFileIndex
}

// get returns the cached index of key and marks it most recently used.
func (c *historyIndexCache) get(key string) (*historyFileIndex, bool) {
	for i, item := range c.items {
		if item.key == key {
			c.items = append(slices.Delete(c.items, i, i+1), item)
			return item.index, true
		}
	}
	return nil, false
}

// put caches index as the most recently used, evicting the least recently
// used index beyond the capacity.
func (c *historyIndexCache) put(key string, index *historyFileIndex) {
	c.evict(key)
	if over := len(c.items) - historyIndexCacheCapacity + 1; over > 0 {
		c.items = slices.Delete(c.items, 0, over)
	}
	c.items = append(c.items, cachedHistoryIndex{key: key, index: index})
}

func (c *historyIndexCache) evict(key string) {
	c.items = slices.DeleteFunc(c.items, func(item cachedHistoryIndex) bool { return item.key == key })
}

func (c *historyIndexCache) has(key string) bool {
	return slices.ContainsFunc(c.items, func(item cachedHistoryIndex) bool { return item.key == key })
}

func (c *historyIndexCache) len() int {
	return len(c.items)
}

type historyFileIndex struct {
	formatVersion uint16
	entries       *orderedIndexList
}

type historyIndex struct {
	headerIndex uint32
	bodyIndex   uint32
}

type orderedIndexList struct {
	mu    sync.RWMutex
	hidxs []historyIndex
	index map[uint64]int
}

func newOrderedIndexList() *orderedIndexList {
	return &orderedIndexList{
		index: map[uint64]int{},
	}
}

func (l *orderedIndexList) Set(id uint64, hidx historyIndex) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if idx, exists := l.index[id]; exists {
		l.hidxs[idx] = hidx
	} else {
		l.hidxs = append(l.hidxs, hidx)
		l.index[id] = len(l.hidxs) - 1
	}
}

func (l *orderedIndexList) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.hidxs)
}

func (l *orderedIndexList) Get(id uint64) (historyIndex, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if idx, exists := l.index[id]; !exists {
		return historyIndex{}, false
	} else {
		return l.hidxs[idx], true
	}
}

func (l *orderedIndexList) ForEachValue(f func(hidx historyIndex) bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, hidx := range l.hidxs {
		if !f(hidx) {
			break
		}
	}
}

// historyIndexRecordSize is the size of one index record: the entry ID and the
// offsets of its header and body.
const historyIndexRecordSize = 8 + 4 + 4

// checkHistoryIndexFile reports whether path holds as many index records as
// its count says, without loading them.
func checkHistoryIndexFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var entryCount uint32
	if err := binary.Read(file, binary.BigEndian, &entryCount); err != nil {
		return fmt.Errorf("failed to read entry count: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if want := 4 + int64(entryCount)*historyIndexRecordSize; info.Size() < want {
		return fmt.Errorf("history index has %d bytes, want %d for %d entries", info.Size(), want, entryCount)
	}
	return nil
}

func initializeIndexMap(r io.Reader, indexMap *orderedIndexList) error {
	var entryCount uint32
	if err := binary.Read(r, binary.BigEndian, &entryCount); err != nil {
		return fmt.Errorf("failed to read entry count: %w", err)
	}
	for i := uint32(0); i < entryCount; i++ {
		var id uint64
		if err := binary.Read(r, binary.BigEndian, &id); err != nil {
			return fmt.Errorf("failed to read entry id at index %d: %w", i, err)
		}
		var headerIndex uint32
		if err := binary.Read(r, binary.BigEndian, &headerIndex); err != nil {
			return fmt.Errorf("failed to read header index at index %d: %w", i, err)
		}
		var bodyIndex uint32
		if err := binary.Read(r, binary.BigEndian, &bodyIndex); err != nil {
			return fmt.Errorf("failed to read body index at index %d: %w", i, err)
		}
		indexMap.Set(id, historyIndex{
			headerIndex: headerIndex,
			bodyIndex:   bodyIndex,
		})
	}
	return nil
}
