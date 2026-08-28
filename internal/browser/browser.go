package browser

import "os/exec"

func Open(address string) error {
	//nolint:gosec,noctx // the caller supplies a trusted address, and the browser outlives this call
	return exec.Command("xdg-open", address).Run()
}
