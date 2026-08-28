package tty

import (
	"os"
	"os/signal"
	"syscall"
)

var fatalSignals = []os.Signal{
	syscall.SIGHUP,
	syscall.SIGINT,
	syscall.SIGQUIT,
	syscall.SIGTERM,
}

func RestoreOnSignal(restore func()) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, fatalSignals...)

	stopped := make(chan struct{})

	go watch(signals, stopped, restore, reraise)

	return func() {
		signal.Stop(signals)
		close(stopped)
	}
}

func watch(signals <-chan os.Signal, stopped <-chan struct{}, restore func(), raise func(os.Signal)) {
	select {
	case received := <-signals:
		restore()
		raise(received)
	case <-stopped:
	}
}

func reraise(received os.Signal) {
	signal.Reset(received)

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		os.Exit(1)
	}

	_ = process.Signal(received)
}
