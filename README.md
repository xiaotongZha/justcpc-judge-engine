# JustCPC 判题引擎（Judge Engine）

面向**赛后统一评测**的 Linux 判题服务。接收「源码 + 语言 + 测试点 + 资源限制」，完成**编译 → 逐测试点执行 → 输出比对**，返回 `AC / WA / TLE / MLE / CE / RE / SYS`。

**发行版构建、部署：[DEPLOY.md](DEPLOY.md)。** 下文为源码开发与实现说明。

- 自行实现沙盒：user/mount/pid/net/uts/ipc/cgroup 命名空间 + 只读运行库挂载 + cgroup v2 资源限制与统计 + seccomp 白名单。
- 同步接口：`POST /api/execute`，一次请求返回编译信息与逐点结果。
- 无任务队列：异步、重试、结果持久化由 Online Judge 的 dispatcher 负责；本引擎只限制并发执行数。
- **仅支持 Linux**，需要 cgroup v2 与可委派的 cgroup 子树。

## 运行环境

- Linux（amd64），内核支持 cgroup v2。
- 通过 systemd 服务运行时需 `Delegate=yes`（见 `script/register-service.sh`）；本地调试用 `script/run-local.sh`。
- 宿主机安装 g++、完整 JDK（包含 javac）和 Python 3，命令位于 `/usr/bin/`。默认直接使用这些工具链，**无需下载或制作 RootFS**。
- 内核和宿主机策略需要允许**引擎可执行文件**创建非特权 user namespace。Ubuntu 24.04 的 AppArmor 限制需要执行下文的首次配置；仅有 cgroup 委托不足以运行沙盒。默认挂载布局针对 Debian/Ubuntu 的系统工具链；其他发行版应核对工具链路径及配置位置。
- OJ 与引擎部署在同一台机器上。OJ 提供 `.in` / `.out` 的绝对路径和输出目录，引擎直接访问这些文件，不通过 HTTP 传输测试数据内容；运行账户需具备相应读写权限。

## 从克隆到运行

以下以 **Ubuntu 24.04、amd64、systemd** 为例。使用普通账号构建和运行，仅安装依赖、注册系统服务时使用 `sudo`。无需 RootFS、Docker，也无需先运行 OJ 后端。

### 1. 安装全局依赖

```bash
sudo apt-get update
sudo apt-get install -y git curl ca-certificates g++ openjdk-21-jdk-headless python3
```

