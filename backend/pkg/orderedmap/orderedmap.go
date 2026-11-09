package orderedmap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"iter"
	"sync"
)

// ========================
// 核心数据结构
// ========================

// Element 双向链表节点
type Element[K comparable, V any] struct {
	key        K
	Value      V
	prev, next *Element[K, V]
}

func (e *Element[K, V]) Key() K { return e.key }

// OrderedMap 有序 Map（插入顺序）
// 零值不可用，请使用 New() 创建
type OrderedMap[K comparable, V any] struct {
	mu    sync.RWMutex
	items map[K]*Element[K, V]
	head  *Element[K, V] // 哨兵头节点
	tail  *Element[K, V] // 哨兵尾节点
	len   int
}

// ========================
// 构造函数
// ========================

// New 创建一个新的 OrderedMap
func New[K comparable, V any]() *OrderedMap[K, V] {
	om := &OrderedMap[K, V]{
		items: make(map[K]*Element[K, V]),
	}
	// 初始化哨兵节点，简化边界处理
	om.head = &Element[K, V]{}
	om.tail = &Element[K, V]{}
	om.head.next = om.tail
	om.tail.prev = om.head
	return om
}

// NewWithCapacity 创建指定初始容量的 OrderedMap
func NewWithCapacity[K comparable, V any](capacity int) *OrderedMap[K, V] {
	om := &OrderedMap[K, V]{
		items: make(map[K]*Element[K, V], capacity),
	}
	om.head = &Element[K, V]{}
	om.tail = &Element[K, V]{}
	om.head.next = om.tail
	om.tail.prev = om.head
	return om
}

// ========================
// 核心操作
// ========================

// Set 设置键值对（存在则更新值并保持原位置，不存在则插入到末尾）
// 返回值：true 表示新增，false 表示更新
func (om *OrderedMap[K, V]) Set(key K, value V) bool {
	om.mu.Lock()
	defer om.mu.Unlock()

	if elem, ok := om.items[key]; ok {
		elem.Value = value
		return false
	}

	elem := &Element[K, V]{key: key, Value: value}
	om.insertBefore(om.tail, elem)
	om.items[key] = elem
	om.len++
	return true
}

// SetFront 设置键值对（插入到头部）
func (om *OrderedMap[K, V]) SetFront(key K, value V) bool {
	om.mu.Lock()
	defer om.mu.Unlock()

	if elem, ok := om.items[key]; ok {
		elem.Value = value
		// 移动到头部
		om.removeFromList(elem)
		om.insertAfter(om.head, elem)
		return false
	}

	elem := &Element[K, V]{key: key, Value: value}
	om.insertAfter(om.head, elem)
	om.items[key] = elem
	om.len++
	return true
}

// Get 获取值
func (om *OrderedMap[K, V]) Get(key K) (V, bool) {
	om.mu.RLock()
	defer om.mu.RUnlock()

	if elem, ok := om.items[key]; ok {
		return elem.Value, true
	}
	var zero V
	return zero, false
}

// GetOrSet 获取值，不存在则设置默认值
func (om *OrderedMap[K, V]) GetOrSet(key K, defaultValue V) (actual V, loaded bool) {
	om.mu.Lock()
	defer om.mu.Unlock()

	if elem, ok := om.items[key]; ok {
		return elem.Value, true
	}

	elem := &Element[K, V]{key: key, Value: defaultValue}
	om.insertBefore(om.tail, elem)
	om.items[key] = elem
	om.len++
	return defaultValue, false
}

func (om *OrderedMap[K, V]) DeleteFront() (V, bool) {
	om.mu.Lock()
	defer om.mu.Unlock()
	if om.len == 0 {
		var zero V
		return zero, false
	}
	elem := om.head.next
	om.removeFromList(elem)
	delete(om.items, elem.key)
	om.len--
	return elem.Value, true
}

// Delete 删除键值对，返回被删除的值
func (om *OrderedMap[K, V]) Delete(key K) (V, bool) {
	om.mu.Lock()
	defer om.mu.Unlock()

	elem, ok := om.items[key]
	if !ok {
		var zero V
		return zero, false
	}

	om.removeFromList(elem)
	delete(om.items, key)
	om.len--
	return elem.Value, true
}

// Contains 判断键是否存在
func (om *OrderedMap[K, V]) Contains(key K) bool {
	om.mu.RLock()
	defer om.mu.RUnlock()
	_, ok := om.items[key]
	return ok
}

