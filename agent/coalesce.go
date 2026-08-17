package agent

import (
	"iter"
	"strings"
)

// Coalescer incrementally combines adjacent text fragments. Add returns every complete event made
// available by the input; Flush releases text held for a later fragment.
type Coalescer struct {
	text strings.Builder
}

// Add takes an event and returns the complete events it makes available.
func (c *Coalescer) Add(event Event) []Event {
	if event.Kind == Text {
		c.text.WriteString(event.Text)
		return nil
	}

	out := c.Flush()
	return append(out, event)
}

// Flush releases text held for another fragment.
func (c *Coalescer) Flush() []Event {
	if c.text.Len() == 0 {
		return nil
	}

	event := Event{Kind: Text, Text: c.text.String()}
	c.text.Reset()
	return []Event{event}
}

// Coalesce combines runs of text fragments arrived via streaming into one coherent conversation.
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
