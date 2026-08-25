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
	"syscall"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/format"
)

// JournalFormat is the shape of the journal holding a stored conversation. The journal is canon, so
// one in an earlier format is migrated to this one rather than written again, and one in a later
// format is refused rather than read wrongly.
const JournalFormat = 6

// MetaFormat is the shape of the listing metadata stored beside a journal. The journal is canon and
// the metadata is derived from it, so metadata in another format is written again, never migrated.
const MetaFormat = 1

// ErrMetaOutOfDate is listing metadata this build does not write. Whoever owns the data it carries
// is free to build it again from the journal.
var ErrMetaOutOfDate = errors.New("listing metadata is in another format")

type Kind string

const (
	Head           Kind = "head"
	Event          Kind = "event"
	Item           Kind = "item"
	TurnCompletion Kind = "turn_completion"
)

type Line struct {
	Kind Kind      `json:"kind"`
	Time time.Time `json:"time"`

	Version int `json:"version,omitempty"` // the format of the journal, on the head record alone

	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Meta    json.RawMessage `json:"meta,omitempty"`
	Event   *agent.Event    `json:"event,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Writer struct {
	file        *os.File
	directory   string
	id          string
	name        string
	started     time.Time
	journalMeta json.RawMessage
	listingData json.RawMessage
	listingMeta Meta
}

func Create(directory string, journalMeta json.RawMessage, listingData json.RawMessage) (*Writer, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}

	name, err := newName(directory)
	if err != nil {
		return nil, err
	}

	return &Writer{
		directory:   directory,
		id:          newID(),
		name:        name,
		journalMeta: slices.Clone(journalMeta),
		listingData: slices.Clone(listingData),
	}, nil
}

var ErrInUse = errors.New("the session is already open elsewhere")

func Open(directory string, name string) (*Writer, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}

	head, err := readHead(directory, name)
	if err != nil {
		return nil, err
	}

	listingMeta, err := ReadMeta(directory, name)
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(journalPath(directory, name), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}

	if err := lockJournal(file); err != nil {
		_ = file.Close()
		return nil, err
	}

	return &Writer{
		file:        file,
		directory:   directory,
		id:          head.ID,
		name:        name,
		started:     head.Time,
		journalMeta: slices.Clone(head.Meta),
		listingMeta: *listingMeta,
	}, nil
}

func lockJournal(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrInUse
	}
	return err
}

func (w *Writer) SetMeta(journalMeta json.RawMessage, listingData json.RawMessage) error {
	if w.file != nil {
		return errors.New("cannot change meta after the session has been stored")
	}

	w.journalMeta = slices.Clone(journalMeta)
	w.listingData = slices.Clone(listingData)
	return nil
}

func (w *Writer) Event(event agent.Event) (time.Time, error) {
	writtenAt, err := w.write(Line{Kind: Event, Event: &event})
	if err != nil {
		return writtenAt, err
	}

	w.listingMeta.takeEvent(event, writtenAt)
	return writtenAt, writeMeta(w.directory, w.listingMeta)
}

func (w *Writer) Item(payload json.RawMessage) error {
	writtenAt, err := w.write(Line{Kind: Item, Payload: payload})
	if err != nil {
		return err
	}

	w.listingMeta.Touched = writtenAt
	return writeMeta(w.directory, w.listingMeta)
}

func (w *Writer) CompleteTurn() error {
	writtenAt, err := w.write(Line{Kind: TurnCompletion})
	if err != nil {
		return err
	}

	w.listingMeta.Touched = writtenAt
	return writeMeta(w.directory, w.listingMeta)
}

func (w *Writer) Name() string                 { return w.name }
func (w *Writer) ID() string                   { return w.id }
func (w *Writer) JournalMeta() json.RawMessage { return slices.Clone(w.journalMeta) }
func (w *Writer) Started() time.Time           { return w.started }
func (w *Writer) Stored() bool                 { return w.file != nil }
func (w *Writer) EnsureStored() error          { return w.ensureOpen() }

func (w *Writer) Close() error {
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *Writer) write(line Line) (time.Time, error) {
	if err := w.ensureOpen(); err != nil {
		return time.Time{}, err
	}
	return w.record(line)
}

func (w *Writer) ensureOpen() error {
	if w.file != nil {
		return nil
	}

	directory := bundlePath(w.directory, w.name)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return err
	}

	file, err := os.OpenFile(journalPath(w.directory, w.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
	if err != nil {
		_ = os.Remove(directory)
		return err
	}

	if err := lockJournal(file); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		_ = os.Remove(directory)
		return err
	}
	w.file = file

	started, err := w.record(Line{Kind: Head, Version: JournalFormat, ID: w.id, Name: w.name, Meta: w.journalMeta})
	if err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		_ = os.Remove(directory)
		w.file = nil
		return err
	}
	w.started = started
	w.listingMeta = Meta{
		Name:    w.name,
		Data:    slices.Clone(w.listingData),
		Started: started,
		Touched: started,
	}
	if err := writeMeta(w.directory, w.listingMeta); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		_ = os.Remove(directory)
		w.file = nil
		return err
	}

	return nil
}

func (w *Writer) record(line Line) (time.Time, error) {
	line.Time = time.Now()
	encodedLine, err := json.Marshal(line)
	if err != nil {
		return line.Time, err
	}

	record := append(encodedLine, '\n')
	n, err := w.file.Write(record)
	if err == nil && n != len(record) {
		return line.Time, io.ErrShortWrite
	}
	return line.Time, err
}

type Session struct {
	Name            string
	ID              string
	Meta            json.RawMessage
	Started         time.Time
	Touched         time.Time
	Events          []agent.Event
	Items           []json.RawMessage
	TurnCompletions int
}

// Read loads one stored session.
func Read(directory string, name string) (*Session, error) {
	storedSession := &Session{Name: name}
	if err := Records(directory, name, func(line Line) error {
		storedSession.take(line)
		return nil
	}); err != nil {
		return nil, err
	}
	return storedSession, nil
}

// Records hands every record of a stored session to visit, in the order they were written. Each one
// carries the time it was written, which Read has no room for.
func Records(directory string, name string, visit func(Line) error) error {
	if err := validateName(name); err != nil {
		return err
	}

	file, err := os.Open(journalPath(directory, name))
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

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
				return errors.New("session does not start with a head")
			}
			if err := format.Check(formatOf(line), JournalFormat); err != nil {
				return fmt.Errorf("session %s: journal %w", name, err)
			}
			sawHead = true
		} else if line.Kind == Head {
			return errors.New("session contains more than one head")
		}

		if err := visit(line); err != nil {
			return err
		}
	}

	if err := lines.Err(); err != nil {
		return err
	}
	if !sawHead {
		return errors.New("session has no complete head")
	}
	return nil
}

func (s *Session) take(line Line) {
	if s.Started.IsZero() {
		s.Started = line.Time
	}
	s.Touched = line.Time

	switch line.Kind {
	case Head:
		s.ID = line.ID
		s.Meta = line.Meta
	case Event:
		if line.Event != nil {
			s.Events = append(s.Events, *line.Event)
		}
	case Item:
		s.Items = append(s.Items, line.Payload)
	case TurnCompletion:
		s.TurnCompletions++
	}
}

// Meta is the compact part of a stored session needed to list conversations.
type Meta struct {
	Version  int             `json:"version"`
	Name     string          `json:"name"`
	Data     json.RawMessage `json:"data,omitempty"`
	Started  time.Time       `json:"started"`
	Touched  time.Time       `json:"touched"`
	Title    string          `json:"title,omitempty"`
	Messages int             `json:"messages"`
}

func (s *Meta) takeEvent(event agent.Event, writtenAt time.Time) {
	s.Touched = writtenAt
	if event.Kind != agent.UserMessageEvent && event.Kind != agent.ModelMessageEvent {
		return
	}

	s.Messages++
	if s.Title == "" && event.Kind == agent.UserMessageEvent {
		s.Title = event.Text
	}
}

// ReadMeta loads the compact listing data for one stored session.
func ReadMeta(directory string, name string) (*Meta, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}

	encoded, err := os.ReadFile(metaPath(directory, name))
	if err != nil {
		return nil, err
	}

	storedFormat, err := format.ReadJSON(encoded)
	if err != nil {
		return nil, err
	}
	if storedFormat != MetaFormat {
		return nil, fmt.Errorf(
			"%w (format %d, not %d)", ErrMetaOutOfDate, storedFormat, MetaFormat,
		)
	}

	var meta Meta
	if err := json.Unmarshal(encoded, &meta); err != nil {
		return nil, err
	}
	if meta.Name != name {
		return nil, errors.New("session metadata names another session")
	}
	return &meta, nil
}

// ListMeta loads compact listing data for every stored session, most recently touched first.
func ListMeta(directory string) ([]*Meta, error) {
	names, err := storedNames(directory)
	if err != nil {
		return nil, err
	}

	metadata := make([]*Meta, 0, len(names))
	for _, name := range names {
		meta, err := ReadMeta(directory, name)
		if err != nil {
			return nil, fmt.Errorf("could not read session %s metadata: %w", name, err)
		}
		metadata = append(metadata, meta)
	}

	slices.SortFunc(metadata, func(first, second *Meta) int {
		if order := second.Touched.Compare(first.Touched); order != 0 {
			return order
		}
		return strings.Compare(second.Name, first.Name)
	})
	return metadata, nil
}

// RebuildMeta derives listing metadata from a journal and caller-owned data.
func RebuildMeta(directory string, name string, listingData json.RawMessage) error {
	storedSession, err := Read(directory, name)
	if err != nil {
		return err
	}

	meta := Meta{
		Name:    storedSession.Name,
		Data:    slices.Clone(listingData),
		Started: storedSession.Started,
		Touched: storedSession.Touched,
	}
	for _, event := range storedSession.Events {
		meta.takeEvent(event, storedSession.Touched)
	}

	return writeMeta(directory, meta)
}

func writeMeta(directory string, meta Meta) error {
	meta.Version = MetaFormat

	encoded, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	bundle := bundlePath(directory, meta.Name)
	file, err := os.CreateTemp(bundle, "meta-*.json")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer func() {
		_ = file.Close()
		_ = os.Remove(temporaryPath)
	}()

	if _, err := file.Write(encoded); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return err
	}
	return os.Rename(temporaryPath, metaPath(directory, meta.Name))
}

// Entry identifies one stored session. Building it costs the head of the journal rather than the
// whole conversation, so a directory of sessions can be surveyed without loading any of them.
type Entry struct {
	Name    string
	ID      string
	Started time.Time
	Format  int
}

// Entries identifies every stored session, oldest first.
func Entries(directory string) ([]Entry, error) {
	names, err := storedNames(directory)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(names))
	for _, name := range names {
		head, err := readHeadSummary(directory, name)
		if err != nil {
			continue
		}
		entries = append(entries, Entry{
			Name:    name,
			ID:      head.ID,
			Started: head.Time,
			Format:  formatOf(head),
		})
	}

	slices.SortFunc(entries, func(first, second Entry) int {
		return first.Started.Compare(second.Started)
	})
	return entries, nil
}

func formatOf(head Line) int {
	if head.Version == 0 {
		return 1
	}

	return head.Version
}

// Outdated names the stored sessions written in an older journal format than this build writes.
func Outdated(directory string) ([]string, error) {
	return namesInFormat(directory, func(stored int) bool { return stored < JournalFormat })
}

// Ahead names the stored sessions written in a newer journal format than this build reads. No
// migration brings one of those back: only a newer build can read it.
func Ahead(directory string) ([]string, error) {
	return namesInFormat(directory, func(stored int) bool { return stored > JournalFormat })
}

func namesInFormat(directory string, wanted func(stored int) bool) ([]string, error) {
	entries, err := Entries(directory)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if wanted(entry.Format) {
			names = append(names, entry.Name)
		}
	}

	return names, nil
}

func storedNames(directory string) ([]string, error) {
	found, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, candidate := range found {
		if candidate.IsDir() && validateName(candidate.Name()) == nil {
			names = append(names, candidate.Name())
		}
	}
	return names, nil
}

func readHeadSummary(directory, name string) (Line, error) {
	file, err := os.Open(journalPath(directory, name))
	if err != nil {
		return Line{}, err
	}
	defer func() { _ = file.Close() }()

	decoder := json.NewDecoder(bufio.NewReaderSize(file, 8192))
	if _, err := decoder.Token(); err != nil {
		return Line{}, err
	}

	var head Line
	for decoder.More() {
		fieldToken, err := decoder.Token()
		if err != nil {
			return Line{}, err
		}
		field, _ := fieldToken.(string)

		var target any
		switch field {
		case "kind":
			target = &head.Kind
		case "time":
			target = &head.Time
		case "version":
			target = &head.Version
		case "id":
			target = &head.ID
		case "name":
			target = &head.Name
		default:
			return head, nil
		}
		if err := decoder.Decode(target); err != nil {
			return Line{}, err
		}
	}

	return head, nil
}

func readHead(directory, name string) (Line, error) {
	file, err := os.Open(journalPath(directory, name))
	if err != nil {
		return Line{}, err
	}
	defer func() { _ = file.Close() }()

	lines := bufio.NewScanner(file)
	lines.Buffer(nil, maxLine)
	if !lines.Scan() {
		if err := lines.Err(); err != nil {
			return Line{}, err
		}
		return Line{}, errors.New("session has no complete head")
	}

	var line Line
	if err := json.Unmarshal(lines.Bytes(), &line); err != nil {
		return Line{}, err
	}
	if line.Kind != Head {
		return Line{}, errors.New("session does not start with a head")
	}
	return line, nil
}

// List loads stored sessions, most recently touched first.
func List(directory string) ([]*Session, error) {
	entries, err := Entries(directory)
	if err != nil {
		return nil, err
	}

	var sessions []*Session
	for _, entry := range entries {
		if storedSession, err := Read(directory, entry.Name); err == nil {
			sessions = append(sessions, storedSession)
		}
	}

	slices.SortFunc(sessions, func(first, second *Session) int {
		if order := second.Touched.Compare(first.Touched); order != 0 {
			return order
		}
		return strings.Compare(second.Name, first.Name)
	})
	return sessions, nil
}

const (
	journalName = "session.jsonl"
	metaName    = "meta.json"
	maxLine     = 16 << 20
)

func bundlePath(directory, name string) string { return filepath.Join(directory, name) }

func journalPath(directory, name string) string {
	return filepath.Join(bundlePath(directory, name), journalName)
}

func metaPath(directory, name string) string {
	return filepath.Join(bundlePath(directory, name), metaName)
}

func validateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid session name %q", name)
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
	for i, value := range id {
		number[i] = uint32(value)
	}

	for position := idLength - 1; position >= 0; position-- {
		carry := uint32(0)
		for i, value := range number {
			carry = carry<<8 | value
			number[i] = carry / base
			carry %= base
		}
		out[position] = digits[carry]
	}
	return string(out[:])
}
