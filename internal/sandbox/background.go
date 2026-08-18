package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unicode"

	"golang.org/x/sys/unix"
)

const supervisorStatusFD = 3

type supervisorStatus struct {
	Result Result `json:"result"`
	Error  string `json:"error,omitempty"`
}

func policyBackground(encodedPolicy string) bool {
	var policy Policy
	return json.Unmarshal([]byte(encodedPolicy), &policy) == nil && policy.Background
}

func superviseSandboxed(encodedPolicy string) error {
	status := os.NewFile(supervisorStatusFD, "sandbox status")
	if status == nil {
		return errors.New("could not open the supervisor status pipe")
	}
	defer func() { _ = status.Close() }()

	report := func(result supervisorStatus) error {
		return json.NewEncoder(status).Encode(result)
	}

	runtime.LockOSThread()

	var policy Policy
	if err := json.Unmarshal([]byte(encodedPolicy), &policy); err != nil {
		return report(supervisorStatus{Error: fmt.Sprintf("could not read the policy: %v", err)})
	}

	if err := applyMounts(policy); err != nil {
		return report(supervisorStatus{Error: err.Error()})
	}
	if err := applyNetwork(); err != nil {
		return report(supervisorStatus{Error: err.Error()})
	}
	if err := dropCapabilities(); err != nil {
		return report(supervisorStatus{Error: err.Error()})
	}

	version, err := landlockVersion()
	if err != nil {
		return report(supervisorStatus{Error: err.Error()})
	}
	if err := applyLandlock(policy, version); err != nil {
		return report(supervisorStatus{Error: err.Error()})
	}
	if err := applySeccomp(version >= unixSocketsABI); err != nil {
		return report(supervisorStatus{Error: err.Error()})
	}
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return report(supervisorStatus{Error: fmt.Sprintf("could not protect the supervisor: %v", err)})
	}

	worker := exec.Command(executable)
	worker.Stdin = os.Stdin
	worker.Stdout = os.Stdout
	worker.Stderr = os.Stderr
	worker.Env = append(os.Environ(), envWorker+"=1")

	err = worker.Run()
	result := resultFrom(worker.ProcessState)

	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		return report(supervisorStatus{Error: fmt.Sprintf("could not run the command: %v", err)})
	}

	background := hasChildren()
	_ = os.Stdout.Close()
	_ = os.Stderr.Close()

	if err := report(supervisorStatus{Result: result}); err != nil {
		return err
	}
	_ = status.Close()

	if background {
		waitForChildren()
	}
	return nil
}

func hasChildren() bool {
	for {
		var status unix.WaitStatus
		pid, err := unix.Wait4(-1, &status, unix.WNOHANG, nil)
		switch {
		case errors.Is(err, unix.ECHILD):
			return false
		case err != nil:
			return true
		case pid == 0:
			return true
		}
	}
}

func waitForChildren() {
	for {
		var status unix.WaitStatus
		_, err := unix.Wait4(-1, &status, 0, nil)
		switch {
		case errors.Is(err, unix.EINTR):
			continue
		case err != nil:
			return
		}
	}
}

func execWorker(encodedPolicy string, command string) error {
	var policy Policy
	if err := json.Unmarshal([]byte(encodedPolicy), &policy); err != nil {
		return fmt.Errorf("could not read the policy: %w", err)
	}
	if err := applyLimits(policy); err != nil {
		return err
	}

	environment := configuredEnvironment(policy.Env, policy.SetEnv)
	//nolint:gosec // running the command inside the inherited sandbox is the point
	return syscall.Exec(shell, []string{shell, "-c", command}, environment)
}

func resultFrom(state *os.ProcessState) Result {
	var result Result
	if state == nil {
		return result
	}

	result.Code = state.ExitCode()
	result.CPUTime = state.UserTime() + state.SystemTime()
	if usage, ok := state.SysUsage().(*syscall.Rusage); ok && usage.Maxrss > 0 {
		result.PeakMemory = uint64(usage.Maxrss) * 1024
	}
	return result
}