构建需要 **Go 1.26.5 或更新版本**，以仓库 `go.mod` 为准。已有满足版本要求的 Go 时跳过下面的安装步骤。以下命令适用于尚未安装 Go、且 `/usr/local/go` 不存在的机器；升级已有安装请参考 [Go 官方安装说明](https://go.dev/doc/install)，不要向旧目录覆盖解压。

```bash
curl -fL https://go.dev/dl/go1.26.5.linux-amd64.tar.gz -o /tmp/go1.26.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf /tmp/go1.26.5.linux-amd64.tar.gz
sudo ln -s /usr/local/go/bin/go /usr/local/bin/go
sudo ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt
```

确认这些命令均可执行。引擎使用的 g++、javac、java、python3 必须在 `/usr/bin/` 下；只装 JRE 会缺少 javac，需安装上面的 JDK 软件包。

```bash
go version
/usr/bin/g++ --version
/usr/bin/javac -version
/usr/bin/java -version
/usr/bin/python3 --version
stat -fc %T /sys/fs/cgroup
```

最后一条应输出 `cgroup2fs`。cgroup 是 Linux 自带机制，无需另装；下面的启动方式通过 systemd 将一个分组的管理权限交给引擎，使它可以为每个测试点设置资源限制。

### 2. 克隆、配置与构建

```bash
git clone https://github.com/xiaotongZha/justcpc-judge-engine.git
cd justcpc-judge-engine

# 首次配置；已有 config.json 时保留原文件
cp -n config.example.json config.json

# 下载 Go 模块并构建二进制
go mod download
go build -o bin/justcpc-judge-engine ./src
```

默认配置即可启动：`rootfs`、`cgroup_root` 都留空，监听 `127.0.0.1:18080`。每次评测请求都必须提供 `problem`，包含资源限制及测试文件绝对路径；由 OJ 管理题目和测试数据，引擎直接读取指定文件。

### 3. 首次配置宿主机权限

Ubuntu 24.04 可能通过 AppArmor 限制普通程序使用 user namespace。**安装工具链、构建成功，不等于引擎已具备沙盒权限。** 在已构建二进制的项目目录执行一次：

```bash
sudo ./script/setup-apparmor.sh
```

脚本检测 AppArmor 的 user namespace 限制；启用时，为当前引擎二进制的**绝对路径**生成、检查并加载专属规则，保存到 `/etc/apparmor.d/justcpc-judge-engine-<路径摘要>`。未启用该限制时直接提示无需安装。脚本不会启动服务，也不会修改全局内核开关。普通用户可先运行 `./script/setup-apparmor.sh --print` 查看将使用的规则。

**AppArmor 不是引擎的通用依赖，不需要为了运行引擎而安装或启用它。** Ubuntu 通常已经提供 `apparmor_parser`；只有主机已启用上述限制、配置脚本又明确提示缺少 `apparmor_parser` 时，才在该 Ubuntu 主机补装 `sudo apt-get install -y apparmor`。已有工具时无需安装；未启用相关限制的主机无需此工具。

规则采用 Ubuntu 同类工具的 `flags=(unconfined)` 与 `userns,` 形式，作用是允许该引擎创建沙盒命名空间；它不是额外的一套 AppArmor 文件访问白名单。选手隔离仍由引擎内部的 namespace、文件视图、cgroup 和 seccomp 实现。

规则按可执行文件路径匹配：同一路径重新构建不需要重复配置，移动或重新克隆到其他路径后需要再次执行。使用自定义部署目录时可指定 `sudo ./script/setup-apparmor.sh --binary /绝对路径/bin/justcpc-judge-engine`。限制仍被容器或其他宿主机策略阻止时，需由宿主机提供相应权限；脚本不会自动解除这些限制。

### 4. 选择一种启动方式

**本地开发：**在普通用户的登录会话中运行，日志直接显示在终端，按 `Ctrl+C` 停止。

```bash
# 首次运行先完整自检；完成后退出，不监听 HTTP 端口
./script/run-local.sh --check

# 自检通过后正式运行
./script/run-local.sh
```

脚本先使用**当前引擎二进制本身**检查与实际沙盒一致的 namespace 权限；检查失败时退出并给出首次配置命令。检查通过后，使用 `systemd-run --user --scope -p Delegate=yes` 请求临时 cgroup 委托。需要可用的 systemd 用户会话；脚本不自动提权。若提示用户会话不可用，使用下面的系统服务方式。

引擎启动时还会验证工具链、cgroup，并实际运行一次沙盒内的 `/bin/true`，检查挂载、切根、执行及资源清理。`--check` 执行同一条启动检查路径后退出；正常启动只有这些检查通过才监听 HTTP。它验证基础沙盒可用性，不代替真实题目的多语言评测测试。

**部署为系统服务：**在项目目录中执行一次注册。注册脚本默认使用当前项目目录及发起 sudo 的普通账号，并立即启动、设置开机自启。

```bash
sudo ./script/register-service.sh
sudo systemctl status justcpc-judge-engine --no-pager
```

运行账号需能读取项目目录、二进制、配置和测试数据，并能写入 `work_root` 及 OJ 提供的输出目录。注册脚本会先为实际部署路径调用 AppArmor 配置脚本，再注册并启动服务，不安装软件依赖。可用 `--run-user <已有账号>` 指定运行账号，用 `--release-dir <绝对路径>` 指定含 `bin/` 和 `config.json` 的部署目录；该目录路径不能包含空白字符。`--no-start` 仅安装权限规则、注册并设置开机自启，不启动或重启当前服务。

后续管理：

```bash
sudo systemctl start justcpc-judge-engine
sudo systemctl stop justcpc-judge-engine
sudo systemctl restart justcpc-judge-engine
sudo journalctl -u justcpc-judge-engine -f
```

两种方式使用相同监听端口，选择一种运行即可。启动成功时日志包含 `judge engine listening on 127.0.0.1:18080`。

### 5. 发起一次完整评测

保持引擎运行，在**同一台机器的另一个终端**执行以下命令。它创建一组示例输入输出，提交 C++ 两数求和程序，验证编译、沙盒执行和答案比对。

```bash
demo_dir="$(mktemp -d /tmp/justcpc-demo.XXXXXX)"
printf '1 2\n' > "$demo_dir/1.in"
printf '3\n' > "$demo_dir/1.out"

python3 - "$demo_dir" <<'PY' | curl -fsS http://127.0.0.1:18080/api/execute \
    -H 'Content-Type: application/json' --data-binary @- | python3 -m json.tool
import json
import sys

root = sys.argv[1]
print(json.dumps({
    "problem_id": "demo",
    "language": "cpp",
    "source": "#include <iostream>\nint main() { long long a, b; std::cin >> a >> b; std::cout << a + b << '\\n'; }",
    "problem": {
        "id": "demo",
        "limits": {"cpp": {"time_limit_ms": 1000, "memory_limit_mb": 256}},
        "cases": [{"id": "1", "input_path": root + "/1.in", "expected_path": root + "/1.out"}]
    }
}))
PY
```

响应中的总 `verdict`、`compile.verdict` 和 `cases[0].verdict` 都应为 `AC`。这一步不依赖未入库的 `test/` 目录。若服务使用另一个运行账号，请确保该账号可读示例目录和文件；`mktemp -d` 默认只允许当前用户访问。

## 更新代码后运行

在仓库目录执行：

```bash
git pull
go build -o bin/justcpc-judge-engine ./src
```

系统服务部署执行 `sudo systemctl restart justcpc-judge-engine`；本地开发则先停止原进程，再运行 `./script/run-local.sh --check` 和 `./script/run-local.sh`。修改 `config.json` 后同样需要重启。只更新代码或配置时无需重新注册服务；改变部署路径或运行账号时重新注册，注册脚本会处理新路径的 AppArmor 规则。

## 常见启动问题

| 现象 | 处理 |
|---|---|
| 找不到 Go，或版本低于 `go.mod` 要求 | 安装符合要求的 Go，并确认 `go version` 输出及 PATH |
| `javac is unavailable` 等工具链错误 | 安装完整 JDK、g++、python3，检查 `/usr/bin/` 下对应命令 |
| `Local debug binary is missing` | 在项目目录执行上面的 `go build` |
| systemd 用户会话不可用 | 使用系统服务方式注册并启动 |
| `prepare delegated cgroup` 失败 | 确认使用 cgroup v2，通过提供的本地脚本或带 `Delegate=yes` 的系统服务启动；受限容器里的只读 `/sys/fs/cgroup` 需要部署方提供可写委托目录 |
| `sandbox namespace check failed` / `operation not permitted` | Ubuntu 启用 AppArmor user namespace 限制时先执行 `sudo ./script/setup-apparmor.sh`，再执行 `./script/run-local.sh --check`；规则需匹配当前二进制路径。若内核禁用了 user namespace 或外层容器禁止创建命名空间，需要部署方提供权限，单独安装 AppArmor 规则不能解决 |
| `sandbox startup check failed`，例如 `make host mounts private: permission denied` | 创建 namespace 成功也可能被 AppArmor 限制其内部的挂载权限。Ubuntu 上先执行 `sudo ./script/setup-apparmor.sh` 再重试；若仍失败，按错误中的具体步骤检查宿主机策略、自备 RootFS 或工具链。服务尚未监听端口 |
| `address already in use` | 停止占用端口的旧实例，或修改 `config.json` 的 `listen` 后重启 |
| 评测返回 `SYS` 且提示测试文件不可读 | 检查请求中的绝对路径，以及引擎运行账号的文件和父目录权限 |

普通终端直接 `go run ./src` 不会自动获得 cgroup 管理权限，推荐使用上述启动方式。若已自行准备独立、可写且已委托的 cgroup 父目录，可填写 `cgroup_root` 后直接运行。不要把含有 `go run` 父进程的当前 cgroup 当作任务父目录。

工具链版本由部署机器决定。固定系统发行版及 g++ / JDK / Python 版本，评测期间不要升级；OJ 与引擎共享同一套测试文件路径即可。

## 沙盒如何构建

沙盒没有依赖 Docker、Isolate 或其他执行器，每条命令仍由本项目完成以下步骤：

1. 父进程创建任务工作目录和 cgroup，配置内存、进程数限制。
2. 重新执行引擎自身作为初始化子进程，创建 user、mount、pid、net 等命名空间。
3. 在新的 tmpfs 根目录中只读绑定宿主机 `/usr`、`/bin`、`/lib`、`/lib64`，保留 merged-/usr 的符号链接；单独提供动态链接器配置、`/etc/alternatives` 和 `/etc/java-*`，不挂载整个 `/etc`、`/home` 或题库。
4. 挂载任务独立的可写 `/w`、私有 `/tmp`、新 `/proc` 和最小 `/dev`，用 `pivot_root` 切换根目录并卸载旧根目录。
5. 父进程把子进程加入任务 cgroup 后放行；子进程设置 rlimit、丢弃 capabilities、安装 seccomp，再执行编译器或选手程序。
6. 父进程每 20 ms 检查累计 CPU 时间及墙钟时间，读取 cgroup 内存峰值、OOM 等信息，收集输出后清理任务资源。

运行库挂载负责提供文件视图，cgroup 负责资源限制与统计，namespace 负责隔离；三者各自的实现都保留在源码中。定位入口为 `src/sandbox/container.go`，挂载布局在 `runtime.go`，切根和初始化在 `init.go`，资源监控在 `cgroup.go`。

## 配置（config.json）

| 字段 | 说明 |
|---|---|
| `listen` | 监听地址，默认 `127.0.0.1:18080` |
| `rootfs` | 默认留空，使用宿主机工具链；仅需要自备运行时的已有部署填写目录路径 |
| `work_root` | 沙盒工作目录，默认 `/tmp/justcpc-judge-engine` |
| `cgroup_root` | 委派的 cgroup v2 目录；留空则自动发现当前进程 cgroup |
| `workers` | 最大并发评测数；默认 `0` 自动选择，正整数为手动设置，负数报错 |

配置路径可用环境变量 `JUSTCPC_ENGINE_CONFIG` 指定；`config.json` 不入库，参考 `config.example.json`。

`workers` 控制同时处理的评测请求数，每个请求包含编译和按顺序运行测试点。省略或设为 `0` 时，启动时自动选择并发：

- CPU 取当前进程允许使用的逻辑 CPU 数，以及当前和可见祖先 cgroup 的 CPU 配额中的较小值；CPU 槽位为其一半向下取整，至少 1。
- 可用内存取宿主机 `MemAvailable` 与各级 cgroup `memory.max - memory.current` 中的较小值；预留一半，按每个槽位 1 GiB 估算内存槽位，至少 1。
- 最终并发取 CPU 槽位和内存槽位的较小值。必要信息读取失败时回退到 1，并在日志及容量接口中给出警告。

自动模式还会按每份任务编译/运行内存限额的较大值加 128 MiB（至少 1 GiB）预留容量，避免同时执行的大内存任务超过估算预算。单份任务的预留量超过预算时允许独占执行，避免永远排队；这是调度估算，不能保证内存不足的机器也能运行任意题目。手动设置正整数时按该并发执行，不应用自动内存预算。

配套 OJ 的 `judge_workers` 默认 `0`，自动读取本引擎的容量。普通部署无需设置两份并发值。需要手动固定为 2 路时，仅将引擎配置改为 `"workers": 2`，OJ 保持自动即可；需要进一步限制派发时，也可单独指定 OJ 的 `judge_workers`。

自动容量只在启动时选择，运行期间保持固定。修改配置或引擎资源配额后重启引擎及 OJ。已有配置中的 `"workers": 10` 继续表示手动 10 路；改为 `0` 才会启用自动选择。该估算用于控制负载，不承诺计时误差上界。

`GET /api/capacity` 返回 `workers`、`mode`（`auto` / `manual`）、实际可用 CPU、可用内存、内存预算、是否受内存限制、警告，以及当前执行数 `active`。启动日志也会打印选择依据。此接口只查询容量，手动修改通过 `config.json` 完成并重启生效。

已有配置若包含 `"rootfs": "runtime/judge-rootfs"`，将其改为 `"rootfs": ""` 即可使用新的默认运行方式；显式填写路径仍会使用自备运行时。

## 接口

`POST /api/execute`（同步）

请求体（`JudgeTask`）：

```json
{
  "problem_id": "demo",
  "language": "cpp",
  "source": "#include <iostream>\nint main() { long long a, b; std::cin >> a >> b; std::cout << a + b << '\\n'; }",
  "output_dir": "/abs/path/to/output",
  "problem": {
    "id": "demo",
    "limits": {"cpp": {"time_limit_ms": 1000, "memory_limit_mb": 256}},
    "cases": [
      {"id": "1", "input_path": "/abs/path/to/1.in", "expected_path": "/abs/path/to/1.out"}
    ]
  }
}
```

`problem` 为必填项，提供资源限制、比对选项和测试文件路径；`problem_id` 只标识题目，不用于查询题库。`output_dir` 可省略，此时引擎在内存中收集 stdout 并比对。请求和响应的完整字段见 `src/judge/model.go`。

响应体（`Response`）：总 `verdict` + `compile` + 每个测试点的 `cases`。

选手程序任意非零退出码（包括 253）均为该测试点的 `RE`，即使输出正确也不计该点分数，其余测试点照常评测。沙盒初始化故障通过独立错误管道报告为 `SYS`，不占用选手退出码。

选手用时读取 cgroup 累计 CPU 时间；墙钟耗时单独记录，默认上限为 CPU 限时的两倍，用于终止阻塞等长时间不退出的程序。排队发生在任务启动前，不计入选手用时；运行时等待 CPU 会增加墙钟耗时，过度并发可能使墙钟保护提前触发。

CPU / 墙钟限额每 20 ms 检查一次。这是检查周期，不能承诺超限后的终止延迟始终不超过 20 ms；监控进程调度及终止任务也需要时间。最终记录的 CPU 用时不是独立基准校准结果，超限后的额外运行时间也不等同于计量误差。部署时应控制并发并观察重复运行的波动；计时容差和边界裁决策略另行确定。

## 目录说明（哪些是本地资源、不入库）

| 路径 | 内容 | 是否入库 |
|---|---|---|
| `src/` | 仅引擎运行代码，不放测试代码或测试脚本 | ✅ |
| `script/` | 构建/运行/注册服务脚本 | ✅ |
| `test/` | 本地测试代码与脚本 | ❌ 本地保留，不入库 |
| `config.example.json`、`go.mod`、`go.sum`、`README.md` | 配置样例与元数据 | ✅ |
| `runtime/` | 可选的自备运行时，默认部署不需要 | ❌ |

