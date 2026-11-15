package store

type orderedSlot[V any] struct {
	value V
	seq   uint64
}

type orderedKey struct {
	id  uint64
	seq uint64
}

// orderedMap is a map that remembers insertion order, so its oldest key can be
// removed in amortized constant time. Replacing a value keeps its position.
type orderedMap[V any] struct {
	slots map[uint64]orderedSlot[V]
	order []orderedKey
	head  int
	seq   uint64
}

func newOrderedMap[V any]() *orderedMap[V] {
	return &orderedMap[V]{slots: make(map[uint64]orderedSlot[V])}
}

func (m *orderedMap[V]) Len() int { return len(m.slots) }

func (m *orderedMap[V]) Has(id uint64) bool {
	_, ok := m.slots[id]
	return ok
}

func (m *orderedMap[V]) Get(id uint64) (V, bool) {
	slot, ok := m.slots[id]
	return slot.value, ok
}

func (m *orderedMap[V]) Set(id uint64, value V) {
	if slot, ok := m.slots[id]; ok {
		slot.value = value
		m.slots[id] = slot
		return
	}
	m.seq++
	m.slots[id] = orderedSlot[V]{value: value, seq: m.seq}
	m.order = append(m.order, orderedKey{id: id, seq: m.seq})
}

func (m *orderedMap[V]) Delete(id uint64) { delete(m.slots, id) }

func (m *orderedMap[V]) live(key orderedKey) bool {
	slot, ok := m.slots[key.id]
	return ok && slot.seq == key.seq
}

// Oldest returns the entry inserted first.
func (m *orderedMap[V]) Oldest() (id uint64, value V, ok bool) {
	for m.head < len(m.order) {
		key := m.order[m.head]
		if m.live(key) {
			return key.id, m.slots[key.id].value, true
		}
		m.head++
	}
	m.order, m.head = m.order[:0], 0
	return 0, value, false
}

// Values returns the values in insertion order.
func (m *orderedMap[V]) Values() []V {
	values := make([]V, 0, len(m.slots))
	m.Range(func(_ uint64, value V) { values = append(values, value) })
	return values
}

// Range calls fn for each entry in insertion order.
func (m *orderedMap[V]) Range(fn func(id uint64, value V)) {
	for _, key := range m.order[m.head:] {
		if m.live(key) {
			fn(key.id, m.slots[key.id].value)
		}
	}
}

func (m *orderedMap[V]) Clear() {
	clear(m.slots)
	m.order, m.head = nil, 0
}

// DeleteOldest removes the entry inserted first.
func (m *orderedMap[V]) DeleteOldest() {
	if id, _, ok := m.Oldest(); ok {
		m.Delete(id)
	}
}

func (m *orderedMap[V]) compact() {
	if m.head < 1024 || m.head*2 < len(m.order) {
		return
	}
	m.order = append([]orderedKey(nil), m.order[m.head:]...)
	m.head = 0
}
