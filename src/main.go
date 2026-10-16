package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"justcpc-judge-engine/src/capacity"
	"justcpc-judge-engine/src/judge"
	"justcpc-judge-engine/src/sandbox"
)

// config 汇集引擎启动配置，全部由配置文件提供；命令行只提供环境自检。
// cgroup_root 为空时表示自动发现当前委托的 cgroup v2 目录。
type config struct {
	Listen     string `json:"listen"`
	RootFS     string `json:"rootfs"`
	WorkRoot   string `json:"work_root"`
	CgroupRoot string `json:"cgroup_root"`
	Workers    int    `json:"workers"`
}

func defaultConfig() config {
	return config{
		Listen:     "127.0.0.1:18080",
		RootFS:     "",
		WorkRoot:   "/tmp/justcpc-judge-engine",
		CgroupRoot: "",
		Workers:    0,
	}
}

// loadConfig 从 path 读取 JSON 配置。文件不存在时使用内置默认值；字段缺省时保持
// 默认值。
func loadConfig(path string) (config, error) {
	cfg := defaultConfig()
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Workers < 0 {
		return cfg, errors.New("workers must be non-negative (0 means automatic)")
	}
	return cfg, nil
}

func main() {
	mode := ""
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || (os.Args[1] != "--check" && os.Args[1] != "--check-namespaces") {
			log.Fatal("usage: justcpc-judge-engine [--check | --check-namespaces]; configuration is read from config.json")
		}
		mode = os.Args[1]
	}
	if err := sandbox.CheckNamespaces(); err != nil {
		log.Fatalf("sandbox namespace check failed: %v\nThe kernel or host policy is blocking this executable. On Ubuntu with AppArmor user namespace restrictions, build the engine and run sudo ./script/setup-apparmor.sh before script/run-local.sh. See README: first-time host setup.", err)
	}
	if mode == "--check-namespaces" {
		log.Print("sandbox namespace permissions: OK")
		return
	}
	configPath := os.Getenv("JUSTCPC_ENGINE_CONFIG")
	if configPath == "" {
		configPath = "config.json"
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("load configuration %s: %v", configPath, err)
	}
	if cfg.RootFS == "" {
		if err := checkHostToolchains(); err != nil {
			log.Fatalf("check host runtime: %v", err)
		}
	}
	cgroupRoot := cfg.CgroupRoot
	if cgroupRoot == "" {
		cgroupRoot, err = sandbox.CurrentCgroupRoot()
		if err != nil {
			log.Fatalf("discover cgroup root: %v", err)
		}
	}
	if err := sandbox.PrepareCgroupRoot(cgroupRoot); err != nil {
		log.Fatalf("prepare delegated cgroup: %v; use script/run-local.sh or a systemd service with Delegate=yes", err)
	}
	runner, err := sandbox.New(sandbox.Config{
		RootFS:     cfg.RootFS,
		WorkRoot:   cfg.WorkRoot,
		CgroupRoot: cgroupRoot,
	})
	if err != nil {
		log.Fatalf("configure sandbox: %v", err)
	}
	// Verify filesystem isolation, execution and cleanup before accepting work.
	// --check follows this same startup path but never opens the HTTP listener.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	result := runner.Run(ctx, sandbox.Task{Args: []string{"/bin/true"}})
	cancel()
	if result.Status != sandbox.StatusNormal || result.ExitCode != 0 {
		log.Fatalf("sandbox startup check failed: status=%s exit=%d error=%s stderr=%s\nOn Ubuntu with AppArmor user namespace restrictions, run sudo ./script/setup-apparmor.sh, then retry script/run-local.sh --check. For other runtime or host-policy failures, see README: startup troubleshooting.", result.Status, result.ExitCode, result.Error, result.Stderr)
	}
	if result.MemoryEstimated || result.ProcessPeakEstimated {
		log.Printf("cgroup compatibility mode: memory_estimated=%t process_peak_estimated=%t; missing kernel peak counters use 20ms samples (memory also uses maximum RSS); brief peaks may be missed; CPU accounting and cgroup hard limits remain enabled", result.MemoryEstimated, result.ProcessPeakEstimated)
	}
	selection, err := capacity.Select(cfg.Workers, capacity.Detect())
	if err != nil {
		log.Fatalf("select execution capacity: %v", err)
	}
	if mode == "--check" {
		log.Printf("startup checks passed: namespaces, toolchains, delegated cgroup and sandbox execution; mode=%s workers=%d; HTTP listener was not started", selection.Mode, selection.Workers)
		return
	}
	handler, err := judge.NewHTTPHandlerWithCapacity(judge.NewEngine(runner, nil, nil), selection)
	if err != nil {
		log.Fatalf("configure judge HTTP handler: %v", err)
	}
	server := &http.Server{Addr: cfg.Listen, Handler: handler}
	log.Printf("judge engine listening on %s (config %s)", cfg.Listen, configPath)
	log.Printf("execution capacity: mode=%s workers=%d available_cpus=%.2f available_memory_mb=%d memory_budget_mb=%d memory_limited=%t", selection.Mode, selection.Workers, selection.AvailableCPUs, selection.AvailableMemoryMB, selection.MemoryBudgetMB, selection.MemoryLimited)
	for _, warning := range selection.Warnings {
		log.Printf("capacity warning: %s", warning)
	}
	log.Fatal(server.ListenAndServe())
}

func checkHostToolchains() error {
	for _, name := range []string{"g++", "javac", "java", "python3"} {
		path := "/usr/bin/" + name
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("%s is unavailable: %w; install g++, a JDK and python3 (see README)", path, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			return fmt.Errorf("%s is not executable", path)
		}
	}
	return nil
}
