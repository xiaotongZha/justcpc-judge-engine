# 测评引擎发行版

## 下载安装

Ubuntu 22.04/24.04、amd64、Linux 5.15+、systemd、cgroup v2；与 OJ 使用同一服务器、同一普通账号。无需 Go、Node.js 或 RootFS。

v0.2.1 起兼容 5.15：缺少原生峰值接口时使用近似统计，资源硬限制仍生效。需允许 namespace，并确保工作目录未挂载为 `noexec`。

```bash
sudo apt-get update
sudo apt-get install -y curl ca-certificates g++ openjdk-17-jdk-headless python3
mkdir -p ~/justcpc/judge-engine
cd ~/justcpc/judge-engine

release_url=https://github.com/xiaotongZha/justcpc-judge-engine/releases/download/v0.2.1
archive=JustCPC-Judge-0.2.1-linux-amd64.tar.gz
curl -fLO "$release_url/$archive"
curl -fLO "$release_url/$archive.sha256"
sha256sum -c "$archive.sha256" && tar -xzf "$archive" --strip-components=1

cp -n config.example.json config.json
sudo ./install-service.sh
sudo journalctl -u justcpc-judge-engine -n 30 --no-pager
```

注册脚本配置 cgroup 委托和必要的 AppArmor 规则，并启动、设置开机自启。若提示缺少 `apparmor_parser`，执行 `sudo apt-get install -y apparmor` 后重试安装命令。

默认监听 `127.0.0.1:18080`，`workers: 0` 自动选择并发。配置改 `config.json`；修改后执行 `sudo systemctl restart justcpc-judge-engine`。

## 升级

停止服务，备份并保留 `config.json`；在原目录重复下载、校验、解压命令（换成新版本），然后启动：

```bash
sudo systemctl stop justcpc-judge-engine
# 备份 config.json，下载并解压新版本
sudo systemctl start justcpc-judge-engine
```

## 从源码构建

构建机安装 Go 1.26.5+，在仓库根目录执行 `./script/build-release.sh`。产物在 `release/`；指定版本用 `VERSION=0.2.2 ./script/build-release.sh`。
