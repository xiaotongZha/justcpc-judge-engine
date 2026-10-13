// Package sandbox runs one command inside the judge RootFS.
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status describes an execution fact. judge maps it to its public verdicts.
type Status string

const (
	StatusNormal              Status = "normal"
	StatusNonzeroExit         Status = "nonzero_exit"
	StatusSignaled            Status = "signaled"
	StatusTimeLimitExceeded   Status = "time_limit_exceeded"
	StatusMemoryLimitExceeded Status = "memory_limit_exceeded"
	StatusOutputLimitExceeded Status = "output_limit_exceeded"
	StatusForbiddenSyscall    Status = "forbidden_syscall"
	StatusSystemError         Status = "system_error"
)

type Profile string

const (
	ProfileRun     Profile = "run"
	ProfileCompile Profile = "compile"
)

// Limit applies to a single execution. Time is CPU time; WallTime is a
// separate guard for blocked programs. Zero values select conservative defaults.
type Limit struct {
	Time         time.Duration
	WallTime     time.Duration
	Memory       uint64
	Processes    uint64
	OutputSize   uint64
	ArtifactSize uint64
	Stack        uint64
}

func (l Limit) normalized() Limit {
	if l.Time <= 0 {
		l.Time = time.Second
	}
	if l.WallTime <= 0 {
		l.WallTime = l.Time * 2
	}
	if l.Memory == 0 {
		l.Memory = 256 << 20
	}
	if l.Processes == 0 {
		l.Processes = 64
	}
	if l.OutputSize == 0 {
		l.OutputSize = 16 << 20
	}
	if l.ArtifactSize == 0 {
		l.ArtifactSize = 64 << 20
	}
	if l.Stack == 0 {
		l.Stack = 64 << 20
	}
	return l
}

type InputFile struct {
	Path string
	Data []byte
	Mode os.FileMode
}

// OutputFile is a regular file copied from the task work directory after the
// command exits. Path is always relative to /w.
type OutputFile struct {
	Path string
	Data []byte
	Mode os.FileMode
}

type Task struct {
	Args  []string
	Env   []string
	Stdin []byte
	// StdinPath is opened only when this task begins and becomes fd 0. The file
	// is not copied into the sandbox work directory.
	StdinPath string
	// StdoutPath sends fd 1 directly to a deployment-owned host file. It avoids
	// retaining a contestant's output in the judge process.
	StdoutPath string
	Files      []InputFile
	// CollectPaths names files or directories under /w that the parent should
	// copy out before the temporary work directory is removed.
	CollectPaths []string
	Limit        Limit
	Profile      Profile
}

type Result struct {
	Status      Status
	ExitCode    int
	Signal      int
	CPUTime     time.Duration
	WallTime    time.Duration
	Memory      uint64
	ProcessPeak uint64
	Stdout      []byte
	Stderr      []byte
	Files       []OutputFile
	Error       string
}

// Config identifies deployment-owned resources. CgroupRoot must be a
// delegated cgroup v2 directory, not the global cgroup root.
type Config struct {
	RootFS     string
	WorkRoot   string
	CgroupRoot string
	// AllowNoCgroupForTest exists only for fixture tests on hosts that do not
	// delegate a writable cgroup subtree. Production keeps it false.
	AllowNoCgroupForTest bool
}

type Sandbox struct {
	config Config
}

func New(config Config) (*Sandbox, error) {
	if config.RootFS == "" {
		return nil, fmt.Errorf("sandbox: RootFS is required")
	}
	rootFS, err := filepath.Abs(config.RootFS)
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve RootFS: %w", err)
	}
	info, err := os.Stat(rootFS)
	if err != nil {
		return nil, fmt.Errorf("sandbox: RootFS: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("sandbox: RootFS is not a directory: %s", rootFS)
	}
	if config.WorkRoot == "" {
		config.WorkRoot = os.TempDir()
	}
	workRoot, err := filepath.Abs(config.WorkRoot)
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve WorkRoot: %w", err)
	}
	if err := os.MkdirAll(workRoot, 0700); err != nil {
		return nil, fmt.Errorf("sandbox: create WorkRoot: %w", err)
	}
	if config.CgroupRoot == "" && !config.AllowNoCgroupForTest {
		return nil, fmt.Errorf("sandbox: CgroupRoot is required for production execution")
	}
	config.RootFS = rootFS
	config.WorkRoot = workRoot
	return &Sandbox{config: config}, nil
}

func validateTask(task Task) error {
	if len(task.Args) == 0 || task.Args[0] == "" {
		return fmt.Errorf("sandbox: command is required")
	}
	if task.Profile == "" {
		task.Profile = ProfileRun
	}
	if task.Profile != ProfileRun && task.Profile != ProfileCompile {
		return fmt.Errorf("sandbox: unknown profile %q", task.Profile)
	}
	for _, file := range task.Files {
		if err := validateRelativePath(file.Path); err != nil {
			return fmt.Errorf("sandbox: input file %q: %w", file.Path, err)
		}
	}
	if task.StdinPath != "" && len(task.Stdin) != 0 {
		return fmt.Errorf("sandbox: stdin and stdin_path are mutually exclusive")
	}
	if task.StdoutPath != "" && !filepath.IsAbs(task.StdoutPath) {
		return fmt.Errorf("sandbox: stdout_path must be absolute")
	}
	for _, path := range task.CollectPaths {
		if err := validateRelativePath(path); err != nil {
			return fmt.Errorf("sandbox: collect path %q: %w", path, err)
		}
	}
	return nil
}

func validateRelativePath(path string) error {
	if path == "" || filepath.IsAbs(path) {
		return fmt.Errorf("must be a non-empty relative path")
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("must not leave /w")
	}
	return nil
}
