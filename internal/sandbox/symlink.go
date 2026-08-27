package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"crdx.org/io/internal/util/pathutil"

	"golang.org/x/sys/unix"
)

func openGrantPath(path string, writableRoots []string) (int, error) {
	if !filepath.IsAbs(path) {
		return -1, fmt.Errorf("%s is not an absolute path", path)
	}

	fd, err := unix.Open("/", unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}

	currentDir := string(os.PathSeparator)

	for part := range strings.SplitSeq(filepath.Clean(path), string(os.PathSeparator)) {
		if part == "" {
			continue
		}

		flags := unix.O_PATH | unix.O_CLOEXEC
		if isBeneathAny(currentDir, writableRoots) {
			flags |= unix.O_NOFOLLOW
		}

		next, err := unix.Openat(fd, part, flags, 0)
		_ = unix.Close(fd)
		if err != nil {
			return -1, err
		}

		fd = next
		currentDir = filepath.Join(currentDir, part)

		if flags&unix.O_NOFOLLOW != 0 && isSymbolicLink(fd) {
			_ = unix.Close(fd)
			return -1, fmt.Errorf("%s is a symbolic link", currentDir)
		}
	}

	return fd, nil
}

func isSymbolicLink(fd int) bool {
	var stat unix.Stat_t

	if err := unix.Fstat(fd, &stat); err != nil {
		return false
	}

	return stat.Mode&unix.S_IFMT == unix.S_IFLNK
}

func isBeneathAny(path string, roots []string) bool {
	path = filepath.Clean(path)

	for _, root := range roots {
		root = filepath.Clean(root)
		if path == root || root == "/" || strings.HasPrefix(path, root+string(os.PathSeparator)) {
			return true
		}
	}

	return false
}

func FirstSymlinkBeneath(path string, roots []string) (string, bool) {
	current := string(os.PathSeparator)

	for part := range strings.SplitSeq(filepath.Clean(path), string(os.PathSeparator)) {
		if part == "" {
			continue
		}

		current = filepath.Join(current, part)

		info, err := os.Lstat(current)
		if err != nil {
			return "", false
		}

		if info.Mode()&os.ModeSymlink != 0 && isBeneathAny(filepath.Dir(current), roots) {
			return current, true
		}
	}

	return "", false
}

func (self Policy) grantPathsSafe() error {
	for _, grant := range self.grants() {
		if grant.isOptional && !pathutil.Exists(grant.path) {
			continue
		}

		if link, redirects := FirstSymlinkBeneath(grant.path, self.Write); redirects {
			return fmt.Errorf("a grant may not pass through %s, a symbolic link", link)
		}
	}

	return nil
}
