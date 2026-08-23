package gitBranch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

var _ segment.Persister = &state{}

const (
	defaultRate    = 5 * time.Second
	shortHashWidth = 7
)

type state struct {
	workspaceDir string
	rate         time.Duration
	readAt       time.Time
	name         string
}

func New(workspaceDir string) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Rate time.Duration `toml:"rate"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		if args.Rate < 0 {
			return nil, fmt.Errorf("rate is %s, and wants to be longer than nothing", args.Rate)
		}

		if args.Rate == 0 {
			args.Rate = defaultRate
		}

		return &state{workspaceDir: workspaceDir, rate: args.Rate}, nil
	}
}

func (self *state) RefreshInterval() time.Duration {
	return self.rate
}

func (self *state) Persistent() bool {
	return true
}

func (self *state) Render(segment.Context) string {
	if self.readAt.IsZero() || time.Since(self.readAt) >= self.rate {
		self.name = branchOf(self.workspaceDir)
		self.readAt = time.Now()
	}

	return style.Subtle(self.name)
}

func branchOf(workspaceDir string) string {
	gitDir := gitDirOf(workspaceDir)
	if gitDir == "" {
		return ""
	}

	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD")) //nolint:gosec // HEAD of the workspace
	if err != nil {
		return ""
	}

	text := strings.TrimSpace(string(head))

	if reference, ok := strings.CutPrefix(text, "ref: "); ok {
		return strings.TrimPrefix(reference, "refs/heads/")
	}

	if len(text) >= shortHashWidth {
		return text[:shortHashWidth]
	}

	return ""
}

func gitDirOf(workspaceDir string) string {
	where := filepath.Join(workspaceDir, ".git")

	info, err := os.Stat(where)
	if err != nil {
		return ""
	}

	if info.IsDir() {
		return where
	}

	pointer, err := os.ReadFile(where) //nolint:gosec // the .git of the workspace
	if err != nil {
		return ""
	}

	elsewhere, ok := strings.CutPrefix(strings.TrimSpace(string(pointer)), "gitdir: ")
	if !ok {
		return ""
	}

	if filepath.IsAbs(elsewhere) {
		return elsewhere
	}

	return filepath.Join(workspaceDir, elsewhere)
}
