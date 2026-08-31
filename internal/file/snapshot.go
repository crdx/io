package file

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

// FileReadState names durable state recording file contents exposed to the model.
const FileReadState = "file_read"

// ErrNotRead is returned when no successful read has recorded a file.
var ErrNotRead = errors.New("file has not been read yet")

// ErrChangedSinceRead is returned when a file no longer has the contents that were read.
var ErrChangedSinceRead = errors.New("file has changed since it was last read")

// ReadSnapshot identifies the contents exposed from one path.
type ReadSnapshot struct {
	Path string `json:"path"`
	Hash string `json:"sha256"`
}

// ReadState is the durable state produced by operations that expose file contents.
type ReadState struct {
	Files []ReadSnapshot `json:"files"`
}

// NewReadSnapshot records the hash of content exposed from path.
func NewReadSnapshot(path string, content []byte) ReadSnapshot {
	return ReadSnapshot{Path: path, Hash: Hash(content)}
}

// EncodeReadState encodes snapshots for a durable state event.
func EncodeReadState(snapshots ...ReadSnapshot) json.RawMessage {
	state, _ := json.Marshal(ReadState{Files: snapshots}) //nolint:errchkjson // strings cannot fail JSON encoding
	return state
}

// Snapshots remembers the contents of files returned by read operations.
type Snapshots struct {
	mutex  sync.RWMutex
	hashes map[string]string
}

// NewSnapshots builds an empty set of file snapshots.
func NewSnapshots() *Snapshots {
	return &Snapshots{hashes: map[string]string{}}
}

// Record remembers the contents read from a file and returns their SHA-256 hash.
func (self *Snapshots) Record(root *Root, name string, content []byte) string {
	hash := Hash(content)
	self.recordHash(root, name, hash)
	return hash
}

// RestoreReadState applies file snapshots loaded from a stored session.
func (self *Snapshots) RestoreReadState(root *Root, payload json.RawMessage) error {
	var state ReadState
	if err := json.Unmarshal(payload, &state); err != nil {
		return err
	}

	for _, snapshot := range state.Files {
		resolvedRoot, name, err := root.Resolve(snapshot.Path)
		if errors.Is(err, ErrOutsideRoot) {
			continue
		}
		if err != nil {
			return err
		}
		if err := self.restoreHash(resolvedRoot, name, snapshot.Hash); err != nil {
			return err
		}
	}

	return nil
}

// Check confirms that content is the last version read from a file.
func (self *Snapshots) Check(root *Root, name string, content []byte) error {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	hash, ok := self.hashes[snapshotKey(root, name)]
	if !ok {
		return ErrNotRead
	}
	if hash != Hash(content) {
		return ErrChangedSinceRead
	}

	return nil
}

func (self *Snapshots) restoreHash(root *Root, name string, hash string) error {
	decodedHash, err := hex.DecodeString(hash)
	if err != nil || len(decodedHash) != sha256.Size {
		return fmt.Errorf("invalid SHA-256 hash %q", hash)
	}

	self.recordHash(root, name, hex.EncodeToString(decodedHash))
	return nil
}

func (self *Snapshots) recordHash(root *Root, name string, hash string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.hashes[snapshotKey(root, name)] = hash
}

func snapshotKey(root *Root, name string) string {
	return filepath.Join(root.Name(), filepath.Clean(name))
}

// Hash returns the SHA-256 hash used to identify file contents in durable state.
func Hash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