// Processes owns commands allowed to outlive a shell call.
type Processes struct {
	mutex             sync.Mutex
	backgroundEnabled bool
	runningProcesses  map[*supervised]struct{}
}

type supervised struct {
	command *exec.Cmd
	done    chan struct{}
}

// NewProcesses makes a process set in the given mode.
func NewProcesses(backgroundEnabled bool) *Processes {
	return &Processes{backgroundEnabled: backgroundEnabled, runningProcesses: map[*supervised]struct{}{}}
}

// Enable lets subsequent commands leave processes behind.
func (self *Processes) Enable() {
	self.mutex.Lock()
	self.backgroundEnabled = true
	self.mutex.Unlock()
}

// Disable prevents new background processes and kills every namespace the set owns.
func (self *Processes) Disable() ([]string, error) {
	self.mutex.Lock()
	self.backgroundEnabled = false
	runningProcesses := make([]*supervised, 0, len(self.runningProcesses))
	for process := range self.runningProcesses {
		runningProcesses = append(runningProcesses, process)
	}
	self.mutex.Unlock()

	var names []string
	for _, process := range runningProcesses {
		names = append(names, processTrees(process.command.Process.Pid)...)
	}
	slices.Sort(names)

	var stopError error
	for _, process := range runningProcesses {
		if err := process.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			stopError = errors.Join(stopError, err)
		}
	}
	for _, process := range runningProcesses {
		<-process.done
	}

	return names, stopError
}

func processTrees(root int) []string {
	seenPIDs := map[int]bool{root: true}
	var trees []string

	for _, child := range childProcesses(root) {
		if tree := processTree(child, seenPIDs); tree != "" {
			trees = append(trees, tree)
		}
	}

	slices.Sort(trees)
	return trees
}

func processTree(pid int, seenPIDs map[int]bool) string {
	if seenPIDs[pid] {
		return ""
	}
	seenPIDs[pid] = true

	var branches []string
	for _, child := range childProcesses(pid) {
		if branch := processTree(child, seenPIDs); branch != "" {
			branches = append(branches, branch)
		}
	}
	slices.Sort(branches)

	return formatProcessTree(processName(pid), branches)
}

func formatProcessTree(name string, branches []string) string {
	switch {
	case name == "":
		return strings.Join(branches, ", ")
	case len(branches) == 0:
		return name
	default:
		return name + " → " + strings.Join(branches, ", ")
	}
}

func childProcesses(pid int) []int {
	tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return nil
	}

	childPIDs := map[int]bool{}
	for _, task := range tasks {
		children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/children", pid, task.Name()))
		if err != nil {
			continue
		}

		for child := range strings.FieldsSeq(string(children)) {
			if childPID, err := strconv.Atoi(child); err == nil {
				childPIDs[childPID] = true
			}
		}
	}

	out := make([]int, 0, len(childPIDs))
	for child := range childPIDs {
		out = append(out, child)
	}
	return out
}

func processName(pid int) string {
	processNameBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return safeProcessName(string(processNameBytes))
}

func safeProcessName(processNameText string) string {
	var name strings.Builder
	for _, character := range strings.TrimSpace(processNameText) {
		if unicode.IsPrint(character) && !strings.ContainsRune("(),", character) {
			name.WriteRune(character)
		} else {
			name.WriteRune('?')
		}
	}
	return name.String()
}

