package gc

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"

	"crdx.org/duckopt/v2"

	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohctl/console"
	"crdx.org/io/internal/util"
	"crdx.org/io/session"
)

const usage = `ohctl gc — remove the caches sessions leave behind

Usage:
    $0 gc [options]

Options:
    -a, --aggressive    Look through the whole of each directory rather than only its root
    -n, --dry-run       Report what would be removed without removing it
    -h, --help          Show this help
`

const (
	cacheName     = ".cache"
	bytePrecision = 3
	farmLabel     = "farm"
	homeLabel     = "home"
	sweepsPerCPU  = 4
	minimumSweeps = 32
	writablePerm  = 0o200
	ownerPerm     = 0o700
)

type Directories struct {
	Farm     string
	Sessions string
	Home     string
}

type root struct {
	path  string
	label string
}

type cache struct {
	path string
	name string
}

type result struct {
	count          int
	reclaimedBytes int64
	failures       []string
}

type inputOpts struct {
	GC         bool `docopt:"gc"`
	DryRun     bool `docopt:"--dry-run"`
	Aggressive bool `docopt:"--aggressive"`
}

type options struct {
	isDryRun     bool
	isAggressive bool
}

func Run() error {
	bound := duckopt.MustBind[inputOpts](usage, "$0")

	directories := Directories{
		Farm:     location.GetFarmDir(),
		Sessions: location.GetSessionsDir(),
		Home:     location.GetShellHomeDir(),
	}

	return run(directories, options{
		isDryRun:     bound.DryRun,
		isAggressive: bound.Aggressive,
	}, console.Standard())
}

func run(directories Directories, choice options, output console.Output) error {
	roots, runningCount, err := collect(directories)
	if err != nil {
		return err
	}

	total := sweep(roots, choice)

	slices.Sort(total.failures)
	for _, failure := range total.failures {
		_, _ = fmt.Fprintln(output.Failure, style.Failure(failure))
	}

	_, _ = fmt.Fprintln(output.Screen, style.Subtle(
		summary(total.count, total.reclaimedBytes, runningCount, choice.isDryRun),
	))

	if failureCount := len(total.failures); failureCount > 0 {
		return fmt.Errorf("%d of %d could not be removed", failureCount, total.count+failureCount)
	}

	return nil
}

func collect(directories Directories) ([]root, int, error) {
	entries, err := os.ReadDir(directories.Farm)
	if err != nil && !os.IsNotExist(err) {
		return nil, 0, err
	}

	roots := make([]root, 0, len(entries)+1)
	runningCount := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		isRunning, err := session.IsInUse(directories.Sessions, entry.Name())
		if err == nil && isRunning {
			runningCount++
			continue
		}

		roots = append(roots, root{
			path:  filepath.Join(directories.Farm, entry.Name()),
			label: filepath.Join(farmLabel, entry.Name()),
		})
	}

	return append(roots, root{path: directories.Home, label: homeLabel}), runningCount, nil
}

func sweep(roots []root, choice options) result {
	queue := make(chan root)
	results := make(chan result)

	var sweepers sync.WaitGroup
	for range min(max(runtime.NumCPU()*sweepsPerCPU, minimumSweeps), len(roots)) {
		sweepers.Go(func() {
			for next := range queue {
				results <- take(next, choice)
			}
		})
	}

	go func() {
		for _, next := range roots {
			queue <- next
		}
		close(queue)
	}()

	go func() {
		sweepers.Wait()
		close(results)
	}()

	var total result
	for one := range results {
		total.count += one.count
		total.reclaimedBytes += one.reclaimedBytes
		total.failures = append(total.failures, one.failures...)
	}

	return total
}

func take(next root, choice options) result {
	var outcome result

	caches, err := find(next, choice.isAggressive)
	if err != nil {
		outcome.failures = append(outcome.failures, next.label+": "+err.Error())
	}

	for _, found := range caches {
		removedBytes, err := size(found.path)
		if err == nil && !choice.isDryRun {
			err = remove(found.path)
		}
		if err != nil {
			outcome.failures = append(outcome.failures, found.name+": "+err.Error())
			continue
		}

		outcome.count++
		outcome.reclaimedBytes += removedBytes
	}

	return outcome
}

func remove(path string) error {
	err := os.RemoveAll(path)
	if err == nil || !os.IsPermission(err) {
		return err
	}

	if err := unlock(path); err != nil {
		return err
	}

	return os.RemoveAll(path)
}

func unlock(root string) error {
	var lockedDirectories []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return skip(entry)
		}
		if !entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return skip(entry)
		}
		if info.Mode().Perm()&writablePerm == 0 {
			lockedDirectories = append(lockedDirectories, path)
		}

		return nil
	})
	if err != nil {
		return err
	}

	for _, path := range lockedDirectories {
		if err := os.Chmod(path, ownerPerm); err != nil {
			return err
		}
	}

	return nil
}

func find(next root, isAggressive bool) ([]cache, error) {
	if !isAggressive {
		return atRoot(next), nil
	}

	var caches []cache

	err := filepath.WalkDir(next.path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return skip(entry)
		}
		if !entry.IsDir() || entry.Name() != cacheName {
			return nil
		}

		name, err := filepath.Rel(next.path, path)
		if err != nil {
			return err
		}
		caches = append(caches, cache{path: path, name: filepath.Join(next.label, name)})

		return fs.SkipDir
	})
	if err != nil && !os.IsNotExist(err) {
		return caches, err
	}

	return caches, nil
}

func atRoot(next root) []cache {
	path := filepath.Join(next.path, cacheName)

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return nil
	}

	return []cache{{path: path, name: filepath.Join(next.label, cacheName)}}
}

func summary(count int, reclaimedBytes int64, runningCount int, isDryRun bool) string {
	text := plural(count, "cache") + ", " + util.FormatBytes(reclaimedBytes, bytePrecision)
	if isDryRun {
		text += " to reclaim"
	} else {
		text += " reclaimed"
	}

	if runningCount > 0 {
		text += ", " + plural(runningCount, "running session") + " left alone"
	}

	return text
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", count, noun)
}

func skip(entry fs.DirEntry) error {
	if entry != nil && entry.IsDir() {
		return fs.SkipDir
	}

	return nil
}

func size(root string) (int64, error) {
	var totalBytes int64

	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return skip(entry)
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return skip(entry)
		}
		totalBytes += info.Size()

		return nil
	})

	return totalBytes, err
}
