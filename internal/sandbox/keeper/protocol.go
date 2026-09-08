package keeper

import "time"

const (
	requestSpawn  = "spawn"
	requestSignal = "signal"

	replyReady    = "ready"
	replySpawned  = "spawned"
	replyRefused  = "refused"
	replyFinished = "finished"
)

type request struct {
	Kind        string   `json:"kind"`
	Token       uint64   `json:"token"`
	Directory   string   `json:"directory,omitempty"`
	Environment []string `json:"environment,omitempty"`
	Signal      int      `json:"signal,omitempty"`
}

type reply struct {
	Kind       string        `json:"kind"`
	Token      uint64        `json:"token"`
	Failure    string        `json:"failure,omitempty"`
	ExitCode   int           `json:"code,omitempty"`
	Signal     int           `json:"signal,omitempty"`
	CPUTime    time.Duration `json:"cpu_time,omitempty"`
	PeakMemory uint64        `json:"peak_memory,omitempty"`
}