// Run confines a command and keeps its PID namespace while backgrounding is enabled.
func (self *Processes) Run(
	ctx context.Context,
	directory string,
	command string,
	policy Policy,
) (Result, error) {
	self.mutex.Lock()
	backgroundEnabled := self.backgroundEnabled
	self.mutex.Unlock()

	if !backgroundEnabled {
		policy.Background = false
		return Run(ctx, directory, command, policy)
	}

	policy.Background = true
	policy = policy.WithExec(executable)

	if err := validate(ctx, policy); err != nil {
		return Result{}, err
	}

	if policy.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, policy.Timeout)
		defer cancel()
	}

	encodedPolicy, err := json.Marshal(policy)
	if err != nil {
		return Result{}, fmt.Errorf("could not write the policy: %w", err)
	}

	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		return Result{}, fmt.Errorf("could not open the output pipe: %w", err)
	}
	defer func() { _ = outputRead.Close() }()

	statusRead, statusWrite, err := os.Pipe()
	if err != nil {
		_ = outputRead.Close()
		_ = outputWrite.Close()
		return Result{}, fmt.Errorf("could not open the status pipe: %w", err)
	}
	defer func() { _ = statusRead.Close() }()

	stub := exec.Command(executable)
	stub.Dir = directory
	stub.Stdout = outputWrite
	stub.Stderr = outputWrite
	stub.ExtraFiles = []*os.File{statusWrite}
	stub.Env = append(
		passedEnvironment(policy.Env),
		envPolicy+"="+string(encodedPolicy),
		envCommand+"="+command,
	)
	stub.SysProcAttr = namespaceAttributes(policy)

	self.mutex.Lock()
	if !self.backgroundEnabled {
		self.mutex.Unlock()
		_ = outputRead.Close()
		_ = outputWrite.Close()
		_ = statusRead.Close()
		_ = statusWrite.Close()
		policy.Background = false
		return Run(ctx, directory, command, policy)
	}

	if err := stub.Start(); err != nil {
		self.mutex.Unlock()
		_ = outputWrite.Close()
		_ = statusWrite.Close()
		return Result{}, fmt.Errorf("could not run the command: %w", err)
	}

	process := &supervised{command: stub, done: make(chan struct{})}
	self.runningProcesses[process] = struct{}{}
	self.mutex.Unlock()

	_ = outputWrite.Close()
	_ = statusWrite.Close()

	go func() {
		_ = stub.Wait()
		self.mutex.Lock()
		delete(self.runningProcesses, process)
		close(process.done)
		self.mutex.Unlock()
	}()

	var output bytes.Buffer
	outputDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&output, outputRead)
		outputDone <- copyErr
	}()

	type supervisorReply struct {
		status supervisorStatus
		err    error
	}
	statusDone := make(chan supervisorReply, 1)
	go func() {
		var got supervisorStatus
		decodeErr := json.NewDecoder(statusRead).Decode(&got)
		statusDone <- supervisorReply{status: got, err: decodeErr}
	}()

	var got supervisorStatus
	select {
	case reply := <-statusDone:
		if reply.err != nil {
			_ = process.command.Process.Kill()
			<-process.done
			<-outputDone
			return Result{Output: output.String()}, fmt.Errorf(
				"the sandbox supervisor stopped: %w", reply.err,
			)
		}
		got = reply.status
	case <-ctx.Done():
		_ = process.command.Process.Kill()
		<-process.done
		<-outputDone
		result := Result{Output: output.String()}
		return stoppedResult(ctx, policy, result)
	}

	select {
	case err := <-outputDone:
		if err != nil {
			_ = process.command.Process.Kill()
			<-process.done
			return Result{}, fmt.Errorf("could not read the command output: %w", err)
		}
	case <-ctx.Done():
		_ = process.command.Process.Kill()
		<-process.done
		<-outputDone
		result := got.Result
		result.Output = output.String()
		return stoppedResult(ctx, policy, result)
	}

	result := got.Result
	result.Output = output.String()
	if got.Error != "" {
		<-process.done
		return Result{}, fmt.Errorf("the sandbox could not start: %s", got.Error)
	}
	if result.Code == notStarted && strings.HasPrefix(result.Output, notice) {
		return Result{}, fmt.Errorf(
			"the sandbox could not start: %s",
			strings.TrimSpace(strings.TrimPrefix(result.Output, notice)),
		)
	}

	return result, nil
}

func stoppedResult(ctx context.Context, policy Policy, result Result) (Result, error) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, fmt.Errorf("the command did not finish within %s", policy.Timeout)
	}
	return result, errors.New("the command was stopped")
}
