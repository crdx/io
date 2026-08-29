package cli

import "runtime/debug"

// Version returns the version embedded in the running build.
func Version() string {
	return buildVersion(debug.ReadBuildInfo())
}

func buildVersion(info *debug.BuildInfo, isAvailable bool) string {
	if !isAvailable || info == nil || info.Main.Version == "" {
		return "unknown"
	}

	return info.Main.Version
}
