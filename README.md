# justcpc-judge-engine

## Linux 安装

Ubuntu 22.04/24.04、amd64、Linux 5.15+、cgroup v2、systemd。首次安装自动补齐 C++/Java/Python 评测工具链。 脚本使用 curl、tar 等 Linux 命令，无需安装编程环境。

```bash
mkdir -p ~/justcpc-judge-engine && cd ~/justcpc-judge-engine
curl -fL https://gitee.com/zhaxt/justcpc-judge-engine/raw/master/install.sh -o install.sh ||
  curl -fL https://raw.githubusercontent.com/xiaotongZha/justcpc-judge-engine/master/install.sh -o install.sh
bash install.sh
```

自动下载最新正式版，解压到当前目录并启动服务；安装后可看到 `update.sh` 和 `uninstall.sh`。

默认监听 `127.0.0.1:18080`，供同机 justcpc-online-judge 调用。

## 更新与卸载

```bash
./update.sh             # 检查版本，确认后原地更新
./uninstall.sh          # 卸载，询问是否删除配置和数据，默认保留
./install.sh --logs     # 查看实时日志，Ctrl+C 退出
./install.sh --restart  # 修改 config.json 后重启
```

更新保留 `config.json` 和用户数据，不创建备份；失败可重跑 `bash install.sh`。卸载可用 `--keep-data` 明确保留，或 `--delete-data` 明确删除（无法恢复）。

[开发说明](docs/DEVELOPMENT.md)
