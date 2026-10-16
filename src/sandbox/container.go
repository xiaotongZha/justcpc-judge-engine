package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const (
	initArgument      = "--justcpc-sandbox-init"
	initErrorFD       = 5
	maxInitErrorBytes = 4096
)

type initConfig struct {
	RootFS      string
	SandboxRoot string
	WorkDir     string
	Args        []string
	Env         []string
	Limit       Limit
	Profile     Profile
}

// Run creates a fresh container for one command. A fresh container is the
// correctness baseline; pool reuse is intentionally a later optimization.
func (s *Sandbox) Run(ctx context.Context, task Task) Result {
	if err := validateTask(task); err != nil {
		return Result{Status: StatusSystemError, Error: err.Error()}
	}
	limit := task.Limit.normalized()
	if task.Profile == "" {
		task.Profile = ProfileRun
	}

	workDir, err := os.MkdirTemp(s.config.WorkRoot, "justcpc-work-")
	if err != nil {
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("create work directory: %v", err)}
	}
	defer os.RemoveAll(workDir)
	if err := writeTaskFiles(workDir, task.Files); err != nil {
		return Result{Status: StatusSystemError, Error: err.Error()}
	}

	sandboxRoot, err := os.MkdirTemp(s.config.WorkRoot, "justcpc-root-")
	if err != nil {
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("create container root: %v", err)}
	}
	defer os.RemoveAll(sandboxRoot)

	stdin, err := openTaskStdin(workDir, task)
	if err != nil {
		return Result{Status: StatusSystemError, Error: err.Error()}
	}
	defer stdin.Close()
	var stdoutReader, stdoutWriter, stdoutFile *os.File
	if task.StdoutPath != "" {
		stdoutFile, err = openTaskStdout(task.StdoutPath)
		if err != nil {
			return Result{Status: StatusSystemError, Error: err.Error()}
		}
		defer stdoutFile.Close()
	} else {
		stdoutReader, stdoutWriter, err = os.Pipe()
		if err != nil {
			return Result{Status: StatusSystemError, Error: fmt.Sprintf("create stdout pipe: %v", err)}
		}
		defer stdoutReader.Close()
		defer stdoutWriter.Close()
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("create stderr pipe: %v", err)}
	}
	defer stderrReader.Close()
	defer stderrWriter.Close()

	var cg *taskCgroup
	if !s.config.AllowNoCgroupForTest {
		cg, err = createTaskCgroup(s.config.CgroupRoot, limit)
		if err != nil {
			return Result{Status: StatusSystemError, Error: fmt.Sprintf("cgroup unavailable: %v", err)}
		}
		defer cg.destroy()
	}

	configReader, configWriter, err := os.Pipe()
	if err != nil {
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("create config pipe: %v", err)}
	}
	defer configReader.Close()
	startReader, startWriter, err := os.Pipe()
	if err != nil {
		configWriter.Close()
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("create start pipe: %v", err)}
	}
	defer startReader.Close()

	config := initConfig{
		RootFS:      s.config.RootFS,
		SandboxRoot: sandboxRoot,
		WorkDir:     workDir,
		Args:        task.Args,
		Env:         task.Env,
		Limit:       limit,
		Profile:     task.Profile,
	}
	if err := json.NewEncoder(configWriter).Encode(config); err != nil {
		configWriter.Close()
		startWriter.Close()
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("encode init config: %v", err)}
	}
	if err := configWriter.Close(); err != nil {
		startWriter.Close()
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("close config pipe: %v", err)}
	}

	executable, err := os.Executable()
	if err != nil {
		startWriter.Close()
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("find executable: %v", err)}
	}
	initErrorReader, initErrorWriter, err := os.Pipe()
	if err != nil {
		startWriter.Close()
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("create init error pipe: %v", err)}
	}
	defer initErrorReader.Close()
	defer initErrorWriter.Close()
	command := exec.Command(executable, initArgument)
	command.Stdin = stdin
	if stdoutFile != nil {
		command.Stdout = stdoutFile
	} else {
		command.Stdout = stdoutWriter
	}
	command.Stderr = stderrWriter
	command.ExtraFiles = []*os.File{configReader, startReader, initErrorWriter}
	command.SysProcAttr = namespaceAttributes()

	started := time.Now()
	if err := command.Start(); err != nil {
		startWriter.Close()
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("start container: %v", err)}
	}
	_ = initErrorWriter.Close()
	// Close the parent copies. The collectors now see EOF as soon as the
	// container process closes its inherited standard streams.
	if stdoutWriter != nil {
		_ = stdoutWriter.Close()
	}
	_ = stderrWriter.Close()
	var stdoutCollector *outputCollector
	if stdoutReader != nil {
		stdoutCollector = startOutputCollector(stdoutReader, limit.OutputSize)
	}
	stderrCollector := startOutputCollector(stderrReader, limit.OutputSize)
	if cg != nil {
		if err := cg.addProcess(command.Process.Pid); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			startWriter.Close()
			return Result{Status: StatusSystemError, Error: fmt.Sprintf("join task cgroup: %v", err)}
		}
	}
	if _, err := startWriter.Write([]byte{1}); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		startWriter.Close()
		return Result{Status: StatusSystemError, Error: fmt.Sprintf("release container init: %v", err)}
	}
	_ = startWriter.Close()

	result := waitForTask(ctx, command, cg, limit, started, stdoutCollector, stdoutFile, stderrCollector)
	if stdoutCollector != nil {
		result.Stdout = stdoutCollector.result()
	}
	result.Stderr = stderrCollector.result()
	initError, err := io.ReadAll(io.LimitReader(initErrorReader, maxInitErrorBytes))
	if err != nil {
		result.Status = StatusSystemError
		result.Error = fmt.Sprintf("read init error: %v", err)
		return result
	}
	if len(initError) > 0 {
		result.Status = StatusSystemError
		result.Error = "sandbox init failed: " + string(initError)
		return result
	}
	if result.Status == StatusNormal {
		files, err := collectTaskFiles(workDir, task.CollectPaths, limit.ArtifactSize)
		if err != nil {
			result.Status = StatusSystemError
			result.Error = fmt.Sprintf("collect task files: %v", err)
			return result
		}
		result.Files = files
	}
	return result
}

