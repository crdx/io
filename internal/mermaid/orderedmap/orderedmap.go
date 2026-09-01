package orderedmap

type OrderedMap[Key comparable, Value any] struct {
	entries map[Key]*Element[Key, Value]
	front   *Element[Key, Value]
	back    *Element[Key, Value]
}

type Element[Key comparable, Value any] struct {
	Key   Key
	Value Value
	next  *Element[Key, Value]
}

func NewOrderedMap[Key comparable, Value any]() *OrderedMap[Key, Value] {
	return &OrderedMap[Key, Value]{entries: make(map[Key]*Element[Key, Value])}
}

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

func (self *OrderedMap[Key, Value]) Get(key Key) (Value, bool) {
	element, ok := self.entries[key]
	if !ok {
		var zero Value
		return zero, false
	}

	return element.Value, true
}

func (self *OrderedMap[Key, Value]) Front() *Element[Key, Value] {
	return self.front
}

func (self *Element[Key, Value]) Next() *Element[Key, Value] {
	return self.next
}
