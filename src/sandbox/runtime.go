package sandbox

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// mountRuntime supplies executables and libraries, not a whole host root.
// /w, /tmp, /proc and /dev are constructed separately by initializeFileSystem.
// Keep this list explicit so the sandbox's filesystem is easy to understand.
func mountRuntime(root, rootFS string) error {
	var paths []string
	if rootFS == "" {
		paths = []string{
			"usr", "bin", "lib", "lib64",
			"etc/alternatives", // Debian/Ubuntu java and javac symlinks.
			"etc/ld.so.cache", "etc/ld.so.conf", "etc/ld.so.conf.d",
		}
		// Debian/Ubuntu JDK installations link their configuration into /etc.
		javaConfigs, err := filepath.Glob("/etc/java-*")
		if err != nil {
			return err
		}
		for _, path := range javaConfigs {
			paths = append(paths, path[1:])
		}
		rootFS = "/"
	} else {
		// Retain explicit custom runtimes for existing deployments and fixtures.
		entries, err := os.ReadDir(rootFS)
		if err != nil {
			return fmt.Errorf("read runtime: %w", err)
		}
		for _, entry := range entries {
			switch entry.Name() {
			case "dev", "proc", "sys", "tmp", "w":
				continue
			}
			paths = append(paths, entry.Name())
		}
	}
	for _, path := range paths {
		source := filepath.Join(rootFS, path)
		info, err := os.Lstat(source)
		if os.IsNotExist(err) {
			continue // e.g. a distribution without /lib64 or /etc/alternatives.
		}
		if err != nil {
			return fmt.Errorf("stat runtime path %s: %w", source, err)
		}
		if err := mountRuntimePath(source, filepath.Join(root, path), info); err != nil {
			return fmt.Errorf("mount runtime path %s: %w", source, err)
		}
	}
	return nil
}

func mountRuntimePath(source, target string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		// Preserve merged-/usr layouts such as /bin -> usr/bin.
		link, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(link, target)
	case info.IsDir():
		if err := os.MkdirAll(target, 0755); err != nil {
			return err
		}
	case info.Mode().IsRegular():
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported file type: %s", info.Mode())
	}
	if err := unix.Mount(source, target, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return err
	}
	return remountReadOnly(target)
}
