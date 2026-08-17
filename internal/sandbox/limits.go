package sandbox

import (
	"fmt"
	"slices"
	"time"

	"crdx.org/io/internal/pathutil"

	"golang.org/x/sys/unix"
)

func applyLimits(policy Policy) error {
	limits := []struct {
		resource int
		value    uint64
		set      bool
	}{
		{unix.RLIMIT_CORE, 0, true},
		{unix.RLIMIT_CPU, uint64(policy.CPUTime.Seconds()), policy.CPUTime > 0},
		{unix.RLIMIT_FSIZE, uint64(policy.FileSize), policy.FileSize > 0},    //nolint:gosec // sane rejects a negative
		{unix.RLIMIT_NOFILE, uint64(policy.OpenFiles), policy.OpenFiles > 0}, //nolint:gosec // sane rejects a negative
	}

	for _, limit := range limits {
		if !limit.set {
			continue
		}

		value := &unix.Rlimit{Cur: limit.value, Max: limit.value}

		if err := unix.Setrlimit(limit.resource, value); err != nil {
			return fmt.Errorf("could not limit resource %d: %w", limit.resource, err)
		}
	}

	return nil
}

func (self Policy) sane() error {
	if self.FileSize < 0 {
		return fmt.Errorf("a file size limit of %d is not a size", self.FileSize)
	}

	if self.OpenFiles < 0 {
		return fmt.Errorf("an open file limit of %d is not a count", self.OpenFiles)
	}

	if self.CPUTime > 0 && self.CPUTime < time.Second {
		return fmt.Errorf("a cpu limit of %s rounds down to no time at all", self.CPUTime)
	}

	return self.reachable()
}

func (self Policy) reachable() error {
	if self.TmpDir == "" {
		return nil
	}

	for _, path := range slices.Concat(self.Read, self.Write, self.Exec) {
		if path == TmpDir {
			continue
		}

		if _, covered := pathutil.RelativeTo(TmpDir, path); covered {
			return fmt.Errorf("a scratch at %s covers %s, which is granted but unreachable", TmpDir, path)
		}
	}

	return nil
}
