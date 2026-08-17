package main

import "runtime/debug"

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown"
	}

	return info.Main.Version
}
