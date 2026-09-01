package historyservice

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

type historyIndexMap map[string]*historyFileIndex

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
