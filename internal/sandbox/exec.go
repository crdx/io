package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"crdx.org/io/internal/stop"
	"crdx.org/io/internal/util"
)

const (
	envPolicy  = "IO_SANDBOX_POLICY"
	envCommand = "IO_SANDBOX_COMMAND"
	envProbe   = "IO_SANDBOX_PROBE"
)

const (
	executable = "/proc/self/exe"
	shell      = "/bin/bash"
)

const (
	notice         = "sandbox: "
	notStarted     = 125
	probeSucceeded = "sandbox probe succeeded"
)

func Init() {
	if os.Getenv(envProbe) != "" {
		if err := applyNetwork(); err != nil {
			fmt.Fprint(os.Stderr, notice, err, "\n")
			os.Exit(notStarted)
		}

		if _, err := fmt.Fprintln(os.Stdout, probeSucceeded); err != nil {
			os.Exit(notStarted)
		}
		os.Exit(0)
	}

	encodedPolicy := os.Getenv(envPolicy)
	if encodedPolicy == "" {
		return
	}

	command := os.Getenv(envCommand)

	if err := execSandboxed(encodedPolicy, command); err != nil {
		fmt.Fprint(os.Stderr, notice, err, "\n")
		os.Exit(notStarted)
	}

	os.Exit(0)
}

func execSandboxed(encodedPolicy string, command string) error {
	runtime.LockOSThread()

	var policy Policy
	if err := json.Unmarshal([]byte(encodedPolicy), &policy); err != nil {
		return fmt.Errorf("could not read the policy: %w", err)
	}

	if err := applyMounts(policy); err != nil {
		return err
	}

	if err := applyNetwork(); err != nil {
		return err
	}

	if err := dropCapabilities(); err != nil {
		return err
	}

	isUnixSocketScoped, err := applyLandlock(policy)
	if err != nil {
		return err
	}

	if err := applySeccomp(isUnixSocketScoped); err != nil {
		return err
	}

	environment := configuredEnvironment(policy.Env, policy.SetEnv)

	if err := applyLimits(policy); err != nil {
		return err
	}

	//nolint:gosec // running the command is the point, and the sandbox is why that is safe to do
	return syscall.Exec(shell, []string{shell, "-c", command}, environment)
}

func passedEnvironment(allowedNames []string) []string {
	return configuredEnvironment(allowedNames, nil)
}

func configuredEnvironment(allowedNames []string, set map[string]string) []string {
	environment := make([]string, 0, len(allowedNames)+len(set))

	for _, name := range allowedNames {
		if _, isOverridden := set[name]; isOverridden {
			continue
		}

		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}

	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		environment = append(environment, name+"="+set[name])
	}

	return environment
}

const maxOutput = 8 << 20

type boundedBuffer struct {
	buffer bytes.Buffer
}

func (self *boundedBuffer) Write(data []byte) (int, error) {
	if room := maxOutput - self.buffer.Len(); room > 0 {
		self.buffer.Write(data[:min(room, len(data))])
	}

	return len(data), nil
}

func (self *boundedBuffer) String() string {
	return self.buffer.String()
}

type Result struct {
	Output     string         `json:"-"`
	Code       int            `json:"code"`
	Signal     syscall.Signal `json:"signal"`
	CPUTime    time.Duration  `json:"cpu_time"`
	PeakMemory uint64         `json:"peak_memory"`
}

func Run(ctx context.Context, directory string, command string, policy Policy) (Result, error) {
	if policy.Yolo {
		return runYolo(ctx, directory, command, policy)
	}

	if err := validate(ctx, policy); err != nil {
		return Result{}, err
	}

	encodedPolicy, err := json.Marshal(policy)
	if err != nil {
		return Result{}, fmt.Errorf("could not write the policy: %w", err)
	}

	if policy.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, policy.Timeout)
		defer cancel()
	}

	var output boundedBuffer

	startedAt := time.Now()

	stub := exec.CommandContext(ctx, executable)
	stub.Dir = directory
	stub.Stdout = &output
	stub.Stderr = &output
	stub.Env = append(
		passedEnvironment(policy.Env),
		append(
			[]string{envPolicy + "=" + string(encodedPolicy), envCommand + "=" + command},
			unmappedTestEnvironment()...,
		)...,
	)

	stub.SysProcAttr = namespaceAttributes()
	stub.Cancel = func() error {
		return syscall.Kill(-stub.Process.Pid, syscall.SIGKILL)
	}

	err = stub.Run()

	result := collect(stub, &output)

	if ctx.Err() != nil {
		return stoppedResult(ctx, policy, result, startedAt)
	}

	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		return Result{}, fmt.Errorf("could not run the command: %w", err)
	}

	if result.Code == notStarted && strings.HasPrefix(output.String(), notice) {
		return Result{}, fmt.Errorf(
			"the sandbox could not start: %s",
			strings.TrimSpace(strings.TrimPrefix(output.String(), notice)),
		)
	}

	return result, nil
}

func collect(child *exec.Cmd, output *boundedBuffer) Result {
	result := Result{Output: output.String()}
	if child.ProcessState == nil {
		return result
	}

	result.Code = child.ProcessState.ExitCode()
	result.CPUTime = child.ProcessState.UserTime() + child.ProcessState.SystemTime()
	if status, ok := child.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		result.Signal = status.Signal()
	}
	if usage, ok := child.ProcessState.SysUsage().(*syscall.Rusage); ok && usage.Maxrss > 0 {
		result.PeakMemory = uint64(usage.Maxrss) * 1024
	}

	return result
}

func validate(ctx context.Context, policy Policy) error {
	if err := policy.sane(); err != nil {
		return err
	}

	if err := policy.grantPathsSafe(); err != nil {
		return err
	}

	if absent := policy.missingPaths(); len(absent) > 0 {
		return fmt.Errorf(
			"the policy grants paths that do not exist: %s", strings.Join(absent, ", "),
		)
	}

	return Supported(ctx)
}

func stoppedResult(ctx context.Context, policy Policy, result Result, startedAt time.Time) (Result, error) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, fmt.Errorf("the command did not finish within %s", policy.Timeout)
	}

	return result, fmt.Errorf(
		"the command was stopped after %s%s",
		util.CompactDuration(time.Since(startedAt).Round(100*time.Millisecond)),
		stop.Phrase(ctx),
	)
}