func writeTaskFiles(workDir string, files []InputFile) error {
	for _, file := range files {
		if err := validateRelativePath(file.Path); err != nil {
			return fmt.Errorf("write input file %q: %w", file.Path, err)
		}
		path := filepath.Join(workDir, file.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return fmt.Errorf("create input directory: %w", err)
		}
		mode := file.Mode
		if mode == 0 {
			mode = 0600
		}
		if err := os.WriteFile(path, file.Data, mode); err != nil {
			return fmt.Errorf("write input file %q: %w", file.Path, err)
		}
	}
	return nil
}

func createTaskFile(workDir, name string, content []byte) (*os.File, error) {
	path := filepath.Join(workDir, name)
	if err := os.WriteFile(path, content, 0600); err != nil {
		return nil, fmt.Errorf("write %s: %w", name, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	return file, nil
}

func openTaskStdin(workDir string, task Task) (*os.File, error) {
	if task.StdinPath != "" {
		file, err := os.Open(task.StdinPath)
		if err != nil {
			return nil, fmt.Errorf("open stdin path %q: %w", task.StdinPath, err)
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("stat stdin path %q: %w", task.StdinPath, err)
		}
		if !info.Mode().IsRegular() {
			_ = file.Close()
			return nil, fmt.Errorf("stdin path %q is not a regular file", task.StdinPath)
		}
		return file, nil
	}
	return createTaskFile(workDir, "stdin", task.Stdin)
}

func openTaskStdout(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("stdout path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create stdout directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("open stdout path %q: %w", path, err)
	}
	return file, nil
}

func collectTaskFiles(workDir string, paths []string, limit uint64) ([]OutputFile, error) {
	var files []OutputFile
	var total uint64
	for _, requested := range paths {
		if err := validateRelativePath(requested); err != nil {
			return nil, err
		}
		root := filepath.Join(workDir, requested)
		info, err := os.Lstat(root)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", requested, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refuse symbolic link %s", requested)
		}
		if !info.IsDir() {
			file, size, err := collectTaskFile(workDir, requested, limit-total)
			if err != nil {
				return nil, err
			}
			total += size
			files = append(files, file)
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("refuse symbolic link %s", path)
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("refuse non-regular file %s", path)
			}
			relative, err := filepath.Rel(workDir, path)
			if err != nil {
				return err
			}
			file, size, err := collectTaskFile(workDir, relative, limit-total)
			if err != nil {
				return err
			}
			total += size
			files = append(files, file)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func collectTaskFile(workDir, relative string, remaining uint64) (OutputFile, uint64, error) {
	path := filepath.Join(workDir, relative)
	info, err := os.Lstat(path)
	if err != nil {
		return OutputFile{}, 0, err
	}
	if !info.Mode().IsRegular() {
		return OutputFile{}, 0, fmt.Errorf("refuse non-regular file %s", relative)
	}
	size := uint64(info.Size())
	if size > remaining {
		return OutputFile{}, 0, fmt.Errorf("artifacts exceed %d bytes", remaining)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return OutputFile{}, 0, err
	}
	return OutputFile{Path: filepath.ToSlash(relative), Data: data, Mode: info.Mode().Perm()}, size, nil
}

func waitForTask(
	ctx context.Context,
	command *exec.Cmd,
	cg *taskCgroup,
	limit Limit,
	started time.Time,
	stdout *outputCollector,
	stdoutFile *os.File,
	stderr *outputCollector,
) Result {
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	stdoutExceeded := outputExceededChannel(stdout)
	stderrExceeded := outputExceededChannel(stderr)
	var wallExceeded, cpuExceeded, outputExceeded bool
	for {
		select {
		case err := <-done:
			return completedResult(command, cg, started, err, wallExceeded, cpuExceeded, outputExceeded)
		case <-ctx.Done():
			_ = command.Process.Kill()
			err := <-done
			result := completedResult(command, cg, started, err, false, false, false)
			result.Status = StatusSystemError
			result.Error = fmt.Sprintf("execution cancelled: %v", ctx.Err())
			return result
		case <-stdoutExceeded:
			outputExceeded = true
			stdoutExceeded = nil
			_ = command.Process.Kill()
		case <-stderrExceeded:
			outputExceeded = true
			stderrExceeded = nil
			_ = command.Process.Kill()
		case <-ticker.C:
			if cg != nil {
				if err := cg.sampleUsage(); err != nil {
					_ = command.Process.Kill()
					waitErr := <-done
					result := completedResult(command, cg, started, waitErr, false, false, false)
					result.Status = StatusSystemError
					result.Error = fmt.Sprintf("sample cgroup usage: %v", err)
					return result
				}
			}
			if stdoutFileReachedLimit(stdoutFile, limit.OutputSize) {
				outputExceeded = true
				_ = command.Process.Kill()
				continue
			}
			if time.Since(started) > limit.WallTime {
				wallExceeded = true
				_ = command.Process.Kill()
				continue
			}
			if cg != nil {
				cpu, err := cg.currentCPU()
				if err != nil {
					_ = command.Process.Kill()
					err = <-done
					result := completedResult(command, cg, started, err, false, false, false)
					result.Status = StatusSystemError
					result.Error = fmt.Sprintf("read cgroup CPU time: %v", err)
					return result
				}
				if cpu > limit.Time {
					cpuExceeded = true
					_ = command.Process.Kill()
				}
			}
		}
	}
}

func stdoutFileReachedLimit(file *os.File, limit uint64) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && uint64(info.Size()) >= limit
}

func outputExceededChannel(collector *outputCollector) <-chan struct{} {
	if collector == nil {
		return nil
	}
	return collector.exceeded
}

func completedResult(command *exec.Cmd, cg *taskCgroup, started time.Time, waitErr error, wallExceeded, cpuExceeded, outputExceeded bool) Result {
	result := Result{Status: StatusSystemError, WallTime: time.Since(started)}
	if command.ProcessState != nil {
		if usage, ok := command.ProcessState.SysUsage().(*syscall.Rusage); ok {
			result.CPUTime = time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
			result.Memory = uint64(usage.Maxrss) << 10
		}
	}
	if cg != nil {
		cpu, memory, processes, err := cg.usage()
		if err != nil {
			result.Error = fmt.Sprintf("collect cgroup usage: %v", err)
			return result
		}
		result.CPUTime = cpu
		if cg.sampleMemory {
			// Maxrss can catch a short-lived allocation between cgroup samples.
			// It is not an aggregate process-tree peak, so this remains estimated.
			result.Memory = max(result.Memory, memory)
		} else {
			result.Memory = memory
		}
		result.ProcessPeak = processes
		result.MemoryEstimated = cg.sampleMemory
		result.ProcessPeakEstimated = cg.sampleProcesses
		if oom, err := cg.oomKilled(); err != nil {
			result.Error = fmt.Sprintf("read cgroup memory events: %v", err)
			return result
		} else if oom {
			result.Status = StatusMemoryLimitExceeded
			return result
		}
	}
	if outputExceeded {
		result.Status = StatusOutputLimitExceeded
		return result
	}
	if wallExceeded || cpuExceeded {
		result.Status = StatusTimeLimitExceeded
		return result
	}
	if waitErr == nil {
		result.Status = StatusNormal
		return result
	}
	var exitError *exec.ExitError
	if !errors.As(waitErr, &exitError) {
		result.Error = fmt.Sprintf("wait container: %v", waitErr)
		return result
	}
	status, ok := exitError.Sys().(syscall.WaitStatus)
	if !ok {
		result.Error = fmt.Sprintf("unexpected wait status: %v", waitErr)
		return result
	}
	if status.Exited() {
		result.Status = StatusNonzeroExit
		result.ExitCode = status.ExitStatus()
		return result
	}
	if status.Signaled() {
		result.Signal = int(status.Signal())
		switch status.Signal() {
		case syscall.SIGSYS:
			result.Status = StatusForbiddenSyscall
		case syscall.SIGXFSZ:
			result.Status = StatusOutputLimitExceeded
		case syscall.SIGXCPU, syscall.SIGKILL:
			result.Status = StatusTimeLimitExceeded
		default:
			result.Status = StatusSignaled
		}
		return result
	}
	result.Error = fmt.Sprintf("unknown process failure: %v", waitErr)
	return result
}

type outputCollector struct {
	file     *os.File
	limit    uint64
	data     []byte
	exceeded chan struct{}
	done     chan struct{}
}

func startOutputCollector(file *os.File, limit uint64) *outputCollector {
	c := &outputCollector{
		file:     file,
		limit:    limit,
		exceeded: make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
	go c.collect()
	return c
}

func (c *outputCollector) collect() {
	defer close(c.done)
	buffer := make([]byte, 32<<10)
	for {
		n, err := c.file.Read(buffer)
		if n > 0 {
			read := n
			remaining := int(c.limit) - len(c.data)
			if remaining > 0 {
				if n > remaining {
					n = remaining
				}
				c.data = append(c.data, buffer[:n]...)
			}
			if read > remaining {
				select {
				case c.exceeded <- struct{}{}:
				default:
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (c *outputCollector) result() []byte {
	<-c.done
	return c.data
}
