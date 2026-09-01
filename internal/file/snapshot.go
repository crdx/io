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

const FileReadState = "file_read"

var ErrNotRead = errors.New("file has not been read yet")

var ErrChangedSinceRead = errors.New("file has changed since it was last read")

type ReadSnapshot struct {
	Path string `json:"path"`
	Hash string `json:"sha256"`
}

type ReadState struct {
	Files []ReadSnapshot `json:"files"`
}

func NewReadSnapshot(path string, content []byte) ReadSnapshot {
	return ReadSnapshot{Path: path, Hash: Hash(content)}
}

func EncodeReadState(snapshots ...ReadSnapshot) json.RawMessage {
	state, _ := json.Marshal(ReadState{Files: snapshots}) //nolint:errchkjson // strings cannot fail JSON encoding
	return state
}

type Snapshots struct {
	mutex  sync.RWMutex
	hashes map[string]string
}

func NewSnapshots() *Snapshots {
	return &Snapshots{hashes: map[string]string{}}
}

func (self *Snapshots) Record(root *Root, name string, content []byte) string {
	hash := Hash(content)
	self.recordHash(root, name, hash)
	return hash
}

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

func Hash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
