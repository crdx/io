package sandbox

import "crdx.org/io/internal/sandbox/loopback"

func applyNetwork() error {
	return loopback.Up()
}
