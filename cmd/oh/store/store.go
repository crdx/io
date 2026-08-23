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

type canonicalWriter interface {
	Event(agent.Event) (time.Time, error)
	Item(json.RawMessage) error
	Name() string
	ID() string
	Started() time.Time
	Stored() bool
	EnsureStored() error
	SetMeta(json.RawMessage) error
	Close() error
}

// Writer coordinates the canonical journal and auxiliary bundle recorders.
type Writer struct {
	innerWriter canonicalWriter
	writerMutex sync.Mutex

	eventBuffer []agent.Event
	directory   string
	meta        Meta

	recorderMutex sync.Mutex

	transcriptLoggingEnabled bool
	transcriptRecorder       *transcript.Recorder

	wireRecordingEnabled bool
	wireRecorder         *wire.Recorder

	warnings []error
}

// Create starts an oh session.
func Create(directory string, meta Meta) (*Writer, error) {
	jsonStr, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	innerWriter, err := session.Create(directory, jsonStr)
	if err != nil {
		return nil, err
	}
	return &Writer{
		innerWriter:              innerWriter,
		directory:                directory,
		meta:                     meta,
		transcriptLoggingEnabled: true,
		wireRecordingEnabled:     true,
	}, nil
}

// Open continues an oh session, and reports session.ErrInUse when another writer holds it.
func Open(directory, name string) (*Writer, error) {
	storedSession, err := Read(directory, name)
	if err != nil {
		return nil, err
	}
	innerWriter, err := session.Open(directory, name)
	if err != nil {
		return nil, err
	}
	writer := &Writer{
		innerWriter:              innerWriter,
		directory:                directory,
		meta:                     storedSession.Meta,
		transcriptLoggingEnabled: true,
		wireRecordingEnabled:     true,
	}
	writer.startRecorders()
	return writer, nil
}

// Event appends one conversation event.
func (self *Writer) Event(event agent.Event) error {
	for _, releasedEvent := range self.release(event) {
		if err := self.appendEvent(releasedEvent); err != nil {
			return err
		}
	}

	return nil
}

// Item appends one provider-state item.
func (self *Writer) Item(item json.RawMessage) error {
	self.writerMutex.Lock()
	err := self.innerWriter.Item(item)
	self.writerMutex.Unlock()
	if err != nil {
		return err
	}
	self.startRecorders()
	return nil
}

// Name is what the session is called, and the name of its bundle directory.
func (self *Writer) Name() string { return self.innerWriter.Name() }

// ID is the session's time-ordered identifier, recorded for provenance and read by nothing.
func (self *Writer) ID() string { return self.innerWriter.ID() }

// SetMeta replaces the meta before the first record is written.
func (self *Writer) SetMeta(meta Meta) error {
	jsonStr, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()
	if err := self.innerWriter.SetMeta(jsonStr); err != nil {
		return err
	}
	self.meta = meta
	return nil
}

// Stored reports whether anything was written.
func (self *Writer) Stored() bool {
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()
	return self.innerWriter.Stored()
}

// Observer returns the session's logical HTTP exchange observer.
func (self *Writer) Observer() req.Observer { return writerObserver{writer: self} }

// TakeWarnings drains auxiliary recorder failures.
func (self *Writer) TakeWarnings() []error {
	self.recorderMutex.Lock()
	defer self.recorderMutex.Unlock()
	warnings := self.warnings
	self.warnings = nil
	return warnings
}

// Close closes a stored session.
func (self *Writer) Close() error {
	self.writerMutex.Lock()
	canonicalError := self.innerWriter.Close()
	self.writerMutex.Unlock()
	self.recorderMutex.Lock()
	transcriptRecorder := self.transcriptRecorder
	wireRecorder := self.wireRecorder
	self.transcriptRecorder = nil
	self.wireRecorder = nil
	self.recorderMutex.Unlock()
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

func (self *Writer) release(event agent.Event) []agent.Event {
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()

	if !self.innerWriter.Stored() && event.Kind != agent.UserMessageEvent {
		self.eventBuffer = append(self.eventBuffer, event)
		return nil
	}

	released := append(self.eventBuffer, event)
	self.eventBuffer = nil

	return released
}

func (self *Writer) appendEvent(event agent.Event) error {
	self.writerMutex.Lock()
	at, err := self.innerWriter.Event(event)
	self.writerMutex.Unlock()
	if err != nil {
		return err
	}
	self.startRecorders()

	self.recorderMutex.Lock()
	defer self.recorderMutex.Unlock()
	if self.transcriptRecorder != nil {
		if err := self.transcriptRecorder.Event(at, event); err != nil {
			_ = self.transcriptRecorder.Close()
			self.transcriptRecorder = nil
			self.transcriptLoggingEnabled = false
			self.warnings = append(self.warnings, fmt.Errorf("chat.md recording disabled: %w", err))
		}
	}
	return nil
}

func (self *Writer) startRecorders() {
	self.writerMutex.Lock()
	err := self.innerWriter.EnsureStored()
	started := self.innerWriter.Started()
	self.writerMutex.Unlock()
	if err != nil {
		return
	}

	self.recorderMutex.Lock()
	defer self.recorderMutex.Unlock()
	bundleDirectory := filepath.Join(self.directory, self.Name())
	if self.transcriptRecorder == nil && self.transcriptLoggingEnabled {
		recorder, err := transcript.Open(filepath.Join(bundleDirectory, transcriptName), transcript.Meta{
			Name:      self.Name(),
			Started:   started,
			Model:     self.meta.Model,
			Effort:    self.meta.Effort,
			Provider:  self.meta.Provider,
			Workspace: self.meta.WorkspaceDir,
		})
		if err != nil {
			self.transcriptLoggingEnabled = false
			self.warnings = append(self.warnings, fmt.Errorf("chat.md recording disabled: %w", err))
		} else {
			self.transcriptRecorder = recorder
		}
	}
	if self.wireRecorder == nil && self.wireRecordingEnabled {
		recorder, err := wire.Open(filepath.Join(bundleDirectory, wireName), wire.Meta{
			Name:      self.Name(),
			Started:   started,
			Model:     self.meta.Model,
			Effort:    self.meta.Effort,
			Provider:  self.meta.Provider,
			Workspace: self.meta.WorkspaceDir,
		}, self.queueWarning)
		if err != nil {
			self.wireRecordingEnabled = false
			self.warnings = append(self.warnings, fmt.Errorf("wire.http recording disabled: %w", err))
		} else {
			self.wireRecorder = recorder
		}
	}
}

func (self *Writer) queueWarning(err error) {
	self.recorderMutex.Lock()
	defer self.recorderMutex.Unlock()
	self.warnings = append(self.warnings, err)
}

type writerObserver struct{ writer *Writer }

func (self writerObserver) Start(request req.Request) req.ExchangeObserver {
	self.writer.startRecorders()
	self.writer.recorderMutex.Lock()
	recorder := self.writer.wireRecorder
	self.writer.recorderMutex.Unlock()
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
		if event.Kind == agent.UserMessageEvent {
			return event.Text
		}
	}
	return ""
}

// Messages counts what the user and the model said, excluding working events.
func (self *Session) Messages() int {
	count := 0
	for _, event := range self.Events {
		if event.Kind == agent.UserMessageEvent || event.Kind == agent.ModelMessageEvent {
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
