package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"crdx.org/io/internal/pathutil"

	"golang.org/x/sys/unix"
)

func (self Policy) nestedPaths() []string {
	var inside []string

	for _, read := range self.Read {
		for _, write := range self.Write {
			if _, ok := pathutil.RelativeTo(write, read); ok {
				inside = append(inside, read)
				break
			}
		}
	}

	return inside
}

// TmpDir is where a policy's scratch space is attached inside the sandbox.
const TmpDir = "/tmp"

func (self Policy) usesMountNamespace() bool { // rearranging mounts is the only reason to unshare
	return len(self.nestedPaths()) > 0 || self.TmpDir != ""
}

func checkNamespaces(ctx context.Context, policy Policy) error {
	probeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	probe := namespaceProbeCommand(probeContext, policy)
	output, err := probe.CombinedOutput()
	message := strings.TrimSpace(strings.TrimPrefix(string(output), notice))
	if err != nil {
		if message != "" {
			return fmt.Errorf("this machine will not give the sandbox its namespaces: %s", message)
		}
		return fmt.Errorf("this machine will not give the sandbox its namespaces: %w", err)
	}
	if message != probeSucceeded {
		return errors.New("the executable did not initialise the sandbox namespace probe")
	}

	return nil
}

func namespaceProbeCommand(ctx context.Context, policy Policy) *exec.Cmd {
	probe := exec.CommandContext(ctx, executable, "-test.run=^$")
	probe.Env = []string{envProbe + "=1"}
	probe.SysProcAttr = namespaceAttributes(policy)
	return probe
}

func applyMounts(policy Policy) error {
	if policy.usesMountNamespace() { // a private devpts would otherwise replace the real one
		if err := mountPseudoterminals(); err != nil {
			return err
		}
	}

	for _, path := range policy.nestedPaths() {
		if err := mountReadOnly(path); err != nil {
			return err
		}
	}

	if policy.TmpDir != "" {
		return attach(policy.TmpDir, TmpDir, nil)
	}

	return nil
}

func mountReadOnly(path string) error {
	return attach(path, path, &unix.MountAttr{Attr_set: unix.MOUNT_ATTR_RDONLY})
}

func mountPseudoterminals() error {
	flags := uintptr(unix.MS_NOSUID | unix.MS_NOEXEC)
	options := "newinstance,ptmxmode=0666,mode=0620,gid=0"
	if err := unix.Mount("devpts", "/dev/pts", "devpts", flags, options); err != nil {
		return fmt.Errorf("could not mount private pseudoterminals: %w", err)
	}
	if err := unix.Mount("/dev/pts/ptmx", "/dev/ptmx", "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("could not attach the pseudoterminal multiplexer: %w", err)
	}
	return nil
}

func attach(source string, target string, attributes *unix.MountAttr) error {
	const clone = unix.OPEN_TREE_CLONE | unix.OPEN_TREE_CLOEXEC // shallow: a mount within is left out

	fd, err := unix.OpenTree(unix.AT_FDCWD, source, clone)
	if err != nil {
		return fmt.Errorf("could not copy the mount at %s: %w", source, err)
	}

	defer func() { _ = unix.Close(fd) }()

	if attributes != nil {
		if err := unix.MountSetattr(fd, "", unix.AT_EMPTY_PATH, attributes); err != nil {
			return fmt.Errorf("could not set the attributes of %s: %w", source, err)
		}
	}

	err = unix.MoveMount(fd, "", unix.AT_FDCWD, target, unix.MOVE_MOUNT_F_EMPTY_PATH)
	if err != nil {
		return fmt.Errorf("could not put %s at %s: %w", source, target, err)
	}

	return nil
}

const lastCapability = 63

func dropCapabilities() error { // the bounding set is what root of a namespace keeps across execve
	for capability := range lastCapability + 1 {
		err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capability), 0, 0, 0)
		if err != nil && !errors.Is(err, unix.EINVAL) { // a capability this kernel lacks
			return fmt.Errorf("could not drop capability %d: %w", capability, err)
		}
	}

	return nil
}

func namespaceAttributes(policy Policy) *syscall.SysProcAttr {
	flags := uintptr(syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET | syscall.CLONE_NEWPID)
	if policy.usesMountNamespace() {
		flags |= syscall.CLONE_NEWNS
	}

	return &syscall.SysProcAttr{
		Setpgid:     !policy.Background,
		Cloneflags:  flags,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
	}
}
