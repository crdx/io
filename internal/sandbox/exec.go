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
)

const (
	envPolicy  = "IO_SANDBOX_POLICY"
	envCommand = "IO_SANDBOX_COMMAND"
	envWorker  = "IO_SANDBOX_WORKER"
	envProbe   = "IO_SANDBOX_PROBE"
)

const (
	executable = "/proc/self/exe"
	shell      = "/bin/bash"
)

const (
	notice         = "sandbox: " // what a stub that never got as far as the command says first
	notStarted     = 125         // and the status it leaves, which is one noexec of its own uses
	probeSucceeded = "sandbox probe succeeded"
)

// Init runs sandbox work encoded in the environment and returns immediately otherwise. Programs
// using Run must call it before opening sensitive resources or starting goroutines.
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

	var err error
	switch {
	case os.Getenv(envWorker) != "":
		err = execWorker(encodedPolicy, command)
	case policyBackground(encodedPolicy):
		err = superviseSandboxed(encodedPolicy)
	default:
		err = execSandboxed(encodedPolicy, command)
	}

	if err != nil {
		fmt.Fprint(os.Stderr, notice, err, "\n")
		os.Exit(notStarted)
	}

	os.Exit(0) // only a supervisor that reported its own refusal returns successfully
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

	version, err := landlockVersion()
	if err != nil {
		return err
	}

	if err := applyLandlock(policy, version); err != nil {
		return err
	}

	if err := applySeccomp(version >= unixSocketsABI); err != nil {
		return err
	}

	environment := configuredEnvironment(policy.Env, policy.SetEnv)

	if err := applyLimits(policy); err != nil {
		return err
	}

	//nolint:gosec // running the command is the point, and the sandbox is why that is safe to do
	return syscall.Exec(shell, []string{shell, "-c", command}, environment)
}

func passedEnvironment(allowed []string) []string {
	return configuredEnvironment(allowed, nil)
}

func configuredEnvironment(allowed []string, set map[string]string) []string {
	environment := make([]string, 0, len(allowed)+len(set))

	for _, name := range allowed {
		if _, overridden := set[name]; overridden {
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

// Result is what a sandboxed command produced.
type Result struct {
	Output     string        `json:"-"` // stdout and stderr, interleaved as the command wrote them
	Code       int           `json:"code"`
	CPUTime    time.Duration `json:"cpu_time"`    // user and system processor time together
	PeakMemory uint64        `json:"peak_memory"` // largest resident set in bytes
}

// Run executes command under policy and waits. Sandbox setup failures are errors; command exit
// statuses are returned in Result. The executable must call Init during startup.
func Run(ctx context.Context, directory string, command string, policy Policy) (Result, error) {
	if policy.Background {
		return Result{}, errors.New("a background policy needs a process set")
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

	var output bytes.Buffer

	stub := exec.CommandContext(ctx, executable)
	stub.Dir = directory
	stub.Stdout = &output
	stub.Stderr = &output
	stub.Env = append(
		passedEnvironment(policy.Env),
		envPolicy+"="+string(encodedPolicy),
		envCommand+"="+command,
	)

	stub.SysProcAttr = namespaceAttributes(policy)
	stub.Cancel = func() error {
		return syscall.Kill(-stub.Process.Pid, syscall.SIGKILL)
	}

	err = stub.Run()

	if stub.Process != nil {
		cleanupErr := syscall.Kill(-stub.Process.Pid, syscall.SIGKILL)
		if cleanupErr != nil && !errors.Is(cleanupErr, syscall.ESRCH) {
			return Result{Output: output.String()}, fmt.Errorf(
				"could not clean up the command's processes: %w", cleanupErr,
			)
		}
	}

	result := Result{Output: output.String()}
	if stub.ProcessState != nil {
		result.Code = stub.ProcessState.ExitCode()
		result.CPUTime = stub.ProcessState.UserTime() + stub.ProcessState.SystemTime()
		if usage, ok := stub.ProcessState.SysUsage().(*syscall.Rusage); ok && usage.Maxrss > 0 {
			result.PeakMemory = uint64(usage.Maxrss) * 1024 // Linux reports ru_maxrss in KiB
		}
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, fmt.Errorf(
			"the command did not finish within %s", policy.Timeout,
		)
	}

	if ctx.Err() != nil { // the caller stopped it, and the timeout has nothing to do with it
		return result, errors.New("the command was stopped")
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

func validate(ctx context.Context, policy Policy) error {
	if err := policy.sane(); err != nil {
		return err
	}

	if absent := policy.missingPaths(); len(absent) > 0 {
		return fmt.Errorf(
			"the policy grants paths that do not exist: %s", strings.Join(absent, ", "),
		)
	}

	return Supported(ctx, policy)
}
