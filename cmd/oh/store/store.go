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

type Meta struct {
	Model        string `json:"model"`
	WorkspaceDir string `json:"workspaceDir"`
	Provider     string `json:"provider"`
	Effort       string `json:"effort,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
}

type listingData struct {
	WorkspaceDir string `json:"workspaceDir"`
}

func encodeMeta(meta Meta) (json.RawMessage, json.RawMessage, error) {
	journalMeta, err := json.Marshal(meta)
	if err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(listingData{WorkspaceDir: meta.WorkspaceDir})
	if err != nil {
		return nil, nil, err
	}
	return journalMeta, data, nil
}

const (
	transcriptName = "chat.md"
	wireName       = "wire.http"
)

type canonicalWriter interface {
	canonicalAppender
	canonicalSession
}

type canonicalAppender interface {
	Event(agent.Event) (time.Time, error)
	Item(json.RawMessage) error
	CompleteTurn() error
}

type canonicalSession interface {
	Name() string
	ID() string
	JournalMeta() json.RawMessage
	Started() time.Time
	IsPersisted() bool
	EnsurePersisted() error
	SetMeta(json.RawMessage, json.RawMessage) error
	Close() error
}

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

func Create(directory string, meta Meta) (*Writer, error) {
	journalMeta, data, err := encodeMeta(meta)
	if err != nil {
		return nil, err
	}
	innerWriter, err := session.Create(directory, journalMeta, data)
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

func Open(directory, name string) (*Writer, error) {
	innerWriter, err := session.Open(directory, name)
	if err != nil {
		return nil, err
	}

	var meta Meta
	if encodedMeta := innerWriter.JournalMeta(); len(encodedMeta) > 0 {
		if err := json.Unmarshal(encodedMeta, &meta); err != nil {
			_ = innerWriter.Close()
			return nil, err
		}
	}

	writer := &Writer{
		innerWriter:              innerWriter,
		directory:                directory,
		meta:                     meta,
		transcriptLoggingEnabled: true,
		wireRecordingEnabled:     true,
	}
	writer.startRecorders()
	return writer, nil
}

func (self *Writer) Event(event agent.Event) error {
	for _, releasedEvent := range self.release(event) {
		if err := self.appendEvent(releasedEvent); err != nil {
			return err
		}
	}

	return nil
}

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

func (self *Writer) CompleteTurn() error {
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()
	return self.innerWriter.CompleteTurn()
}

func (self *Writer) Name() string { return self.innerWriter.Name() }

func (self *Writer) ID() string { return self.innerWriter.ID() }

func (self *Writer) SetMeta(meta Meta) error {
	journalMeta, data, err := encodeMeta(meta)
	if err != nil {
		return err
	}
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()
	if err := self.innerWriter.SetMeta(journalMeta, data); err != nil {
		return err
	}
	self.meta = meta
	return nil
}

func (self *Writer) IsPersisted() bool {
	self.writerMutex.Lock()
	defer self.writerMutex.Unlock()
	return self.innerWriter.IsPersisted()
}

func (self *Writer) Observer() req.Observer { return writerObserver{writer: self} }

func (self *Writer) TakeWarnings() []error {
	self.recorderMutex.Lock()
	defer self.recorderMutex.Unlock()
	warnings := self.warnings
	self.warnings = nil
	return warnings
}

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

	if !self.innerWriter.IsPersisted() && event.Kind != agent.UserMessageEvent {
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
	err := self.innerWriter.EnsurePersisted()
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

type Session struct {
	Name            string
	ID              string
	Meta            Meta
	Started         time.Time
	Touched         time.Time
	Events          []agent.Event
	Items           []json.RawMessage
	TurnCompletions int
}

func Read(directory, name string) (*Session, error) {
	storedSession, err := session.Read(directory, name)
	if err != nil {
		return nil, err
	}
	return decode(storedSession)
}

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
		Name:            storedSession.Name,
		ID:              storedSession.ID,
		Meta:            meta,
		Started:         storedSession.Started,
		Touched:         storedSession.Touched,
		Events:          storedSession.Events,
		Items:           storedSession.Items,
		TurnCompletions: storedSession.TurnCompletions,
	}, nil
}

func (self *Session) FirstMessage() string {
	for _, event := range self.Events {
		if event.Kind == agent.UserMessageEvent {
			return event.Text
		}
	}
	return ""
}

func (self *Session) Messages() int {
	count := 0
	for _, event := range self.Events {
		if event.Kind == agent.UserMessageEvent || event.Kind == agent.ModelMessageEvent {
			count++
		}
	}
	return count
}

func (self *Session) CanResume() bool {
	userTurns := 0
	for _, event := range self.Events {
		if event.Kind == agent.UserMessageEvent {
			userTurns++
		}
	}
	return userTurns == self.TurnCompletions
}

func RebuildMeta(directory, name string) error {
	storedSession, err := Read(directory, name)
	if err != nil {
		return err
	}
	_, data, err := encodeMeta(storedSession.Meta)
	if err != nil {
		return err
	}
	return session.RebuildMeta(directory, name, data)
}

func StaleMeta(directory string) ([]string, error) {
	entries, err := session.Entries(directory)
	if err != nil {
		return nil, err
	}

	var stale []string
	for _, entry := range entries {
		if _, err := session.ReadMeta(directory, entry.Name); err != nil {
			stale = append(stale, entry.Name)
		}
	}

	return stale, nil
}

func RebuildStaleMeta(directory string) (int, error) {
	stale, err := StaleMeta(directory)
	if err != nil {
		return 0, err
	}

	for at, name := range stale {
		if err := RebuildMeta(directory, name); err != nil {
			return at, fmt.Errorf("could not rebuild the listing metadata of %s: %w", name, err)
		}
	}

	return len(stale), nil
}

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
