package location

import "crdx.org/io/internal/xdg"

const (
	namespace = "org.crdx"
	app       = "oh"
)

func GetStateDir(parts ...string) string {
	return xdg.StatePath(append([]string{namespace, app}, parts...)...)
}

func GetConfigDir(parts ...string) string {
	return xdg.ConfigPath(append([]string{namespace, app}, parts...)...)
}

func GetConfigFile() string {
	return GetConfigDir("config.toml")
}

func GetGlobalContextPath() string {
	return GetConfigDir("SYSTEM.md")
}

func GetModelCachePath(isSimulation bool) string {
	if isSimulation {
		return GetStateDir("models.sim.json")
	}

	return GetStateDir("models.json")
}

func GetModelRoundRobinPath() string {
	return GetStateDir("model-round-robin.json")
}

func GetHistoryPath() string {
	return GetStateDir("history")
}

func GetSessionsDir() string {
	return GetStateDir("sessions")
}

func GetShellHomeDir() string {
	return GetStateDir("home")
}

func GetTmpDir(name string) string {
	return GetStateDir("tmps", name)
}