// Len 返回元素数量
func (om *OrderedMap[K, V]) Len() int {
	om.mu.RLock()
	defer om.mu.RUnlock()
	return om.len
}

// Front 返回第一个元素
func (om *OrderedMap[K, V]) Front() *Element[K, V] {
	om.mu.RLock()
	defer om.mu.RUnlock()
	if om.len == 0 {
		return nil
	}
	return om.head.next
}

// Back 返回最后一个元素
func (om *OrderedMap[K, V]) Back() *Element[K, V] {
	om.mu.RLock()
	defer om.mu.RUnlock()
	if om.len == 0 {
		return nil
	}
	return om.tail.prev
}

// Next 返回下一个元素（遍历时使用，注意并发安全）
func (om *OrderedMap[K, V]) Next(elem *Element[K, V]) *Element[K, V] {
	om.mu.RLock()
	defer om.mu.RUnlock()
	next := elem.next
	if next == om.tail {
		return nil
	}
	return next
}

// MoveToFront 将指定 key 移动到头部
func (om *OrderedMap[K, V]) MoveToFront(key K) bool {
	om.mu.Lock()
	defer om.mu.Unlock()

	elem, ok := om.items[key]
	if !ok {
		return false
	}
	om.removeFromList(elem)
	om.insertAfter(om.head, elem)
	return true
}

// MoveToBack 将指定 key 移动到尾部
func (om *OrderedMap[K, V]) MoveToBack(key K) bool {
	om.mu.Lock()
	defer om.mu.Unlock()

	elem, ok := om.items[key]
	if !ok {
		return false
	}
	om.removeFromList(elem)
	om.insertBefore(om.tail, elem)
	return true
}

// Clear 清空所有元素
func (om *OrderedMap[K, V]) Clear() {
	om.mu.Lock()
	defer om.mu.Unlock()

	var zeroK K
	var zeroV V
	// 断开链表上所有节点的指针，避免长链被意外引用
	for e := om.head.next; e != nil && e != om.tail; {
		next := e.next
		e.prev, e.next = nil, nil
		e.key, e.Value = zeroK, zeroV
		e = next
	}
	om.items = make(map[K]*Element[K, V])
	// 重置哨兵与长度
	om.head.next = om.tail
	om.tail.prev = om.head
	om.len = 0
}

// ========================
// 批量操作
// ========================

// Keys 返回所有键（按插入顺序）
func (om *OrderedMap[K, V]) Keys() []K {
	om.mu.RLock()
	defer om.mu.RUnlock()

	keys := make([]K, 0, om.len)
	for elem := om.head.next; elem != om.tail; elem = elem.next {
		keys = append(keys, elem.key)
	}
	return keys
}

// Values 返回所有值（按插入顺序）
func (om *OrderedMap[K, V]) Values() []V {
	om.mu.RLock()
	defer om.mu.RUnlock()

	values := make([]V, 0, om.len)
	for elem := om.head.next; elem != om.tail; elem = elem.next {
		values = append(values, elem.Value)
	}
	return values
}

// TailValues returns at most limit values from the newest end while preserving
// their insertion order. It walks only the requested window instead of first
// allocating a slice for the complete map.
func (om *OrderedMap[K, V]) TailValues(limit int) []V {
	if limit <= 0 {
		return nil
	}
	om.mu.RLock()
	defer om.mu.RUnlock()

	count := min(limit, om.len)
	values := make([]V, count)
	elem := om.tail.prev
	for index := count - 1; index >= 0; index-- {
		values[index] = elem.Value
		elem = elem.prev
	}
	return values
}

// All 返回正向迭代器（Go 1.23+）
func (om *OrderedMap[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		om.mu.RLock()
		defer om.mu.RUnlock()
		for elem := om.head.next; elem != om.tail; elem = elem.next {
			if !yield(elem.key, elem.Value) {
				return
			}
		}
	}
}

// Backward 返回反向迭代器（Go 1.23+）
func (om *OrderedMap[K, V]) Backward() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		om.mu.RLock()
		defer om.mu.RUnlock()
		for elem := om.tail.prev; elem != om.head; elem = elem.prev {
			if !yield(elem.key, elem.Value) {
				return
			}
		}
	}
}

