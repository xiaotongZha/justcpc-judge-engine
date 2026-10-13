package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const initFailureExitCode = 253

func init() {
	if len(os.Args) == 2 && os.Args[1] == initArgument {
		if err := runInit(); err != nil {
			fmt.Fprintf(os.Stderr, "justcpc sandbox init: %v\n", err)
			os.Exit(initFailureExitCode)
		}
		os.Exit(0)
	}
}

func runInit() error {
	configFile := os.NewFile(uintptr(3), "sandbox-config")
	if configFile == nil {
		return fmt.Errorf("missing configuration fd")
	}
	defer configFile.Close()
	var config initConfig
	if err := json.NewDecoder(configFile).Decode(&config); err != nil {
		return fmt.Errorf("decode configuration: %w", err)
	}
	if err := initializeFileSystem(config); err != nil {
		return err
	}
	startFile := os.NewFile(uintptr(4), "sandbox-start")
	if startFile == nil {
		return fmt.Errorf("missing start fd")
	}
	defer startFile.Close()
	var marker [1]byte
	if _, err := startFile.Read(marker[:]); err != nil {
		return fmt.Errorf("wait for cgroup setup: %w", err)
	}
	if err := applyRlimits(config.Limit); err != nil {
		return fmt.Errorf("apply rlimits: %w", err)
	}
	if err := restoreExecSignals(); err != nil {
		return fmt.Errorf("restore exec signals: %w", err)
	}
	if err := dropPrivileges(); err != nil {
		return fmt.Errorf("drop capabilities: %w", err)
	}
	if err := applySeccomp(config.Profile); err != nil {
		return fmt.Errorf("apply seccomp: %w", err)
	}
	path, err := resolveExecutable(config.Args[0])
	if err != nil {
		return err
	}
	env := []string{"PATH=/usr/bin:/bin", "HOME=/w", "TMPDIR=/tmp", "LANG=C.UTF-8"}
	env = append(env, config.Env...)
	return syscall.Exec(path, config.Args, env)
}

// The Go runtime installs handlers of its own. The executed program must not
// inherit those handlers, otherwise RLIMIT_CPU and RLIMIT_FSIZE can turn into
// ignored signals instead of the kernel termination the result mapper expects.
// os/signal.Reset is not enough here: a Go runtime may intentionally preserve
// an ignored disposition across exec. This is the kernel ABI on Linux amd64.
type kernelSigaction struct {
	handler  uintptr
	flags    uint64
	restorer uintptr
	mask     uint64
}

func restoreExecSignals() error {
	for _, sig := range []syscall.Signal{syscall.SIGXCPU, syscall.SIGXFSZ} {
		action := kernelSigaction{}
		_, _, errno := unix.RawSyscall6(
			unix.SYS_RT_SIGACTION,
			uintptr(sig),
			uintptr(unsafe.Pointer(&action)),
			0,
			8,
			0,
			0,
		)
		if errno != 0 {
			return errno
		}
	}
	return nil
}

func initializeFileSystem(config initConfig) error {
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make host mounts private: %w", err)
	}
	if err := unix.Mount("tmpfs", config.SandboxRoot, "tmpfs", 0, "mode=0755,size=64m"); err != nil {
		return fmt.Errorf("mount container root: %w", err)
	}
	entries, err := os.ReadDir(config.RootFS)
	if err != nil {
		return fmt.Errorf("read RootFS: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "dev" || name == "proc" || name == "sys" || name == "tmp" || name == "w" {
			continue
		}
		source := filepath.Join(config.RootFS, name)
		target := filepath.Join(config.SandboxRoot, name)
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat RootFS entry %s: %w", name, err)
		}
		switch {
		case info.IsDir():
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			if err := unix.Mount(source, target, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
				return fmt.Errorf("bind RootFS entry %s: %w", name, err)
			}
			if err := remountReadOnly(target); err != nil {
				return fmt.Errorf("remount RootFS entry %s readonly: %w", name, err)
			}
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(source)
			if err != nil {
				return err
			}
			if err := os.Symlink(link, target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported RootFS top-level entry %s", name)
		}
	}
	if err := os.MkdirAll(filepath.Join(config.SandboxRoot, "w"), 0755); err != nil {
		return err
	}
	if err := unix.Mount(config.WorkDir, filepath.Join(config.SandboxRoot, "w"), "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind work directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(config.SandboxRoot, "tmp"), 01777); err != nil {
		return err
	}
	if err := unix.Mount("tmpfs", filepath.Join(config.SandboxRoot, "tmp"), "tmpfs", 0, "mode=1777,size=64m"); err != nil {
		return fmt.Errorf("mount /tmp: %w", err)
	}
	if err := mountProc(config.SandboxRoot); err != nil {
		return err
	}
	if err := mountDevices(config.SandboxRoot); err != nil {
		return err
	}
	oldRoot := filepath.Join(config.SandboxRoot, "old_root")
	if err := os.Mkdir(oldRoot, 0755); err != nil {
		return fmt.Errorf("make old root: %w", err)
	}
	if err := unix.PivotRoot(config.SandboxRoot, oldRoot); err != nil {
		return fmt.Errorf("pivot root: %w", err)
	}
	if err := os.Chdir("/"); err != nil {
		return err
	}
	if err := unix.Unmount("/old_root", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("detach old root: %w", err)
	}
	if err := os.Remove("/old_root"); err != nil {
		return err
	}
	if err := remountReadOnly("/"); err != nil {
		return fmt.Errorf("remount container root readonly: %w", err)
	}
	if err := os.Chdir("/w"); err != nil {
		return fmt.Errorf("enter work directory: %w", err)
	}
	return nil
}

func remountReadOnly(path string) error {
	return unix.Mount("", path, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV, "")
}

func mountProc(root string) error {
	target := filepath.Join(root, "proc")
	if err := os.MkdirAll(target, 0555); err != nil {
		return err
	}
	if err := unix.Mount("proc", target, "proc", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); err != nil {
		return fmt.Errorf("mount proc: %w", err)
	}
	return nil
}

func mountDevices(root string) error {
	dev := filepath.Join(root, "dev")
	if err := os.MkdirAll(dev, 0755); err != nil {
		return err
	}
	for _, name := range []string{"null", "zero", "random", "urandom"} {
		target := filepath.Join(dev, name)
		file, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		file.Close()
		if err := unix.Mount(filepath.Join("/dev", name), target, "", unix.MS_BIND, ""); err != nil {
			return fmt.Errorf("bind /dev/%s: %w", name, err)
		}
	}
	for name, target := range map[string]string{
		"fd":     "/proc/self/fd",
		"stdin":  "/proc/self/fd/0",
		"stdout": "/proc/self/fd/1",
		"stderr": "/proc/self/fd/2",
	} {
		if err := os.Symlink(target, filepath.Join(dev, name)); err != nil {
			return err
		}
	}
	return nil
}

func dropPrivileges() error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	// Capability API version 3 requires two 32-bit capability words. Passing
	// one struct leaves the second word undefined and can accidentally request
	// a capability instead of dropping it.
	data := [2]unix.CapUserData{}
	return unix.Capset(&header, &data[0])
}

func resolveExecutable(command string) (string, error) {
	if strings.ContainsRune(command, '/') {
		return command, nil
	}
	for _, dir := range []string{"/usr/bin", "/bin"} {
		candidate := filepath.Join(dir, command)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("executable %q not found in RootFS", command)
}
