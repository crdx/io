package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"crdx.org/io/internal/format"
)

func Update[State any](path string, supportedFormat int, update func(*State) error) error {
	lock, err := lock(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}()

	var state State
	if err := read(path, supportedFormat, &state); err != nil {
		return err
	}
	if err := update(&state); err != nil {
		return err
	}

	return write(path, state)
}

func lock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}

	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the path is ours
	if err != nil {
		return nil, fmt.Errorf("open state lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock state: %w", err)
	}

	return file, nil
}

func read(path string, supportedFormat int, state any) error {
	data, err := os.ReadFile(path) //nolint:gosec // the path is ours
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}

	storedFormat, err := format.ReadJSON(data)
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	if err := format.Check(storedFormat, supportedFormat); err != nil {
		return fmt.Errorf("read state %s: %w", path, err)
	}

	if err := json.Unmarshal(data, state); err != nil {
		return fmt.Errorf("read state: %w", err)
	}

	return nil
}

func write(path string, state any) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	pending, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	defer func() { _ = os.Remove(pending.Name()) }()

	if _, err := pending.Write(data); err != nil {
		_ = pending.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := pending.Close(); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(pending.Name(), path); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	return nil
}
