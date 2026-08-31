package access

import (
	"strings"
	"sync"
)

type Definition[Value any] struct {
	Clone    func(Value) Value
	Describe func(known Value, current Value) string
}

type State[Value any] struct {
	definition Definition[Value]

	mutex        sync.Mutex
	currentValue Value
	knownValue   Value
}

func New[Value any](current Value, definition Definition[Value]) *State[Value] {
	return NewRestored(current, current, definition)
}

func NewRestored[Value any](current Value, known Value, definition Definition[Value]) *State[Value] {
	return &State[Value]{
		definition:   definition,
		currentValue: definition.Clone(current),
		knownValue:   definition.Clone(known),
	}
}

func (self *State[Value]) GetCurrent() Value {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.definition.Clone(self.currentValue)
}

func (self *State[Value]) Change(transform func(Value) Value) Value {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	current := self.definition.Clone(self.currentValue)
	self.currentValue = self.definition.Clone(transform(current))
	return self.definition.Clone(self.currentValue)
}

func (self *State[Value]) Replace(current Value) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.currentValue = self.definition.Clone(current)
}

func (self *State[Value]) Inject() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	message := self.definition.Describe(
		self.definition.Clone(self.knownValue),
		self.definition.Clone(self.currentValue),
	)
	self.knownValue = self.definition.Clone(self.currentValue)
	return message
}

type Teller interface {
	Inject() string
}

type Group struct {
	tellers []Teller
}

func NewGroup(tellers ...Teller) Group {
	return Group{tellers: append([]Teller(nil), tellers...)}
}

func (self Group) Inject() string {
	var messages []string
	for _, teller := range self.tellers {
		if message := teller.Inject(); message != "" {
			messages = append(messages, message)
		}
	}
	return strings.Join(messages, " ")
}
