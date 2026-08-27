package session

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/format"
)

const JournalFormat = 10

const MetaFormat = 1

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

	Version int `json:"version,omitempty"`

	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Meta    json.RawMessage `json:"meta,omitempty"`
	Event   *agent.Event    `json:"event,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Writer struct {
	file        *os.File
	lock        *Lock
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

var ErrNotFound = errors.New("no session named")

type Lock struct {
	sessionDir  *os.File
	journalFile *os.File
}

func openSessionDir(directory string, name string) (*os.File, error) {
	sessionDir, err := os.Open(sessionDir(directory, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w %q", ErrNotFound, name)
	}
	return sessionDir, err
}

func openJournal(directory string, name string, flag int) (*os.File, error) {
	file, err := os.OpenFile(journalPath(directory, name), flag, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w %q", ErrNotFound, name)
	}
	return file, err
}

func AcquireLock(directory string, name string) (*Lock, error) {
	return acquireLock(directory, name, os.O_RDONLY)
}

func acquireLock(directory string, name string, journalFlag int) (*Lock, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}

	sessionDir, err := openSessionDir(directory, name)
	if err != nil {
		return nil, err
	}
	if err := lockFile(sessionDir); err != nil {
		_ = sessionDir.Close()
		return nil, err
	}

	journal, err := openJournal(directory, name, journalFlag)
	if err != nil {
		_ = sessionDir.Close()
		return nil, err
	}
	if err := lockFile(journal); err != nil {
		_ = journal.Close()
		_ = sessionDir.Close()
		return nil, err
	}

	return &Lock{sessionDir: sessionDir, journalFile: journal}, nil
}

func (self *Lock) Release() error {
	return errors.Join(self.journalFile.Close(), self.sessionDir.Close())
}

func IsInUse(directory string, name string) (bool, error) {
	heldLock, err := AcquireLock(directory, name)
	if errors.Is(err, ErrInUse) {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	return false, heldLock.Release()
}

func Open(directory string, name string) (*Writer, error) {
	heldLock, err := acquireLock(directory, name, os.O_WRONLY|os.O_APPEND)
	if err != nil {
		return nil, err
	}

	head, err := readHead(directory, name)
	if err != nil {
		_ = heldLock.Release()
		return nil, err
	}

	listingMeta, err := ReadMeta(directory, name)
	if err != nil {
		_ = heldLock.Release()
		return nil, err
	}

	return &Writer{
		file:        heldLock.journalFile,
		lock:        heldLock,
		directory:   directory,
		id:          head.ID,
		name:        name,
		started:     head.Time,
		journalMeta: slices.Clone(head.Meta),
		listingMeta: *listingMeta,
	}, nil
}

func lockFile(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrInUse
	}
	return err
}

func (self *Writer) SetMeta(journalMeta json.RawMessage, listingData json.RawMessage) error {
	if self.file != nil {
		return errors.New("cannot change meta after the session has been stored")
	}

	self.journalMeta = slices.Clone(journalMeta)
	self.listingData = slices.Clone(listingData)
	return nil
}

func (self *Writer) Event(event agent.Event) (time.Time, error) {
	writtenAt, err := self.write(Line{Kind: Event, Event: &event})
	if err != nil {
		return writtenAt, err
	}

	self.listingMeta.takeEvent(event, writtenAt)
	return writtenAt, writeMeta(self.directory, self.listingMeta)
}

func (self *Writer) Item(payload json.RawMessage) error {
	writtenAt, err := self.write(Line{Kind: Item, Payload: payload})
	if err != nil {
		return err
	}

	self.listingMeta.Touched = writtenAt
	return writeMeta(self.directory, self.listingMeta)
}

func (self *Writer) CompleteTurn() error {
	writtenAt, err := self.write(Line{Kind: TurnCompletion})
	if err != nil {
		return err
	}

	self.listingMeta.Touched = writtenAt
	return writeMeta(self.directory, self.listingMeta)
}

func (self *Writer) Name() string                 { return self.name }
func (self *Writer) ID() string                   { return self.id }
func (self *Writer) JournalMeta() json.RawMessage { return slices.Clone(self.journalMeta) }
func (self *Writer) Started() time.Time           { return self.started }
func (self *Writer) IsPersisted() bool            { return self.file != nil }
func (self *Writer) EnsurePersisted() error       { return self.ensureOpen() }

func (self *Writer) Close() error {
	if self.file == nil {
		return nil
	}
	if self.lock == nil {
		return self.file.Close()
	}

	return self.lock.Release()
}

func (self *Writer) write(line Line) (time.Time, error) {
	if err := self.ensureOpen(); err != nil {
		return time.Time{}, err
	}
	return self.record(line)
}

func (self *Writer) ensureOpen() error {
	if self.file != nil {
		return nil
	}

	directory := sessionDir(self.directory, self.name)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return err
	}

	sessionDir, err := openSessionDir(self.directory, self.name)
	if err != nil {
		_ = os.Remove(directory)
		return err
	}
	if err := lockFile(sessionDir); err != nil {
		_ = sessionDir.Close()
		_ = os.Remove(directory)
		return err
	}

	file, err := os.OpenFile(journalPath(self.directory, self.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
	if err != nil {
		_ = sessionDir.Close()
		_ = os.Remove(directory)
		return err
	}

	if err := lockFile(file); err != nil {
		_ = file.Close()
		_ = sessionDir.Close()
		_ = os.Remove(file.Name())
		_ = os.Remove(directory)
		return err
	}
	self.file = file
	self.lock = &Lock{sessionDir: sessionDir, journalFile: file}

	started, err := self.record(Line{Kind: Head, Version: JournalFormat, ID: self.id, Name: self.name, Meta: self.journalMeta})
	if err != nil {
		_ = self.lock.Release()
		_ = os.Remove(file.Name())
		_ = os.Remove(directory)
		self.file = nil
		self.lock = nil
		return err
	}
	self.started = started
	self.listingMeta = Meta{
		Name:    self.name,
		Data:    slices.Clone(self.listingData),
		Started: started,
		Touched: started,
	}
	if err := writeMeta(self.directory, self.listingMeta); err != nil {
		_ = self.lock.Release()
		_ = os.Remove(file.Name())
		_ = os.Remove(directory)
		self.file = nil
		self.lock = nil
		return err
	}

	return nil
}

func (self *Writer) record(line Line) (time.Time, error) {
	line.Time = time.Now()
	encodedLine, err := json.Marshal(line)
	if err != nil {
		return line.Time, err
	}

	encodedLine = append(encodedLine, '\n')
	n, err := self.file.Write(encodedLine)
	if err == nil && n != len(encodedLine) {
		return line.Time, io.ErrShortWrite
	}
	return line.Time, err
}

type Session struct {
	Name              string
	ID                string
	Meta              json.RawMessage
	Started           time.Time
	Touched           time.Time
	Events            []agent.Event
	Items             []json.RawMessage
	TurnCompletions   int
	HasIncompleteTurn bool
}

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

func Records(directory string, name string, visit func(Line) error) error {
	if err := validateName(name); err != nil {
		return err
	}

	file, err := openJournal(directory, name, os.O_RDONLY)
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

func (self *Session) take(line Line) {
	if self.Started.IsZero() {
		self.Started = line.Time
	}
	self.Touched = line.Time

	switch line.Kind {
	case Head:
		self.ID = line.ID
		self.Meta = line.Meta
	case Event:
		if line.Event != nil {
			self.Events = append(self.Events, *line.Event)
			if line.Event.Kind == agent.UserMessageEvent {
				self.HasIncompleteTurn = true
			}
		}
	case Item:
		self.Items = append(self.Items, line.Payload)
	case TurnCompletion:
		self.TurnCompletions++
		self.HasIncompleteTurn = false
	}
}

type Meta struct {
	Version  int             `json:"version"`
	Name     string          `json:"name"`
	Data     json.RawMessage `json:"data,omitempty"`
	Started  time.Time       `json:"started"`
	Touched  time.Time       `json:"touched"`
	Title    string          `json:"title,omitempty"`
	Messages int             `json:"messages"`
}

func (self *Meta) takeEvent(event agent.Event, writtenAt time.Time) {
	self.Touched = writtenAt

	if event.Kind == agent.StateChangeEvent {
		if title, isTitle := agent.TitleFromEvent(event); isTitle {
			self.Title = title
		}
		return
	}

	if event.Kind != agent.UserMessageEvent && event.Kind != agent.ModelMessageEvent {
		return
	}

	self.Messages++
	if self.Title == "" && event.Kind == agent.UserMessageEvent {
		self.Title = event.Text
	}
}

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

	sessionDir := sessionDir(directory, meta.Name)
	file, err := os.CreateTemp(sessionDir, "meta-*.json")
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

type Entry struct {
	Name    string
	ID      string
	Started time.Time
	Format  int
}

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

func Outdated(directory string) ([]string, error) {
	return namesInFormat(directory, func(stored int) bool { return stored < JournalFormat })
}

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
	file, err := openJournal(directory, name, os.O_RDONLY)
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
	file, err := openJournal(directory, name, os.O_RDONLY)
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

func sessionDir(directory, name string) string {
	return filepath.Join(directory, name)
}

func journalPath(directory, name string) string {
	return filepath.Join(sessionDir(directory, name), journalName)
}

func metaPath(directory, name string) string {
	return filepath.Join(sessionDir(directory, name), metaName)
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
