// Package orderedmap provides the small insertion-ordered map needed by the vendored renderer.
package orderedmap

// OrderedMap stores values in insertion order.
type OrderedMap[Key comparable, Value any] struct {
	entries map[Key]*Element[Key, Value]
	front   *Element[Key, Value]
	back    *Element[Key, Value]
}

// Element is one entry in an OrderedMap.
type Element[Key comparable, Value any] struct {
	Key   Key
	Value Value
	next  *Element[Key, Value]
}

// NewOrderedMap returns an empty OrderedMap.
func NewOrderedMap[Key comparable, Value any]() *OrderedMap[Key, Value] {
	return &OrderedMap[Key, Value]{entries: make(map[Key]*Element[Key, Value])}
}

// Set inserts or replaces a value without changing its position.
func (self *OrderedMap[Key, Value]) Set(key Key, value Value) {
	if element, ok := self.entries[key]; ok {
		element.Value = value
		return
	}

	element := &Element[Key, Value]{Key: key, Value: value}
	self.entries[key] = element
	if self.back == nil {
		self.front = element
		self.back = element
		return
	}

	self.back.next = element
	self.back = element
}

// Get returns the value stored under key.
func (self *OrderedMap[Key, Value]) Get(key Key) (Value, bool) {
	element, ok := self.entries[key]
	if !ok {
		var zero Value
		return zero, false
	}

	return element.Value, true
}

// Front returns the first element, or nil when the map is empty.
func (self *OrderedMap[Key, Value]) Front() *Element[Key, Value] {
	return self.front
}

// Next returns the element after this one, or nil at the end.
func (self *Element[Key, Value]) Next() *Element[Key, Value] {
	return self.next
}
