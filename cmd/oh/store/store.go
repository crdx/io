// Package store gives oh's metadata and picker views to the core session journal.
package store

import (
	"encoding/json"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/session"
)

// Meta is the conversation-specific configuration needed to resume a session.
type Meta struct {
	Model     string `json:"model"`
	Workspace string `json:"workspace"`
	Provider  string `json:"provider"`
	Effort    string `json:"effort,omitempty"`
	Context   string `json:"context,omitempty"`
}

// Writer appends oh records to the core journal.
type Writer struct {
	inner *session.Writer
}

// Create starts an oh session.
func Create(directory string, meta Meta) (*Writer, error) {
	jsonStr, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	inner, err := session.Create(directory, jsonStr)
	if err != nil {
		return nil, err
	}
	return &Writer{inner: inner}, nil
}

// Open continues an oh session.
func Open(directory, id string) (*Writer, error) {
	inner, err := session.Open(directory, id)
	if err != nil {
		return nil, err
	}
	return &Writer{inner: inner}, nil
}

// Event appends one conversation event.
func (w *Writer) Event(event agent.Event) error { return w.inner.Event(event) }

// Item appends one provider-state item.
func (w *Writer) Item(item json.RawMessage) error { return w.inner.Item(item) }

// ID is the session identifier.
func (w *Writer) ID() string { return w.inner.ID() }

// SetMeta replaces the meta before the first record is written.
func (w *Writer) SetMeta(meta Meta) error {
	jsonStr, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return w.inner.SetMeta(jsonStr)
}

// Stored reports whether anything was written.
func (w *Writer) Stored() bool { return w.inner.Stored() }

// Close closes a stored session.
func (w *Writer) Close() error { return w.inner.Close() }

// Session is an oh session read back.
type Session struct {
	ID      string
	Meta    Meta
	Started time.Time
	Touched time.Time
	Events  []agent.Event
	Items   []json.RawMessage
}

// Read loads one oh session.
func Read(directory, id string) (*Session, error) {
	storedSession, err := session.Read(directory, id)
	if err != nil {
		return nil, err
	}
	return decode(storedSession)
}

// List loads oh sessions, most recently touched first.
func List(directory string) ([]*Session, error) {
	storedSessions, err := session.List(directory)
	if err != nil {
		return nil, err
	}

	out := make([]*Session, 0, len(storedSessions))
	for _, item := range storedSessions {
		decodedSession, err := decode(item)
		if err == nil {
			out = append(out, decodedSession)
		}
	}
	return out, nil
}

func decode(storedSession *session.Session) (*Session, error) {
	var meta Meta
	if len(storedSession.Meta) > 0 {
		if err := json.Unmarshal(storedSession.Meta, &meta); err != nil {
			return nil, err
		}
	}

	return &Session{
		ID:      storedSession.ID,
		Meta:    meta,
		Started: storedSession.Started,
		Touched: storedSession.Touched,
		Events:  storedSession.Events,
		Items:   storedSession.Items,
	}, nil
}

// FirstPrompt is the first prompt in the session.
func (s *Session) FirstPrompt() string {
	for _, event := range s.Events {
		if event.Kind == agent.Prompt {
			return event.Text
		}
	}
	return ""
}

// Messages counts prompts and answers, excluding working events.
func (s *Session) Messages() int {
	count := 0
	for _, event := range s.Events {
		if event.Kind == agent.Prompt || event.Kind == agent.Text {
			count++
		}
	}
	return count
}
