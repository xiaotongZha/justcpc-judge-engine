// Package capacity chooses a fixed, conservative execution capacity at startup.
package capacity

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const jobMemoryBytes uint64 = 1 << 30

type Resources struct {
	CPUs           float64
	AvailableBytes uint64
	Warnings       []string
}

type Selection struct {
	Workers           int      `json:"workers"`
	Mode              string   `json:"mode"`
	AvailableCPUs     float64  `json:"available_cpus"`
	AvailableMemoryMB uint64   `json:"available_memory_mb"`
	MemoryBudgetMB    uint64   `json:"memory_budget_mb"`
	MemoryLimited     bool     `json:"memory_limited"`
	Warnings          []string `json:"warnings,omitempty"`
}

// Select reserves half the available CPU and memory for the host and services.
// A slot is estimated at 1 GiB; admission also considers each request's limits.
// Incomplete detection falls back to one slot rather than guessing host capacity.
func Select(workers int, resources Resources) (Selection, error) {
	if workers < 0 {
		return Selection{}, fmt.Errorf("workers must be non-negative (0 means automatic)")
	}
	selection := Selection{
		Mode: "auto", AvailableCPUs: resources.CPUs,
		AvailableMemoryMB: resources.AvailableBytes >> 20,
		MemoryBudgetMB:    resources.AvailableBytes / 2 >> 20,
		Warnings:          resources.Warnings,
	}
	if workers > 0 {
		selection.Workers, selection.Mode = workers, "manual"
		return selection, nil
	}
	selection.Workers = 1
	if len(resources.Warnings) != 0 || resources.CPUs <= 0 || resources.AvailableBytes == 0 {
		return selection, nil
	}
	cpuSlots := max(1, int(math.Floor(resources.CPUs/2)))
	memorySlots := max(1, int(resources.AvailableBytes/2/jobMemoryBytes))
	selection.Workers = min(cpuSlots, memorySlots)
	selection.MemoryLimited = memorySlots < cpuSlots
	return selection, nil
}

func Detect() Resources {
	var affinity unix.CPUSet
	if err := unix.SchedGetaffinity(0, &affinity); err != nil {
		return Resources{CPUs: float64(runtime.NumCPU()), Warnings: []string{fmt.Sprintf("read CPU affinity: %v; automatic capacity falls back to 1", err)}}
	}
	return detect("/proc", affinity.Count())
}

func detect(proc string, affinityCPUs int) Resources {
	resources := Resources{CPUs: float64(affinityCPUs)}
	warn := func(err error) { resources.Warnings = append(resources.Warnings, err.Error()) }
	available, err := memAvailable(filepath.Join(proc, "meminfo"))
	if err != nil {
		warn(err)
	} else {
		resources.AvailableBytes = available
	}
	root, current, err := cgroupPaths(proc)
	if err != nil {
		warn(err)
		return resources
	}
	for path := current; ; path = filepath.Dir(path) {
		data, err := os.ReadFile(filepath.Join(path, "cpu.max"))
		if err == nil {
			cpus, err := cpuQuota(string(data))
			if err != nil {
				warn(fmt.Errorf("read %s/cpu.max: %w", path, err))
			} else if cpus > 0 {
				resources.CPUs = min(resources.CPUs, cpus)
			}
		} else if !os.IsNotExist(err) {
			warn(err)
		}
		data, err = os.ReadFile(filepath.Join(path, "memory.max"))
		if err == nil && strings.TrimSpace(string(data)) != "max" {
			limit, parseErr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
			used, usedErr := readUint(filepath.Join(path, "memory.current"))
			if parseErr != nil || usedErr != nil {
				warn(fmt.Errorf("read cgroup memory at %s: limit=%v usage=%v", path, parseErr, usedErr))
			} else {
				remaining := uint64(0)
				if used < limit {
					remaining = limit - used
				}
				resources.AvailableBytes = min(resources.AvailableBytes, remaining)
			}
		} else if err != nil && !os.IsNotExist(err) {
			warn(err)
		}
		if path == root {
			break
		}
	}
	return resources
}

func cpuQuota(value string) (float64, error) {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return 0, fmt.Errorf("expected quota and period")
	}
	period, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || period == 0 {
		return 0, fmt.Errorf("invalid CPU period %q", fields[1])
	}
	if fields[0] == "max" {
		return 0, nil
	}
	quota, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || quota == 0 {
		return 0, fmt.Errorf("invalid CPU quota %q", fields[0])
	}
	return float64(quota) / float64(period), nil
}

func memAvailable(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 3 && fields[0] == "MemAvailable:" && fields[2] == "kB" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil || value > math.MaxUint64/1024 {
				return 0, fmt.Errorf("invalid MemAvailable")
			}
			return value * 1024, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("MemAvailable not found in %s", path)
}

func readUint(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

func cgroupPaths(proc string) (string, string, error) {
	groups, err := os.ReadFile(filepath.Join(proc, "self/cgroup"))
	if err != nil {
		return "", "", err
	}
	group := ""
	for _, line := range strings.Split(string(groups), "\n") {
		if strings.HasPrefix(line, "0::/") {
			group = filepath.Clean(strings.TrimPrefix(line, "0::"))
			break
		}
	}
	if group == "" {
		return "", "", fmt.Errorf("cgroup v2 membership not found")
	}
	mounts, err := os.ReadFile(filepath.Join(proc, "self/mountinfo"))
	if err != nil {
		return "", "", err
	}
	for _, line := range strings.Split(string(mounts), "\n") {
		before, after, ok := strings.Cut(line, " - ")
		if !ok || !strings.HasPrefix(after, "cgroup2 ") {
			continue
		}
		fields := strings.Fields(before)
		if len(fields) < 5 {
			continue
		}
		unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
		mountRoot, mountPoint := unescape.Replace(fields[3]), unescape.Replace(fields[4])
		relative, err := filepath.Rel(mountRoot, group)
		// A cgroup namespace may report membership at its own root while the
		// mount still names the corresponding host subtree.
		if group == "/" {
			relative, err = ".", nil
		}
		if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
			continue
		}
		current := filepath.Join(mountPoint, relative)
		if _, err := os.Stat(current); err != nil {
			continue
		}
		return filepath.Clean(mountPoint), current, nil
	}
	return "", "", fmt.Errorf("cgroup v2 mount for %s not found", group)
}
