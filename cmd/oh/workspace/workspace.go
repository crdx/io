package workspace

import (
	"errors"
	"fmt"
	"path/filepath"

	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util/pathutil"
)

var ErrShadowed = errors.New("workspace cannot use /tmp because the sandbox shadows it with private scratch space")

func Validate(workspaceDir string) error {
	if IsShadowed(workspaceDir) {
		return ErrShadowed
	}

	resolvedWorkspaceDir, err := filepath.EvalSymlinks(workspaceDir)
	if err != nil {
		return fmt.Errorf("could not resolve workspace links: %w", err)
	}
	if IsShadowed(resolvedWorkspaceDir) {
		return ErrShadowed
	}

	return nil
}

func IsShadowed(workspaceDir string) bool {
	_, shadowed := pathutil.RelativeTo(sandbox.TmpDir, workspaceDir)
	return shadowed
}
