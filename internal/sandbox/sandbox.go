// Package sandbox confines a child process to granted paths and a private loopback network. Path
// metadata remains visible, and descriptors inherited before confinement remain usable.
package sandbox

import (
	"maps"
	"slices"
	"time"

	"crdx.org/io/internal/pathutil"
)

var base = []grant{
	{path: "/usr", rights: rightsExec, optional: true},               // where the commands live
	{path: "/bin", rights: rightsExec, optional: true},               // and where they lived before
	{path: "/sbin", rights: rightsExec, optional: true},              // likewise
	{path: "/lib", rights: rightsExec, optional: true},               // what they are linked against
	{path: "/lib64", rights: rightsExec, optional: true},             // likewise
	{path: "/etc/ld.so.cache", rights: rightsRead, optional: true},   // where the loader looks first
	{path: "/etc/ld.so.conf", rights: rightsRead, optional: true},    // and what it looks by
	{path: "/etc/ld.so.conf.d", rights: rightsRead, optional: true},  // likewise
	{path: "/etc/ld.so.preload", rights: rightsRead, optional: true}, // and what it loads regardless
	{path: "/etc/nsswitch.conf", rights: rightsRead, optional: true}, // where a name is looked up
	{path: "/etc/passwd", rights: rightsRead, optional: true},        // a uid, and a home directory
	{path: "/etc/group", rights: rightsRead, optional: true},         // a gid
	{path: "/etc/localtime", rights: rightsRead, optional: true},     // what the clock is read in
	{path: "/etc/os-release", rights: rightsRead, optional: true},    // which machine this is
	{path: "/etc/terminfo", rights: rightsRead, optional: true},      // what a terminal understands
	{path: "/dev/null", rights: rightsWrite, optional: true},         // where output is thrown away
	{path: "/dev/zero", rights: rightsRead, optional: true},          // and where padding comes from
	{path: "/dev/full", rights: rightsWrite, optional: true},         // a disk that is always full
	{path: "/dev/random", rights: rightsRead, optional: true},        // entropy
	{path: "/dev/urandom", rights: rightsRead, optional: true},       // and entropy that never waits
	{path: "/proc/self", rights: rightsRead, optional: true},         // what a process knows of itself
}

// Policy grants paths, environment, resources, and background behavior. Other paths and external
// networks are denied. Read entries inside Write remain read-only, and TmpDir appears at /tmp.
// Background policies run through Processes.
type Policy struct {
	Read    []string          `json:"read"`    // paths readable
	Write   []string          `json:"write"`   // paths readable and writable
	Exec    []string          `json:"exec"`    // paths binaries may run from
	TmpDir  string            `json:"tmpdir"`  // a directory to appear at /tmp
	Env     []string          `json:"env"`     // environment variables passed through
	SetEnv  map[string]string `json:"set_env"` // environment variables set by the caller
	Timeout time.Duration     `json:"timeout"` // how long a command may run

	CPUTime    time.Duration `json:"cpu_time"`   // how much processor time it may burn
	FileSize   int64         `json:"file_size"`  // the largest file it may write, in bytes
	OpenFiles  int64         `json:"open_files"` // how many descriptors it may hold at once
	Background bool          `json:"background"` // whether descendants may outlive the command
}

// WithRead returns a policy with additional readable paths without sharing the changed slice with
// the original policy.
func (self Policy) WithRead(paths ...string) Policy {
	self.Read = append(slices.Clone(self.Read), paths...)
	return self
}

// WithWrite returns a policy with additional writable paths.
func (self Policy) WithWrite(paths ...string) Policy {
	self.Write = append(slices.Clone(self.Write), paths...)
	return self
}

// WithExec returns a policy with additional executable paths.
func (self Policy) WithExec(paths ...string) Policy {
	self.Exec = append(slices.Clone(self.Exec), paths...)
	return self
}

// WithSetEnv returns a policy with an environment variable set.
func (self Policy) WithSetEnv(name string, value string) Policy {
	self.SetEnv = maps.Clone(self.SetEnv)
	if self.SetEnv == nil {
		self.SetEnv = make(map[string]string)
	}
	self.SetEnv[name] = value
	return self
}

// Writable reports whether the policy grants changes outside its temporary directory.
func (self Policy) Writable() bool {
	for _, path := range self.Write {
		if path != TmpDir {
			return true
		}
	}

	return false
}

type grant struct {
	path     string // what is granted
	rights   uint64 // what may be done with it
	optional bool   // whether its absence is acceptable
}

func (self Policy) grants() []grant {
	grants := make([]grant, 0, len(base)+len(self.Read)+len(self.Write)+len(self.Exec)+2)
	grants = append(grants, base...)
	if self.usesMountNamespace() {
		grants = append(
			grants,
			grant{path: "/dev/ptmx", rights: rightsWrite},
			grant{path: "/dev/pts", rights: rightsWrite},
		)
	}

	for _, path := range self.Read {
		grants = append(grants, grant{path: path, rights: rightsRead})
	}

	for _, path := range self.Exec {
		grants = append(grants, grant{path: path, rights: rightsExec})
	}

	for _, path := range self.Write {
		grants = append(grants, grant{path: path, rights: rightsWrite})
	}

	return grants
}

func (self Policy) missingPaths() []string {
	var absent []string

	if self.TmpDir != "" && !pathutil.Exists(self.TmpDir) {
		absent = append(absent, self.TmpDir)
	}

	for _, grant := range self.grants() {
		if !grant.optional && !pathutil.Exists(grant.path) {
			absent = append(absent, grant.path)
		}
	}

	return absent
}
