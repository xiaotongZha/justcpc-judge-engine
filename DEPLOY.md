# 测评引擎发行版

## 构建

构建机安装 Go 1.26.5+。在仓库根目录执行：

```bash
./script/build-release.sh
```

产物在 `release/`；指定版本：`VERSION=0.2.1 ./script/build-release.sh`。

## 部署

Ubuntu 24.04、amd64、systemd、cgroup v2；与 OJ 使用同一普通账号、同一服务器。

```bash
sudo apt-get update
sudo apt-get install -y g++ openjdk-21-jdk-headless python3
```

解压发行包，进入目录：

```bash
cp -n config.example.json config.json
sudo ./install-service.sh
sudo journalctl -u justcpc-judge-engine -n 30 --no-pager
```

注册脚本配置 cgroup 委托和按需安装 AppArmor 规则，并启动、设置开机自启。若提示缺少 `apparmor_parser`，安装 `apparmor` 后重试。无需 Go、Node.js 或 RootFS。

默认监听 `127.0.0.1:18080`，`workers: 0` 自动选择并发；OJ 的 `judge_url` 指向此地址。

升级：停服务，保留 `config.json`，将新包复制到原部署目录后启动：

```bash
sudo systemctl stop justcpc-judge-engine
# 更新文件，保留 config.json
sudo systemctl start justcpc-judge-engine
```
