# JustCPC 判题引擎（Judge Engine）

面向**赛后统一评测**的 Linux 判题服务。接收「源码 + 语言 + 测试点 + 资源限制」，完成**编译 → 逐测试点执行 → 输出比对**，返回 `AC / WA / TLE / MLE / CE / RE / SYS`。

- 硬隔离：user/mount/pid/net/uts/ipc/cgroup 命名空间 + 只读 RootFS + cgroup v2 限流 + seccomp 白名单。
- 同步接口：`POST /api/execute`，一次请求返回编译信息与逐点结果。
- 无任务队列：异步、重试、结果持久化由 Online Judge 的 dispatcher 负责；本引擎只限制并发执行数。
- **仅支持 Linux**，需要 cgroup v2 与可委派的 cgroup 子树。

## 运行环境

- Linux（amd64），内核支持 cgroup v2。
- 通过 systemd 服务运行时需 `Delegate=yes`（见 `script/register-service.sh`）；本地调试用 `script/run-local.sh`。
- 需要一个**判题 RootFS**（含 g++ / JDK / python3 等工具链）；这是部署方本地准备的运行时资源，不入库。

## 构建与运行

```bash
# 构建
go build -o bin/justcpc-judge-engine ./src

# 本地调试（自动委托 cgroup，必要时 sudo）
./script/run-local.sh

# 注册为 systemd 服务（需 root）
sudo ./script/register-service.sh --run-user <账号>
```

## 配置（config.json）

| 字段 | 说明 |
|---|---|
| `listen` | 监听地址，默认 `127.0.0.1:18080` |
| `rootfs` | 判题 RootFS 路径，默认 `runtime/judge-rootfs` |
| `problem_root` | 文件系统题库根目录，默认 `testdata`（仅本地文件仓库使用） |
| `work_root` | 沙盒工作目录，默认 `/tmp/justcpc-judge-engine` |
| `cgroup_root` | 委派的 cgroup v2 目录；留空则自动发现当前进程 cgroup |
| `workers` | 最大并发执行数，默认 10 |

配置路径可用环境变量 `JUSTCPC_ENGINE_CONFIG` 指定；`config.json` 不入库，参考 `config.example.json`。

## 接口

`POST /api/execute`（同步）

请求体（`JudgeTask`）：

```json
{
  "problem_id": "18/A",
  "language": "cpp",
  "source": "int main(){}",
  "output_dir": "/abs/path/to/output",     // 可选：逐点 stdout 落盘目录
  "problem": { "id": "18/A", "limits": {}, "cases": [ ... ] }  // 可选：直接内联题目元数据
}
```

响应体（`Response`）：总 `verdict` + `compile` + 每个测试点的 `cases`。`problem` 省略时按 `problem_id` 从配置的题库加载。

## 本地文件题库布局（可选）

引擎自带一个文件系统题库实现，供本地联调使用：

```
<problem_root>/contest-<year>/cases/<题目>/<n>.in
<problem_root>/contest-<year>/cases/<题目>/<n>.out
```

## 目录说明（哪些是本地资源、不入库）

| 路径 | 内容 | 是否入库 |
|---|---|---|
| `src/` | 引擎本体代码 | ✅ |
| `script/` | 构建/运行/注册服务脚本 | ✅ |
| `test/` | 单元与集成测试代码 | ✅ |
| `config.example.json`、`go.mod`、`go.sum`、`README.md` | 配置样例与元数据 | ✅ |
| `runtime/judge-rootfs` | 判题 RootFS | ❌ 本地准备 |
| `testdata/` | 本地测试题库 | ❌ 仅本地测试 |
| `bin/`、`work/`、`release/`、`test/results/`、`config.json` | 构建/运行产物 | ❌ |
