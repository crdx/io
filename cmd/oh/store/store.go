// Package store gives oh's metadata and picker views to the core session journal.
package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/session"

	"crdx.org/io/cmd/oh/store/chat"
	"crdx.org/io/cmd/oh/store/wire"
)

// Meta is the conversation-specific configuration needed to resume a session.
type Meta struct {
	Model        string `json:"model"`
	WorkspaceDir string `json:"workspaceDir"`
	Provider     string `json:"provider"`
	Effort       string `json:"effort,omitempty"`
	Context      string `json:"context,omitempty"`
}

const (
	chatName = "chat.md"
	wireName = "wire.http"
)

// Writer coordinates the canonical journal and auxiliary bundle recorders.
type Writer struct {
	inner     *session.Writer
	directory string
	meta      Meta
	started   time.Time

	canonicalMutex sync.Mutex
	mutex          sync.Mutex
	chat           *chat.Recorder
	wire           *wire.Recorder
	chatDisabled   bool
	wireDisabled   bool
	warnings       []error
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
	return &Writer{inner: inner, directory: directory, meta: meta, started: time.Now()}, nil
}

// Open continues an oh session.
func Open(directory, id string) (*Writer, error) {
	storedSession, err := Read(directory, id)
	if err != nil {
		return nil, err
	}
	inner, err := session.Open(directory, id)
	if err != nil {
		return nil, err
	}
	writer := &Writer{
		inner: inner, directory: directory, meta: storedSession.Meta, started: storedSession.Started,
	}
	writer.ensureAuxiliaryRecorders()
	return writer, nil
}

// Event appends one conversation event.
func (self *Writer) Event(event agent.Event) error {
	self.canonicalMutex.Lock()
	err := self.inner.Event(event)
	self.canonicalMutex.Unlock()
	if err != nil {
		return err
	}
	self.ensureAuxiliaryRecorders()

	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.chat != nil {
		if err := self.chat.Event(time.Now(), event); err != nil {
			_ = self.chat.Close()
			self.chat = nil
			self.chatDisabled = true
			self.warnings = append(self.warnings, fmt.Errorf("chat.md recording disabled: %w", err))
		}
	}
	return nil
}

// Item appends one provider-state item.
func (self *Writer) Item(item json.RawMessage) error {
	self.canonicalMutex.Lock()
	err := self.inner.Item(item)
	self.canonicalMutex.Unlock()
	if err != nil {
		return err
	}
	self.ensureAuxiliaryRecorders()
	return nil
}

// ID is the session identifier.
func (self *Writer) ID() string { return self.inner.ID() }

// SetMeta replaces the meta before the first record is written.
func (self *Writer) SetMeta(meta Meta) error {
	jsonStr, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	self.canonicalMutex.Lock()
	defer self.canonicalMutex.Unlock()
	if err := self.inner.SetMeta(jsonStr); err != nil {
		return err
	}
	self.meta = meta
	return nil
}

// Stored reports whether anything was written.
func (self *Writer) Stored() bool {
	self.canonicalMutex.Lock()
	defer self.canonicalMutex.Unlock()
	return self.inner.Stored()
}

// Observer returns the session's logical HTTP exchange observer.
func (self *Writer) Observer() req.Observer { return writerObserver{writer: self} }

// TakeWarnings drains auxiliary recorder failures.
func (self *Writer) TakeWarnings() []error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	warnings := self.warnings
	self.warnings = nil
	return warnings
}

// Close closes a stored session.
func (self *Writer) Close() error {
	self.canonicalMutex.Lock()
	canonicalError := self.inner.Close()
	self.canonicalMutex.Unlock()
	self.mutex.Lock()
	chatRecorder := self.chat
	wireRecorder := self.wire
	self.chat = nil
	self.wire = nil
	self.mutex.Unlock()
	if chatRecorder != nil {
		if err := chatRecorder.Close(); err != nil {
			self.queueWarning(fmt.Errorf("chat.md recording disabled: %w", err))
		}
	}
	if wireRecorder != nil {
		_ = wireRecorder.Close()
	}
	return canonicalError
}

func (self *Writer) ensureAuxiliaryRecorders() {
	self.canonicalMutex.Lock()
	err := self.inner.EnsureStored()
	self.canonicalMutex.Unlock()
	if err != nil {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()
	bundleDirectory := filepath.Join(self.directory, self.ID())
	if self.chat == nil && !self.chatDisabled {
		recorder, err := chat.Open(filepath.Join(bundleDirectory, chatName), chat.Meta{
			ID: self.ID(), Started: self.started, Model: self.meta.Model, Effort: self.meta.Effort,
			Provider: self.meta.Provider, Workspace: self.meta.WorkspaceDir,
		})
		if err != nil {
			self.chatDisabled = true
			self.warnings = append(self.warnings, fmt.Errorf("chat.md recording disabled: %w", err))
		} else {
			self.chat = recorder
		}
	}
	if self.wire == nil && !self.wireDisabled {
		recorder, err := wire.Open(filepath.Join(bundleDirectory, wireName), wire.Meta{
			ID: self.ID(), Started: self.started, Model: self.meta.Model, Effort: self.meta.Effort,
			Provider: self.meta.Provider, Workspace: self.meta.WorkspaceDir,
		}, self.queueWarning)
		if err != nil {
			self.wireDisabled = true
			self.warnings = append(self.warnings, fmt.Errorf("wire.http recording disabled: %w", err))
		} else {
			self.wire = recorder
		}
	}
}

func (self *Writer) queueWarning(err error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.warnings = append(self.warnings, err)
}

type writerObserver struct{ writer *Writer }

func (self writerObserver) Start(request req.Request) req.ExchangeObserver {
	self.writer.ensureAuxiliaryRecorders()
	self.writer.mutex.Lock()
	recorder := self.writer.wire
	self.writer.mutex.Unlock()
	if recorder == nil {
		return nil
	}
	return recorder.Start(request)
}

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
