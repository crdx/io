// Package store adds oh's metadata and auxiliary recorders to the core session journal.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/session"

	"crdx.org/io/cmd/oh/store/transcript"
	"crdx.org/io/cmd/oh/store/wire"
)

// Meta is the conversation-specific configuration needed to resume a session.
type Meta struct {
	Model        string `json:"model"`
	WorkspaceDir string `json:"workspaceDir"`
	Provider     string `json:"provider"`
	Effort       string `json:"effort,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
}

const (
	transcriptName = "chat.md"
	wireName       = "wire.http"
)

// Writer coordinates the canonical journal and auxiliary bundle recorders.
type Writer struct {
	inner     *session.Writer
	directory string
	meta      Meta

	canonicalMutex    sync.Mutex
	mutex             sync.Mutex
	transcript        *transcript.Recorder
	wire              *wire.Recorder
	transcriptEnabled bool
	wireEnabled       bool
	warnings          []error
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
	return &Writer{
		inner:             inner,
		directory:         directory,
		meta:              meta,
		transcriptEnabled: true,
		wireEnabled:       true,
	}, nil
}

// Open continues an oh session, and reports session.ErrInUse when another writer holds it.
func Open(directory, name string) (*Writer, error) {
	storedSession, err := Read(directory, name)
	if err != nil {
		return nil, err
	}
	inner, err := session.Open(directory, name)
	if err != nil {
		return nil, err
	}
	writer := &Writer{
		inner:             inner,
		directory:         directory,
		meta:              storedSession.Meta,
		transcriptEnabled: true,
		wireEnabled:       true,
	}
	writer.ensureAuxiliaryRecorders()
	return writer, nil
}

// Event appends one conversation event.
func (self *Writer) Event(event agent.Event) error {
	self.canonicalMutex.Lock()
	at, err := self.inner.Event(event)
	self.canonicalMutex.Unlock()
	if err != nil {
		return err
	}
	self.ensureAuxiliaryRecorders()

	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.transcript != nil {
		if err := self.transcript.Event(at, event); err != nil {
			_ = self.transcript.Close()
			self.transcript = nil
			self.transcriptEnabled = false
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

// Name is what the session is called, and the name of its bundle directory.
func (self *Writer) Name() string { return self.inner.Name() }

// ID is the session's time-ordered identifier, recorded for provenance and read by nothing.
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
	transcriptRecorder := self.transcript
	wireRecorder := self.wire
	self.transcript = nil
	self.wire = nil
	self.mutex.Unlock()
	if transcriptRecorder != nil {
		if err := transcriptRecorder.Close(); err != nil {
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
	bundleDirectory := filepath.Join(self.directory, self.Name())
	if self.transcript == nil && self.transcriptEnabled {
		recorder, err := transcript.Open(filepath.Join(bundleDirectory, transcriptName), transcript.Meta{
			Name:      self.Name(),
			Started:   self.inner.Started(),
			Model:     self.meta.Model,
			Effort:    self.meta.Effort,
			Provider:  self.meta.Provider,
			Workspace: self.meta.WorkspaceDir,
		})
		if err != nil {
			self.transcriptEnabled = false
			self.warnings = append(self.warnings, fmt.Errorf("chat.md recording disabled: %w", err))
		} else {
			self.transcript = recorder
		}
	}
	if self.wire == nil && self.wireEnabled {
		recorder, err := wire.Open(filepath.Join(bundleDirectory, wireName), wire.Meta{
			Name:      self.Name(),
			Started:   self.inner.Started(),
			Model:     self.meta.Model,
			Effort:    self.meta.Effort,
			Provider:  self.meta.Provider,
			Workspace: self.meta.WorkspaceDir,
		}, self.queueWarning)
		if err != nil {
			self.wireEnabled = false
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
	Name    string
	ID      string
	Meta    Meta
	Started time.Time
	Touched time.Time
	Events  []agent.Event
	Items   []json.RawMessage
}

// Read loads one oh session.
func Read(directory, name string) (*Session, error) {
	storedSession, err := session.Read(directory, name)
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
		Name:    storedSession.Name,
		ID:      storedSession.ID,
		Meta:    meta,
		Started: storedSession.Started,
		Touched: storedSession.Touched,
		Events:  storedSession.Events,
		Items:   storedSession.Items,
	}, nil
}

// FirstMessage is the first message the user sent in the session.
func (self *Session) FirstMessage() string {
	for _, event := range self.Events {
		if event.Kind == agent.UserMessage {
			return event.Text
		}
	}
	return ""
}

// Messages counts what the user and the model said, excluding working events.
func (self *Session) Messages() int {
	count := 0
	for _, event := range self.Events {
		if event.Kind == agent.UserMessage || event.Kind == agent.ModelMessage {
			count++
		}
	}
	return count
}

// Rebuild writes a session's transcript again from its journal, replacing whatever was there.
func Rebuild(directory, name string) error {
	storedSession, err := Read(directory, name)
	if err != nil {
		return err
	}

	path := filepath.Join(directory, name, transcriptName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	recorder, err := transcript.Open(path, transcript.Meta{
		Name:      name,
		Started:   storedSession.Started,
		Model:     storedSession.Meta.Model,
		Effort:    storedSession.Meta.Effort,
		Provider:  storedSession.Meta.Provider,
		Workspace: storedSession.Meta.WorkspaceDir,
	})
	if err != nil {
		return err
	}

	writeError := session.Records(directory, name, func(line session.Line) error {
		if line.Kind != session.Event || line.Event == nil {
			return nil
		}
		return recorder.Event(line.Time, *line.Event)
	})

	if closeError := recorder.Close(); writeError == nil {
		writeError = closeError
	}
	return writeError
}
