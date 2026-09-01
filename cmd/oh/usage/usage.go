package usage

import (
	"context"
	"sync"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/state"
)

const cacheFormat = 1

type cache struct {
	Version   int                 `json:"version"`
	FetchedAt time.Time           `json:"fetched_at"`
	Windows   []agent.UsageWindow `json:"windows"`
}

type shared struct {
	reporter agent.UsageReporter
	path     string
	ttl      time.Duration
	now      func() time.Time

	mutex     sync.Mutex
	windows   []agent.UsageWindow
	fetchedAt time.Time
}

type Snapshotter interface {
	agent.UsageReporter

	GetSnapshot() ([]agent.UsageWindow, time.Time)
}

func Shared(
	reporter agent.UsageReporter, path string, ttl time.Duration, now func() time.Time,
) agent.UsageReporter {
	if reporter == nil || path == "" {
		return reporter
	}

	self := &shared{reporter: reporter, path: path, ttl: ttl, now: now}

	storedCache := self.stored()
	self.windows, self.fetchedAt = storedCache.Windows, storedCache.FetchedAt

	return self
}

func (self *shared) IsAvailable() bool {
	if self.reporter.IsAvailable() {
		return true
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	return len(self.windows) > 0
}

func (self *shared) GetSnapshot() ([]agent.UsageWindow, time.Time) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.windows, self.fetchedAt
}

func (self *shared) UsageWindows(ctx context.Context) ([]agent.UsageWindow, error) {
	hasClaimed, err := state.TryUpdate(self.path, cacheFormat, func(storedCache *cache) error {
		if self.isFresh(*storedCache) {
			self.keep(storedCache.Windows, storedCache.FetchedAt)

			return nil
		}

		windows, err := self.reporter.UsageWindows(ctx)
		if err != nil {
			return err
		}

		if len(windows) == 0 {
			return nil
		}

		fetchedAt := self.now()

		*storedCache = cache{Version: cacheFormat, FetchedAt: fetchedAt, Windows: windows}
		self.keep(windows, fetchedAt)

		return nil
	})
	if err != nil {
		return nil, err
	}

	if !hasClaimed {
		storedCache := self.stored()
		self.keep(storedCache.Windows, storedCache.FetchedAt)
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.windows, nil
}

func (self *shared) isFresh(storedCache cache) bool {
	return !storedCache.FetchedAt.IsZero() && self.now().Sub(storedCache.FetchedAt) < self.ttl
}

func (self *shared) keep(windows []agent.UsageWindow, fetchedAt time.Time) {
	if len(windows) == 0 {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.windows, self.fetchedAt = windows, fetchedAt
}

func (self *shared) stored() cache {
	var storedCache cache

	if err := state.Read(self.path, cacheFormat, &storedCache); err != nil {
		return cache{}
	}

	return storedCache
}
