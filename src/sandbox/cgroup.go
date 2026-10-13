package sandbox

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// taskCgroup is deliberately small. The deployment gives the engine a
// delegated v2 parent, so this code never moves unrelated host processes.
type taskCgroup struct {
	path string
}

// CurrentCgroupRoot returns the cgroup v2 directory of the current process.
// A service launched with systemd Delegate=yes can use this as Config.CgroupRoot.
func CurrentCgroupRoot() (string, error) {
	content, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", fmt.Errorf("read current cgroup: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || parts[0] != "0" || parts[1] != "" {
			continue
		}
		relative := filepath.Clean(parts[2])
		if relative == "." || !strings.HasPrefix(relative, "/") {
			return "", fmt.Errorf("invalid current cgroup path %q", parts[2])
		}
		return filepath.Join("/sys/fs/cgroup", relative), nil
	}
	return "", fmt.Errorf("cgroup v2 path not found in /proc/self/cgroup")
}

// PrepareCgroupRoot turns a delegated service cgroup into a task parent. A
// service initially contains the engine process, while a cgroup with enabled
// domain controllers must not contain ordinary processes. Move the engine into
// its own leaf first, then enable memory and pids for sibling task cgroups.
// This function never operates on the host root cgroup.
func PrepareCgroupRoot(root string) error {
	root = filepath.Clean(root)
	if root == "/sys/fs/cgroup" {
		return fmt.Errorf("refuse host cgroup root; run under a delegated service or set -cgroup-root")
	}
	current, err := CurrentCgroupRoot()
	if err != nil {
		return err
	}
	if filepath.Clean(current) == root {
		leaf := filepath.Join(root, fmt.Sprintf("engine-%d", os.Getpid()))
		if err := os.Mkdir(leaf, 0755); err != nil && !os.IsExist(err) {
			return fmt.Errorf("create engine cgroup: %w", err)
		}
		if err := os.WriteFile(filepath.Join(leaf, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			return fmt.Errorf("move engine into leaf cgroup: %w", err)
		}
	}
	controllers, err := os.ReadFile(filepath.Join(root, "cgroup.controllers"))
	if err != nil {
		return fmt.Errorf("read delegated controllers: %w", err)
	}
	available := map[string]bool{}
	for _, controller := range strings.Fields(string(controllers)) {
		available[controller] = true
	}
	for _, controller := range []string{"memory", "pids"} {
		if !available[controller] {
			return fmt.Errorf("delegated cgroup lacks %s controller", controller)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte("+memory +pids"), 0644); err != nil {
		return fmt.Errorf("enable delegated controllers: %w", err)
	}
	return nil
}

func createTaskCgroup(root string, limit Limit) (*taskCgroup, error) {
	name := fmt.Sprintf("justcpc-%d-%d", os.Getpid(), time.Now().UnixNano())
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0755); err != nil {
		return nil, fmt.Errorf("create cgroup under %s: %w", root, err)
	}
	cg := &taskCgroup{path: path}
	if err := cg.writeUint("memory.max", limit.Memory); err != nil {
		cg.destroy()
		return nil, fmt.Errorf("set memory.max: %w", err)
	}
	// A contest memory limit must not be bypassed by moving anonymous pages to
	// swap. memory.swap.max exists on normal cgroup v2 Linux kernels; older WSL
	// kernels omit it and are rejected by the integration test instead.
	if _, err := os.Stat(filepath.Join(path, "memory.swap.max")); err == nil {
		if err := cg.writeUint("memory.swap.max", 0); err != nil {
			cg.destroy()
			return nil, fmt.Errorf("set memory.swap.max: %w", err)
		}
	} else if !os.IsNotExist(err) {
		cg.destroy()
		return nil, fmt.Errorf("check memory.swap.max: %w", err)
	}
	if err := cg.writeUint("pids.max", limit.Processes); err != nil {
		cg.destroy()
		return nil, fmt.Errorf("set pids.max: %w", err)
	}
	// An OOM must kill the whole task, rather than leaving its container init alive.
	if err := os.WriteFile(filepath.Join(path, "memory.oom.group"), []byte("1"), 0644); err != nil {
		cg.destroy()
		return nil, fmt.Errorf("set memory.oom.group: %w", err)
	}
	return cg, nil
}

func (c *taskCgroup) addProcess(pid int) error {
	return os.WriteFile(filepath.Join(c.path, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0644)
}

func (c *taskCgroup) usage() (time.Duration, uint64, uint64, error) {
	cpu, err := c.cpuTime()
	if err != nil {
		return 0, 0, 0, err
	}
	memory, err := c.readUint("memory.peak")
	if err != nil {
		return 0, 0, 0, err
	}
	processes, err := c.readUint("pids.peak")
	if err != nil {
		return 0, 0, 0, err
	}
	return cpu, memory, processes, nil
}

func (c *taskCgroup) currentCPU() (time.Duration, error) {
	return c.cpuTime()
}

func (c *taskCgroup) oomKilled() (bool, error) {
	f, err := os.Open(filepath.Join(c.path, "memory.events"))
	if err != nil {
		return false, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) == 2 && fields[0] == "oom_kill" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			return value > 0, err
		}
	}
	return false, s.Err()
}

func (c *taskCgroup) cpuTime() (time.Duration, error) {
	f, err := os.Open(filepath.Join(c.path, "cpu.stat"))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) == 2 && fields[0] == "usage_usec" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			return time.Duration(value) * time.Microsecond, err
		}
	}
	return 0, fmt.Errorf("cpu.stat does not contain usage_usec")
}

func (c *taskCgroup) readUint(name string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(c.path, name))
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}

func (c *taskCgroup) writeUint(name string, value uint64) error {
	return os.WriteFile(filepath.Join(c.path, name), []byte(strconv.FormatUint(value, 10)), 0644)
}

func (c *taskCgroup) destroy() {
	if c != nil {
		_ = os.Remove(c.path)
	}
}
