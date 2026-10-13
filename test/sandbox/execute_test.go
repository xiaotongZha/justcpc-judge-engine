package sandbox_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"justcpc-judge-engine/src/sandbox"
)

func TestRunNormalProgram(t *testing.T) {
	s := newTestSandbox(t)
	result := runFixture(t, s, "normal", sandbox.Limit{Time: time.Second, WallTime: 2 * time.Second})
	if result.Status != sandbox.StatusNormal {
		t.Fatalf("status = %s, error = %s, stderr = %q", result.Status, result.Error, result.Stderr)
	}
	if string(result.Stdout) != "sandbox-ok\n" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if result.CPUTime <= 0 || result.WallTime <= 0 {
		t.Fatalf("invalid usage: CPU = %s, wall = %s", result.CPUTime, result.WallTime)
	}
}

func TestRunWritesStdoutToPath(t *testing.T) {
	s := newTestSandbox(t)
	binary := buildFixture(t, "normal")
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "submission", "1.out")
	result := s.Run(context.Background(), sandbox.Task{
		Args:       []string{"/w/program"},
		Files:      []sandbox.InputFile{{Path: "program", Data: content, Mode: 0755}},
		StdoutPath: outputPath,
		Limit:      sandbox.Limit{Time: time.Second, WallTime: 2 * time.Second},
		Profile:    sandbox.ProfileRun,
	})
	if result.Status != sandbox.StatusNormal {
		t.Fatalf("status = %s, error = %s, stderr = %q", result.Status, result.Error, result.Stderr)
	}
	if len(result.Stdout) != 0 {
		t.Fatalf("stdout was retained in memory: %q", result.Stdout)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "sandbox-ok\n" {
		t.Fatalf("output file = %q", output)
	}
}

func TestRunTimesOut(t *testing.T) {
	s := newCgroupTestSandbox(t)
	result := runFixture(t, s, "busy", sandbox.Limit{Time: time.Second, WallTime: 2 * time.Second})
	if result.Status != sandbox.StatusTimeLimitExceeded {
		t.Fatalf("status = %s, error = %s, stderr = %q", result.Status, result.Error, result.Stderr)
	}
	if result.WallTime < time.Second {
		t.Fatalf("wall time = %s, expected the program to run before cancellation", result.WallTime)
	}
}

