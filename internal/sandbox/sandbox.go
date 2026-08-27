package sandbox

import (
	"maps"
	"slices"
	"time"

	"crdx.org/io/internal/util/pathutil"
)

var base = []grant{
	{path: "/usr", rights: rightsExec, isOptional: true},
	{path: "/bin", rights: rightsExec, isOptional: true},
	{path: "/sbin", rights: rightsExec, isOptional: true},
	{path: "/lib", rights: rightsExec, isOptional: true},
	{path: "/lib64", rights: rightsExec, isOptional: true},
	{path: "/etc/ld.so.cache", rights: rightsRead, isOptional: true},
	{path: "/etc/ld.so.conf", rights: rightsRead, isOptional: true},
	{path: "/etc/ld.so.conf.d", rights: rightsRead, isOptional: true},
	{path: "/etc/ld.so.preload", rights: rightsRead, isOptional: true},
	{path: "/etc/fonts", rights: rightsRead, isOptional: true},
	{path: "/etc/ssl/openssl.cnf", rights: rightsRead, isOptional: true},
	{path: "/etc/nsswitch.conf", rights: rightsRead, isOptional: true},
	{path: "/etc/passwd", rights: rightsRead, isOptional: true},
	{path: "/etc/group", rights: rightsRead, isOptional: true},
	{path: "/etc/localtime", rights: rightsRead, isOptional: true},
	{path: "/etc/os-release", rights: rightsRead, isOptional: true},
	{path: "/etc/terminfo", rights: rightsRead, isOptional: true},
	{path: "/dev/null", rights: rightsWrite, isOptional: true},
	{path: "/dev/zero", rights: rightsRead, isOptional: true},
	{path: "/dev/full", rights: rightsWrite, isOptional: true},
	{path: "/dev/random", rights: rightsRead, isOptional: true},
	{path: "/dev/urandom", rights: rightsRead, isOptional: true},
	{path: "/proc/self", rights: rightsRead, isOptional: true},
}

// Policy grants paths, environment, resources, and background behavior. Other paths and external
// networks are denied. Read entries inside Write remain read-only, and TmpDir appears at /tmp. A
// grant may not pass through a symbolic link beneath a writable path, which the model could point
// anywhere. VirtualResolver replaces the host resolver files with a deterministic private-loopback
// configuration. Background policies run through Processes.
type Policy struct {
	Read            []string          `json:"read"`
	Write           []string          `json:"write"` // paths readable and writable
	Exec            []string          `json:"exec"`
	TmpDir          string            `json:"tmpdir"`           // a directory to appear at /tmp
	VirtualResolver bool              `json:"virtual_resolver"` // synthesise the resolver files
	Env             []string          `json:"env"`
	SetEnv          map[string]string `json:"set_env"`
	Timeout         time.Duration     `json:"timeout"`

	CPUTime    time.Duration `json:"cpu_time"`
	FileSize   int64         `json:"file_size"` // the largest file it may write, in bytes
	OpenFiles  int64         `json:"open_files"`
	Background bool          `json:"background"` // whether descendants may outlive the command
}

// WithRead returns a policy with additional readable paths without sharing the changed slice with
// the original policy.
func (self Policy) WithRead(paths ...string) Policy {
	self.Read = append(slices.Clone(self.Read), paths...)
	return self
}

// WithoutRead returns a policy without the named readable paths.
func (self Policy) WithoutRead(paths ...string) Policy {
	self.Read = slices.DeleteFunc(slices.Clone(self.Read), func(path string) bool {
		return slices.Contains(paths, path)
	})
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
	path       string
	rights     uint64
	isOptional bool
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

	if self.VirtualResolver {
		for _, file := range resolverFiles {
			grants = append(grants, grant{path: file.path, rights: rightsRead})
		}
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
		if !grant.isOptional && !pathutil.Exists(grant.path) {
			absent = append(absent, grant.path)
		}
	}

	return absent
}
