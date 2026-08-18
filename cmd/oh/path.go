package main

import "crdx.org/io/internal/xdgutil"

const (
	namespace = "org.crdx"
	app       = "oh"
)

func stateDir(parts ...string) string {
	return xdgutil.StatePath(append([]string{namespace, app}, parts...)...)
}

func configDir(parts ...string) string {
	return xdgutil.ConfigPath(append([]string{namespace, app}, parts...)...)
}

func configPath() string {
	return configDir("config.toml")
}

func globalContextPath() string {
	return configDir("SYSTEM.md")
}

func historyPath() string {
	return stateDir("history")
}

func sessionsDir() string {
	return stateDir("sessions")
}

func shellHomeDir() string {
	return stateDir("home")
}

func tmpDir(id string) string { // one per session, so a resumed one finds what it left
	return stateDir("tmps", id)
}
