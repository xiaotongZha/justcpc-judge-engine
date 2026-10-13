package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"justcpc-judge-engine/src/judge"
	"justcpc-judge-engine/src/sandbox"
)

// config 汇集引擎启动配置，全部由配置文件提供，不使用命令行参数。
// cgroup_root 为空时表示自动发现当前委托的 cgroup v2 目录。
type config struct {
	Listen      string `json:"listen"`
	RootFS      string `json:"rootfs"`
	ProblemRoot string `json:"problem_root"`
	WorkRoot    string `json:"work_root"`
	CgroupRoot  string `json:"cgroup_root"`
	Workers     int    `json:"workers"`
}

func defaultConfig() config {
	return config{
		Listen:      "127.0.0.1:18080",
		RootFS:      "runtime/judge-rootfs",
		ProblemRoot: "testdata",
		WorkRoot:    "/tmp/justcpc-judge-engine",
		CgroupRoot:  "",
		Workers:     10,
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
	return cfg, nil
}

func main() {
	configPath := os.Getenv("JUSTCPC_ENGINE_CONFIG")
	if configPath == "" {
		configPath = "config.json"
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("load configuration %s: %v", configPath, err)
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 10
	}
	cgroupRoot := cfg.CgroupRoot
	if cgroupRoot == "" {
		cgroupRoot, err = sandbox.CurrentCgroupRoot()
		if err != nil {
			log.Fatalf("discover cgroup root: %v", err)
		}
	}
	if err := sandbox.PrepareCgroupRoot(cgroupRoot); err != nil {
		log.Fatalf("prepare delegated cgroup: %v", err)
	}
	runner, err := sandbox.New(sandbox.Config{
		RootFS:     cfg.RootFS,
		WorkRoot:   cfg.WorkRoot,
		CgroupRoot: cgroupRoot,
	})
	if err != nil {
		log.Fatalf("configure sandbox: %v", err)
	}
	repository, err := judge.NewFileSystemRepository(judge.FileSystemRepositoryConfig{
		Root:         cfg.ProblemRoot,
		DefaultLimit: defaultLimits(),
	})
	if err != nil {
		log.Fatalf("configure problem repository: %v", err)
	}
	handler, err := judge.NewHTTPHandler(judge.NewEngine(runner, repository, nil, nil), cfg.Workers)
	if err != nil {
		log.Fatalf("configure judge HTTP handler: %v", err)
	}
	server := &http.Server{Addr: cfg.Listen, Handler: handler}
	log.Printf("judge engine listening on %s (config %s)", cfg.Listen, configPath)
	log.Fatal(server.ListenAndServe())
}

func defaultLimits() map[judge.Language]judge.ResourceLimit {
	return map[judge.Language]judge.ResourceLimit{
		judge.LanguageCPP:     {TimeLimitMS: 1000, MemoryLimitMB: 256},
		judge.LanguageJava:    {TimeLimitMS: 1000, MemoryLimitMB: 256},
		judge.LanguagePython3: {TimeLimitMS: 2000, MemoryLimitMB: 512},
	}
}
