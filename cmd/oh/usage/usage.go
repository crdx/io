// Package usage shares one subscription snapshot between every session running at once.
//
// A subscription belongs to the person rather than to the session, so five sessions asking their
// provider the same question five times over is five times the traffic for one answer, and the
// endpoints that answer it are undocumented and unbudgeted. One file under the state directory
// holds the last snapshot: whoever finds it stale refreshes it, and whoever finds another session
// already refreshing carries on with what is written rather than queueing behind it.
//
// The file is also what a session shows before it has figures of its own. Codex reports usage from
// the headers of a turn it has taken, so a session that has taken none knows nothing until the
// first one lands; reading another session's snapshot means the bar has something to say from the
// moment it is drawn.
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

	mutex   sync.Mutex
	windows []agent.UsageWindow
}

// Shared wraps a reporter so that what it costs to ask is paid once across every session, and so
// that a session with nothing of its own to say starts from what the last one found out. A
// snapshot counts as fresh for ttl, which is how often the caller means to ask.
func Shared(
	reporter agent.UsageReporter, path string, ttl time.Duration, now func() time.Time,
) agent.UsageReporter {
	if reporter == nil || path == "" {
		return reporter
	}

	self := &shared{reporter: reporter, path: path, ttl: ttl, now: now}
	self.windows = self.stored().Windows

	return self
}

// IsAvailable answers for the cache as well as the reporter, so a provider that knows nothing yet
// still shows what another session found out.
func (self *shared) IsAvailable() bool {
	if self.reporter.IsAvailable() {
		return true
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	return len(self.windows) > 0
}

func (self *shared) UsageWindows(ctx context.Context) ([]agent.UsageWindow, error) {
	claimed, err := state.TryUpdate(self.path, cacheFormat, func(stored *cache) error {
		if self.isFresh(*stored) {
			self.keep(stored.Windows)

			return nil
		}

		windows, err := self.reporter.UsageWindows(ctx)
		if err != nil {
			return err
		}

		if len(windows) == 0 {
			return nil
		}

		*stored = cache{Version: cacheFormat, FetchedAt: self.now(), Windows: windows}
		self.keep(windows)

		return nil
	})
	if err != nil {
		return nil, err
	}

	if !claimed {
		self.keep(self.stored().Windows)
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.windows, nil
}

func (self *shared) isFresh(stored cache) bool {
	return !stored.FetchedAt.IsZero() && self.now().Sub(stored.FetchedAt) < self.ttl
}

func (self *shared) keep(windows []agent.UsageWindow) {
	if len(windows) == 0 {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.windows = windows
}

func (self *shared) stored() cache {
	var stored cache

	if err := state.Read(self.path, cacheFormat, &stored); err != nil {
		return cache{}
	}

	return stored
}
