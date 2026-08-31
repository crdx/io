package onboarding

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"crdx.org/io/cmd/oh/config"
)

func setInitialModel(path string, selection string) (bool, error) {
	lock, err := lockConfig(path)
	if err != nil {
		return false, err
	}
	defer unlockConfig(lock)

	settings, err := config.Load(path)
	if err != nil {
		return false, err
	}
	if len(settings.Model.RoundRobin) > 0 {
		return false, nil
	}

	contents, err := os.ReadFile(path) //nolint:gosec // the configured path is ours
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}

	updatedContents := addInitialModel(contents, selection)
	if err := writeConfig(path, updatedContents); err != nil {
		return false, err
	}

	return true, nil
}

func addInitialModel(contents []byte, selection string) []byte {
	setting := "round_robin = [" + strconv.Quote(selection) + "]\n"
	if len(contents) == 0 {
		return fmt.Appendf(nil, "version = %d\n\n[model]\n%s", config.Format, setting)
	}

	lines := strings.SplitAfter(string(contents), "\n")
	for i, line := range lines {
		header, _, _ := strings.Cut(line, "#")
		header = strings.TrimSpace(header)
		if header == "[model]" || header == `["model"]` {
			return []byte(strings.Join(lines[:i+1], "") + setting + strings.Join(lines[i+1:], ""))
		}
	}

	separator := "\n"
	if strings.HasSuffix(string(contents), "\n") {
		separator = ""
	}

	return fmt.Appendf(contents, "%s\n[model]\n%s", separator, setting)
}

func lockConfig(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}

	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the path is ours
	if err != nil {
		return nil, fmt.Errorf("open config lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock config: %w", err)
	}

	return lock, nil
}

func unlockConfig(lock *os.File) {
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func writeConfig(path string, contents []byte) error {
	pending, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer func() { _ = os.Remove(pending.Name()) }()

	if _, err := pending.Write(contents); err != nil {
		_ = pending.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := pending.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(pending.Name(), path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}
