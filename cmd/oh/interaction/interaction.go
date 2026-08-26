package interaction

import (
	"bufio"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/turn"
)

const (
	settling  = 100 * time.Millisecond
	heartRate = 15 * time.Second
)

type Handler struct {
	Events       func() <-chan turn.Event
	Key          func(key.Key) bool
	Turn         func(turn.Event)
	TurnFinished func() bool
	Resize       func()
	Running      func() bool
	Tick         func()
	Beat         func()
	Draw         func()
}

func Run(terminal *os.File, refreshInterval time.Duration, getIdleInterval func() time.Duration, handler Handler) {
	resizeSignals := resizes()
	defer signal.Stop(resizeSignals)

	ticker := newOptionalTicker(refreshInterval)
	defer ticker.Stop()

	beater := time.NewTicker(heartRate)
	defer beater.Stop()

	run(keypresses(terminal), resizeSignals, ticker.C, beater.C, getIdleInterval, handler)
}

func run(keys <-chan key.Key, resizeSignals <-chan os.Signal, ticks <-chan time.Time, beats <-chan time.Time, getIdleInterval func() time.Duration, handler Handler) {
	idle := idleRefresh{getInterval: getIdleInterval}
	for {
		select {
		case keypress, open := <-keys:
			if !open || !handler.Key(keypress) {
				return
			}
		case event, running := <-handler.Events():
			if running {
				handler.Turn(event)
			} else if !handler.TurnFinished() {
				return
			}
		case <-resizeSignals:
			settle(resizeSignals)
			handler.Resize()
		case <-beats:
			handler.Beat()
			continue
		case <-ticks:
			if handler.Running() {
				handler.Tick()
			} else if !idle.isDue() {
				continue
			}
		}
		handler.Draw()
	}
}

type idleRefresh struct {
	getInterval func() time.Duration
	drawnAt     time.Time
}

func (self *idleRefresh) isDue() bool {
	interval := self.getInterval()
	if interval <= 0 || time.Since(self.drawnAt) < interval {
		return false
	}
	self.drawnAt = time.Now()
	return true
}

func newOptionalTicker(interval time.Duration) *time.Ticker {
	if interval <= 0 {
		ticker := time.NewTicker(time.Hour)
		ticker.Stop()
		return ticker
	}
	return time.NewTicker(interval)
}

func keypresses(terminal *os.File) <-chan key.Key {
	keys := make(chan key.Key)
	go func() {
		defer close(keys)
		decoder := key.NewDecoder(bufio.NewReader(terminal))
		for {
			keypress, err := decoder.Next()
			if err != nil {
				return
			}
			keys <- keypress
		}
	}()
	return keys
}

func resizes() chan os.Signal {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	return signals
}

func settle(signals <-chan os.Signal) {
	time.Sleep(settling)
	for {
		select {
		case <-signals:
		default:
			return
		}
	}
}
