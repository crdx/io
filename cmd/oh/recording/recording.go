package recording

import (
	"encoding/json"
	"errors"
	"fmt"

	"crdx.org/io/agent"
)

// Session is the stored-session interface needed while a conversation runs.
type Session interface {
	Stored() bool
	Name() string
	Event(agent.Event) error
	Item(json.RawMessage) error
	TakeWarnings() []error
}

// Recorder tracks canonical session writes and the provider-state append boundary.
type Recorder struct {
	session       Session
	flushBoundary int
}

// New constructs a recorder for a session.
func New(session Session) *Recorder {
	return &Recorder{session: session}
}

// Stored reports whether the session has durable content.
func (self *Recorder) Stored() bool {
	return self.session.Stored()
}

// Name returns the session name.
func (self *Recorder) Name() string {
	return self.session.Name()
}

// Resume advances the provider-state boundary past items already stored in a resumed session.
func (self *Recorder) Resume(storedItems int) {
	self.flushBoundary = storedItems
}

// Event appends one canonical event.
func (self *Recorder) Event(event agent.Event) error {
	return self.session.Event(event)
}

// StoreItems appends provider state beyond the last successful flush boundary.
func (self *Recorder) StoreItems(items []json.RawMessage) error {
	if len(items) < self.flushBoundary {
		return errors.New("the provider replaced append-only conversation state")
	}

	for _, item := range items[self.flushBoundary:] {
		if err := self.session.Item(item); err != nil {
			return fmt.Errorf("the conversation state could not be stored: %w", err)
		}
		self.flushBoundary++
	}

	return nil
}

// TakeWarnings returns and clears auxiliary recorder warnings.
func (self *Recorder) TakeWarnings() []error {
	return self.session.TakeWarnings()
}