// ForEach 遍历所有元素（函数返回 false 时停止）
func (om *OrderedMap[K, V]) ForEach(fn func(key K, value V) bool) {
	om.mu.RLock()
	defer om.mu.RUnlock()

	for elem := om.head.next; elem != om.tail; elem = elem.next {
		if !fn(elem.key, elem.Value) {
			return
		}
	}
}

// Filter 过滤元素，返回新的 OrderedMap
func (om *OrderedMap[K, V]) Filter(fn func(key K, value V) bool) *OrderedMap[K, V] {
	om.mu.RLock()
	defer om.mu.RUnlock()

	result := NewWithCapacity[K, V](om.len / 2)
	for elem := om.head.next; elem != om.tail; elem = elem.next {
		if fn(elem.key, elem.Value) {
			result.Set(elem.key, elem.Value)
		}
	}
	return result
}

// ========================
// 序列化支持
// ========================

// MarshalJSON 实现 json.Marshaler，保持键顺序
func (om *OrderedMap[K, V]) MarshalJSON() ([]byte, error) {
	om.mu.RLock()
	defer om.mu.RUnlock()

	var buf bytes.Buffer
	buf.WriteByte('{')

	first := true
	var err error
	for elem := om.head.next; elem != om.tail; elem = elem.next {
		if !first {
			buf.WriteByte(',')
		}
		first = false

		// 序列化 key
		keyBytes, e := json.Marshal(elem.key)
		if e != nil {
			err = fmt.Errorf("orderedmap: marshal key %v: %w", elem.key, e)
			break
		}
		buf.Write(keyBytes)
		buf.WriteByte(':')

		// 序列化 value
		valBytes, e := json.Marshal(elem.Value)
		if e != nil {
			err = fmt.Errorf("orderedmap: marshal value for key %v: %w", elem.key, e)
			break
		}
		buf.Write(valBytes)
	}

	if err != nil {
		return nil, err
	}

	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON 实现 json.Unmarshaler
func (om *OrderedMap[K, V]) UnmarshalJSON(data []byte) error {
	om.mu.Lock()
	defer om.mu.Unlock()

	// 使用 json.Decoder 保持顺序
	dec := json.NewDecoder(bytes.NewReader(data))

	// 读取 '{'
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("orderedmap: unmarshal: %w", err)
	}

	for dec.More() {
		// 读取 key
		keyToken, err := dec.Token()
		if err != nil {
			return fmt.Errorf("orderedmap: unmarshal key: %w", err)
		}

		keyStr, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("orderedmap: unmarshal: key must be string, got %T", keyToken)
		}

		// 将 string key 转换为 K 类型
		var key K
		if err := json.Unmarshal([]byte(`"`+keyStr+`"`), &key); err != nil {
			return fmt.Errorf("orderedmap: unmarshal key %q: %w", keyStr, err)
		}

		// 读取 value
		var value V
		if err := dec.Decode(&value); err != nil {
			return fmt.Errorf("orderedmap: unmarshal value for key %q: %w", keyStr, err)
		}

		// 内部直接操作，避免重复加锁
		elem := &Element[K, V]{key: key, Value: value}
		om.insertBefore(om.tail, elem)
		om.items[key] = elem
		om.len++
	}

	return nil
}

// String 实现 fmt.Stringer
func (om *OrderedMap[K, V]) String() string {
	om.mu.RLock()
	defer om.mu.RUnlock()

	var buf bytes.Buffer
	buf.WriteString("OrderedMap{")
	first := true
	for elem := om.head.next; elem != om.tail; elem = elem.next {
		if !first {
			buf.WriteString(", ")
		}
		first = false
		fmt.Fprintf(&buf, "%v:%v", elem.key, elem.Value)
	}
	buf.WriteByte('}')
	return buf.String()
}

// ========================
// 内部链表操作（非线程安全，调用前需加锁）
// ========================

// insertAfter 在 at 节点之后插入 elem
func (om *OrderedMap[K, V]) insertAfter(at, elem *Element[K, V]) {
	elem.prev = at
	elem.next = at.next
	at.next.prev = elem
	at.next = elem
}

// insertBefore 在 at 节点之前插入 elem
func (om *OrderedMap[K, V]) insertBefore(at, elem *Element[K, V]) {
	om.insertAfter(at.prev, elem)
}

// removeFromList 从链表中移除 elem
func (om *OrderedMap[K, V]) removeFromList(elem *Element[K, V]) {
	elem.prev.next = elem.next
	elem.next.prev = elem.prev
	elem.prev = nil
	elem.next = nil
}
