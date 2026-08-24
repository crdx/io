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

const settling = 100 * time.Millisecond

type Handler struct {
	Events       func() <-chan turn.Event
	Key          func(key.Key) bool
	Turn         func(turn.Event)
	TurnFinished func() bool
	Resize       func()
	Running      func() bool
	Tick         func()
	Draw         func()
}

func Run(terminal *os.File, refreshInterval time.Duration, idleInterval time.Duration, handler Handler) {
	resizeSignals := resizes()
	defer signal.Stop(resizeSignals)
	ticker := newTicker(refreshInterval)
	defer ticker.Stop()
	run(keypresses(terminal), resizeSignals, ticker.C, idleInterval, handler)
}

func run(keys <-chan key.Key, resizeSignals <-chan os.Signal, ticks <-chan time.Time, idleInterval time.Duration, handler Handler) {
	idle := idleRefresh{interval: idleInterval}
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
	interval time.Duration
	drawnAt  time.Time
}

func (self *idleRefresh) isDue() bool {
	if self.interval <= 0 || time.Since(self.drawnAt) < self.interval {
		return false
	}
	self.drawnAt = time.Now()
	return true
}

func newTicker(interval time.Duration) *time.Ticker {
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