func newCgroupTestSandbox(t *testing.T) *sandbox.Sandbox {
	t.Helper()
	root := delegatedCgroupRoot(t)
	if root == "" {
		t.Skip("the current host does not provide a writable cgroup v2 subtree")
	}
	s, err := sandbox.New(sandbox.Config{
		RootFS:     projectPath(t, "runtime", "judge-rootfs"),
		WorkRoot:   t.TempDir(),
		CgroupRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunCannotReadHostPath(t *testing.T) {
	s := newTestSandbox(t)
	result := runFixture(t, s, "read_host", sandbox.Limit{Time: time.Second, WallTime: 2 * time.Second})
	if result.Status != sandbox.StatusNormal {
		t.Fatalf("status = %s, error = %s, stderr = %q", result.Status, result.Error, result.Stderr)
	}
	if string(result.Stdout) != "host file is not visible\n" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func TestRunRejectsForbiddenSyscall(t *testing.T) {
	s := newTestSandbox(t)
	result := runFixture(t, s, "forbidden", sandbox.Limit{Time: time.Second, WallTime: 2 * time.Second})
	if result.Status != sandbox.StatusForbiddenSyscall {
		t.Fatalf("status = %s, signal = %d, error = %s, stderr = %q", result.Status, result.Signal, result.Error, result.Stderr)
	}
}

func TestRunStopsAtOutputLimit(t *testing.T) {
	s := newTestSandbox(t)
	result := runFixture(t, s, "output", sandbox.Limit{
		Time:       time.Second,
		WallTime:   2 * time.Second,
		OutputSize: 4 << 10,
	})
	if result.Status != sandbox.StatusOutputLimitExceeded {
		t.Fatalf("status = %s, signal = %d, output = %d bytes, error = %s", result.Status, result.Signal, len(result.Stdout), result.Error)
	}
	if len(result.Stdout) > 4<<10 {
		t.Fatalf("collected %d bytes with a 4096 byte limit", len(result.Stdout))
	}
}

func TestRunStopsAtOutputLimitWithStdoutPath(t *testing.T) {
	s := newTestSandbox(t)
	binary := buildFixture(t, "output")
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "output.out")
	result := s.Run(context.Background(), sandbox.Task{
		Args:       []string{"/w/program"},
		Files:      []sandbox.InputFile{{Path: "program", Data: content, Mode: 0755}},
		StdoutPath: outputPath,
		Limit: sandbox.Limit{
			Time:       time.Second,
			WallTime:   2 * time.Second,
			OutputSize: 4 << 10,
		},
		Profile: sandbox.ProfileRun,
	})
	if result.Status != sandbox.StatusOutputLimitExceeded {
		t.Fatalf("status = %s, signal = %d, error = %s", result.Status, result.Signal, result.Error)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 4<<10 {
		t.Fatalf("output file is %d bytes with a 4096-byte limit", info.Size())
	}
}

func TestRunMemoryLimitWithCgroup(t *testing.T) {
	root := delegatedCgroupRoot(t)
	if root == "" {
		t.Skip("the current host does not delegate a writable cgroup v2 subtree")
	}
	if cgroupSwapIsUnbounded(root) {
		t.Skip("the current host has active swap but no cgroup memory.swap.max; a 256 MiB contest memory limit cannot be verified")
	}
	rootFS := projectPath(t, "runtime", "judge-rootfs")
	s, err := sandbox.New(sandbox.Config{
		RootFS:     rootFS,
		WorkRoot:   t.TempDir(),
		CgroupRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := runFixture(t, s, "memory", sandbox.Limit{
		Time:       5 * time.Second,
		WallTime:   8 * time.Second,
		Memory:     256 << 20,
		Processes:  32,
		OutputSize: 1 << 20,
	})
	if result.Status != sandbox.StatusMemoryLimitExceeded {
		t.Fatalf("status = %s, memory = %d, error = %s, stderr = %q", result.Status, result.Memory, result.Error, result.Stderr)
	}
}

func newTestSandbox(t *testing.T) *sandbox.Sandbox {
	t.Helper()
	s, err := sandbox.New(sandbox.Config{
		RootFS:               projectPath(t, "runtime", "judge-rootfs"),
		WorkRoot:             t.TempDir(),
		AllowNoCgroupForTest: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func runFixture(t *testing.T, s *sandbox.Sandbox, name string, limit sandbox.Limit) sandbox.Result {
	t.Helper()
	binary := buildFixture(t, name)
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	return s.Run(context.Background(), sandbox.Task{
		Args:    []string{"/w/program"},
		Files:   []sandbox.InputFile{{Path: "program", Data: content, Mode: 0755}},
		Limit:   limit,
		Profile: sandbox.ProfileRun,
	})
}

func buildFixture(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOARCH != "amd64" {
		t.Skip("fixture RootFS is amd64")
	}
	output := filepath.Join(t.TempDir(), name)
	fixtureDir := "./test/sandbox/programs/" + name
	var command *exec.Cmd
	if _, err := os.Stat(filepath.Join(projectPath(t), fixtureDir, "main.cpp")); err == nil {
		command = exec.Command("g++", "-static", "-O2", "-std=c++17", "-o", output, filepath.Join(fixtureDir, "main.cpp"))
	} else if _, err := os.Stat(filepath.Join(projectPath(t), fixtureDir, "main.c")); err == nil {
		command = exec.Command("gcc", "-static", "-O2", "-o", output, filepath.Join(fixtureDir, "main.c"))
	} else {
		command = exec.Command("go", "build", "-o", output, fixtureDir)
	}
	command.Dir = projectPath(t)
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fixture %s: %v\n%s", name, err, data)
	}
	return output
}

func delegatedCgroupRoot(t *testing.T) string {
	t.Helper()
	root, err := sandbox.CurrentCgroupRoot()
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(root, "justcpc-test-probe")
	if err := os.Mkdir(probe, 0755); err != nil {
		return ""
	}
	if err := os.Remove(probe); err != nil {
		t.Fatalf("remove cgroup probe: %v", err)
	}
	return root
}

func cgroupSwapIsUnbounded(root string) bool {
	if _, err := os.Stat(filepath.Join(root, "memory.swap.max")); err == nil {
		return false
	}
	data, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return false
	}
	return len(strings.Fields(strings.TrimSpace(string(data)))) > 5
}

func projectPath(t *testing.T, elements ...string) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(append([]string{root}, elements...)...)
}
