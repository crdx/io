package testnamespace

import (
	"os"
	"testing"
)

const Variable = "IO_SANDBOX_TEST_UNMAPPED"

func Environment() []string {
	if os.Getenv(Variable) == "" {
		return nil
	}

	return []string{Variable + "=1"}
}

func IsUnmapped() bool {
	if !testing.Testing() || os.Getenv(Variable) == "" {
		return false
	}

	file, err := os.OpenFile("/proc/self/uid_map", os.O_WRONLY, 0)
	if err != nil {
		return true
	}
	_ = file.Close()

	return false
}
