package agent

import (
	"iter"
	"strings"
)

// Coalescer incrementally combines adjacent prose fragments. Add returns every complete event made
// available by the input; Flush releases prose held for a later fragment.
type Coalescer struct {
	kind Kind
	text strings.Builder
}

// Add takes an event and returns the complete events it makes available.
func (self *Coalescer) Add(event Event) []Event {
	if event.Kind == ModelMessage || event.Kind == ModelReasoning {
		var out []Event
		if self.kind != "" && self.kind != event.Kind {
			out = self.Flush()
		}
		self.kind = event.Kind
		self.text.WriteString(event.Text)
		return out
	}

	out := self.Flush()
	return append(out, event)
}

// Flush releases prose held for another fragment.
func (self *Coalescer) Flush() []Event {
	if self.text.Len() == 0 {
		return nil
	}

	event := Event{Kind: self.kind, Text: self.text.String()}
	self.kind = ""
	self.text.Reset()
	return []Event{event}
}

// Coalesce combines runs of prose fragments arrived via streaming into one coherent conversation.
func Coalesce(events iter.Seq2[Event, error]) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		var held Coalescer

		release := func(events []Event) bool {
			for _, event := range events {
				if !yield(event, nil) {
					return false
				}
			}
			return true
		}

		for event, err := range events {
			if err != nil {
				if release(held.Flush()) {
					yield(Event{}, err)
				}
				return
			}

			if !release(held.Add(event)) {
				return
			}
		}

		release(held.Flush())
	}
}
