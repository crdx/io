// Package session stores an agent conversation as an append-only JSON-lines journal.
//
// Events are the portable transcript and durable state transitions. Items are opaque, append-only
// provider state: a provider hands them out and takes the same bytes back on resume. Meta belongs
// to the caller.
package session

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"crdx.org/io/agent"
)

// Kind is what one journal line holds.
type Kind string

// The kinds of line in a journal.
const (
	Head  Kind = "head"
	Event Kind = "event"
	Item  Kind = "item"
)

// Line is one journal record. Head records include the original session ID.
type Line struct {
	Kind Kind      `json:"kind"`
	Time time.Time `json:"time"`

	ID      string          `json:"id,omitempty"`
	Meta    json.RawMessage `json:"meta,omitempty"`
	Event   *agent.Event    `json:"event,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Writer appends records to a session. The file is made by the first record, so an unused session
// leaves nothing behind.
type Writer struct {
	file      *os.File
	directory string
	id        string
	meta      json.RawMessage
}

// Create starts a session in directory with caller-owned meta.
func Create(directory string, meta json.RawMessage) (*Writer, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}

	return &Writer{directory: directory, id: newID(), meta: slices.Clone(meta)}, nil
}

// Open continues an existing session.
func Open(directory string, id string) (*Writer, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(journalPath(directory, id), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}

	return &Writer{file: file, id: id}, nil
}

// SetMeta replaces the caller-owned meta before the first record is written.
func (w *Writer) SetMeta(meta json.RawMessage) error {
	if w.file != nil {
		return errors.New("cannot change meta after the session has been stored")
	}

	w.meta = slices.Clone(meta)
	return nil
}

// Event appends one portable conversation event.
func (w *Writer) Event(event agent.Event) error {
	return w.write(Line{Kind: Event, Event: &event})
}

// Item appends one opaque provider-state item.
func (w *Writer) Item(payload json.RawMessage) error {
	return w.write(Line{Kind: Item, Payload: payload})
}

// ID is the session's time-ordered identifier.
func (w *Writer) ID() string { return w.id }

// Stored reports whether the lazy writer has made a file.
func (w *Writer) Stored() bool { return w.file != nil }

// EnsureStored creates the journal and writes its head without adding an event or item.
func (w *Writer) EnsureStored() error { return w.ensureOpen() }

// Close closes a session that has been stored.
func (w *Writer) Close() error {
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *Writer) write(line Line) error {
	if err := w.ensureOpen(); err != nil {
		return err
	}
	return w.record(line)
}

func (w *Writer) ensureOpen() error {
	if w.file != nil {
		return nil
	}

	directory := bundlePath(w.directory, w.id)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return err
	}

	file, err := os.OpenFile(journalPath(w.directory, w.id), os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
	if err != nil {
		_ = os.Remove(directory)
		return err
	}
	w.file = file

	if err := w.record(Line{Kind: Head, ID: w.id, Meta: w.meta}); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		_ = os.Remove(directory)
		w.file = nil
		return err
	}

	return nil
}

func (w *Writer) record(line Line) error {
	line.Time = time.Now()
	encodedLine, err := json.Marshal(line)
	if err != nil {
		return err
	}

	record := append(encodedLine, '\n')
	n, err := w.file.Write(record)
	if err == nil && n != len(record) {
		return io.ErrShortWrite
	}
	return err
}

// Session is a stored conversation.
type Session struct {
	ID      string
	Meta    json.RawMessage
	Started time.Time
	Touched time.Time
	Events  []agent.Event
	Items   []json.RawMessage
}

// Read loads one stored session.
func Read(directory string, id string) (*Session, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}

	file, err := os.Open(journalPath(directory, id))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	storedSession := &Session{ID: id}
	lines := bufio.NewScanner(file)
	lines.Buffer(nil, maxLine)
	sawHead := false

	for lines.Scan() {
		var line Line
		if json.Unmarshal(lines.Bytes(), &line) != nil {
			break
		}

		if !sawHead {
			if line.Kind != Head {
				return nil, errors.New("session does not start with a head")
			}
			sawHead = true
		} else if line.Kind == Head {
			return nil, errors.New("session contains more than one head")
		}

		storedSession.take(line)
	}

	if err := lines.Err(); err != nil {
		return nil, err
	}
	if !sawHead {
		return nil, errors.New("session has no complete head")
	}
	return storedSession, nil
}

func (s *Session) take(line Line) {
	if s.Started.IsZero() {
		s.Started = line.Time
	}
	s.Touched = line.Time

	switch line.Kind {
	case Head:
		s.Meta = slices.Clone(line.Meta)
	case Event:
		if line.Event != nil {
			s.Events = append(s.Events, *line.Event)
		}
	case Item:
		s.Items = append(s.Items, slices.Clone(line.Payload))
	}
}

// List loads stored sessions, most recently touched first.
func List(directory string) ([]*Session, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []*Session
	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() || validateID(id) != nil {
			continue
		}
		if storedSession, err := Read(directory, id); err == nil {
			sessions = append(sessions, storedSession)
		}
	}

	slices.SortFunc(sessions, func(first, second *Session) int {
		if order := second.Touched.Compare(first.Touched); order != 0 {
			return order
		}
		return strings.Compare(second.ID, first.ID)
	})
	return sessions, nil
}

const (
	journalName = "session.jsonl"
	maxLine     = 16 << 20
)

func bundlePath(directory, id string) string { return filepath.Join(directory, id) }

func journalPath(directory, id string) string {
	return filepath.Join(bundlePath(directory, id), journalName)
}

func validateID(id string) error {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id {
		return fmt.Errorf("invalid session ID %q", id)
	}
	return nil
}

func newID() string {
	var id [16]byte
	var stamp [8]byte
	_, _ = rand.Read(id[:])
	binary.BigEndian.PutUint64(stamp[:], uint64(time.Now().UnixMilli()))
	copy(id[:6], stamp[2:])
	id[6] = 0x70 | id[6]&0x0f
	id[8] = 0x80 | id[8]&0x3f
	return encode(id)
}

const (
	digits   = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	base     = uint32(len(digits))
	idLength = 22
)

func encode(id [16]byte) string {
	out := [idLength]byte{}
	number := [16]uint32{}
	for index, value := range id {
		number[index] = uint32(value)
	}

	for position := idLength - 1; position >= 0; position-- {
		carry := uint32(0)
		for index, value := range number {
			carry = carry<<8 | value
			number[index] = carry / base
			carry %= base
		}
		out[position] = digits[carry]
	}
	return string(out[:])
}
