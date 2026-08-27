package sandbox

import (
	"context"
	"fmt"
	"unsafe"

	"crdx.org/io/internal/util/pathutil"

	"golang.org/x/sys/unix"
)

const (
	sysCreateRuleset = 444
	sysAddRule       = 445
	sysRestrictSelf  = 446
)

const (
	accessExecute    = 1 << 0
	accessWriteFile  = 1 << 1
	accessReadFile   = 1 << 2
	accessReadDir    = 1 << 3
	accessRemoveDir  = 1 << 4
	accessRemoveFile = 1 << 5
	accessMakeChar   = 1 << 6
	accessMakeDir    = 1 << 7
	accessMakeReg    = 1 << 8
	accessMakeSock   = 1 << 9
	accessMakeFifo   = 1 << 10
	accessMakeBlock  = 1 << 11
	accessMakeSym    = 1 << 12
	accessRefer      = 1 << 13
	accessTruncate   = 1 << 14
)

const (
	minABI         = 3
	unixSocketsABI = 9
)

const (
	rightsRead  = accessReadFile | accessReadDir
	rightsExec  = rightsRead | accessExecute
	rightsWrite = rightsRead | accessWriteFile | accessTruncate | accessRemoveDir |
		accessRemoveFile | accessMakeChar | accessMakeDir | accessMakeReg | accessMakeSock |
		accessMakeFifo | accessMakeBlock | accessMakeSym | accessRefer

	rightsFile = accessReadFile | accessExecute | accessWriteFile | accessTruncate
)

const (
	accessResolveUnix = 1 << 16
	scopeAbstractUnix = 1 << 0
)

const handledRights = rightsWrite | accessExecute

type rulesetAttr struct {
	handledAccessFS    uint64
	handledAccessNet   uint64
	scopedRestrictions uint64
}

type pathBeneathAttr struct {
	allowedAccess uint64
	parentFD      int32
	_             [4]byte // the kernel struct is packed to 8 bytes
}

func landlockVersion() (int, error) {
	version, _, errno := unix.Syscall(sysCreateRuleset, 0, 0, 1)

	if errno != 0 {
		return 0, fmt.Errorf("landlock is unavailable: %w", errno)
	}

	if int(version) < minABI {
		return 0, fmt.Errorf("landlock is version %d, and this needs at least %d", version, minABI)
	}

	return int(version), nil
}

func configuredRuleset(version int) rulesetAttr {
	attr := rulesetAttr{handledAccessFS: handledRights}

	if version >= unixSocketsABI {
		attr.handledAccessFS |= accessResolveUnix
		attr.scopedRestrictions = scopeAbstractUnix
	}

	return attr
}

func versionedRights(rights uint64, version int, background bool) uint64 {
	if background && version >= unixSocketsABI && rights&accessMakeSock != 0 {
		rights |= accessResolveUnix
	}
	return rights
}

func applyLandlock(policy Policy, version int) error {
	attr := configuredRuleset(version)

	fd, _, errno := unix.Syscall(
		sysCreateRuleset,
		uintptr(unsafe.Pointer(&attr)), //nolint:gosec // the syscall takes the struct by address
		unsafe.Sizeof(attr),
		0,
	)

	if errno != 0 {
		return fmt.Errorf("could not create the ruleset: %w", errno)
	}

	ruleset := int(fd)
	defer func() { _ = unix.Close(ruleset) }()

	for _, grant := range policy.grants() {
		if grant.isOptional && !pathutil.Exists(grant.path) {
			continue
		}

		if err := addRule(ruleset, grant.path, versionedRights(grant.rights, version, policy.Background), policy.Write); err != nil {
			return err
		}
	}

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("could not set no_new_privs: %w", err)
	}

	if _, _, errno := unix.Syscall(sysRestrictSelf, uintptr(ruleset), 0, 0); errno != 0 {
		return fmt.Errorf("could not enter the sandbox: %w", errno)
	}

	return nil
}

func addRule(ruleset int, path string, rights uint64, writableRoots []string) error {
	fd, err := openGrantPath(path, writableRoots)
	if err != nil {
		return fmt.Errorf("could not grant access to %s: %w", path, err)
	}

	defer func() { _ = unix.Close(fd) }()

	if !isDir(fd) {
		rights &= rightsFile
	}

	attr := pathBeneathAttr{allowedAccess: rights, parentFD: int32(fd)} //nolint:gosec // a descriptor fits

	_, _, errno := unix.Syscall6(
		sysAddRule,
		uintptr(ruleset),
		1,
		uintptr(unsafe.Pointer(&attr)), //nolint:gosec // the syscall takes the struct by address
		0, 0, 0,
	)

	if errno != 0 {
		return fmt.Errorf("could not grant access to %s: %w", path, errno)
	}

	return nil
}

func isDir(fd int) bool {
	var stat unix.Stat_t

	if err := unix.Fstat(fd, &stat); err != nil {
		return false
	}

	return stat.Mode&unix.S_IFMT == unix.S_IFDIR
}

// AvailableAtAll reports whether this kernel can enforce a policy at all, so a caller may refuse to
// start rather than hand a model a sandbox that is not there.
func AvailableAtAll() error {
	_, err := landlockVersion()
	return err
}

// Supported reports whether this kernel can enforce a particular policy. Every command needs a
// network namespace, nested paths need a mount namespace, and background commands need a PID
// namespace; a machine may refuse to give any of them out.
func Supported(ctx context.Context, policy Policy) error {
	if err := AvailableAtAll(); err != nil {
		return err
	}

	return checkNamespaces(ctx, policy)
}
