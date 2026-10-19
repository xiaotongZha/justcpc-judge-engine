# 更新记录

## 1.0.0 · 2026-10-19

- 统一正式发行版本与下载、安装文档，配套 JustCPC Online Judge 1.0.0。
- 保留 v0.2.1 的 Linux 5.15 兼容方案：缺少 cgroup 峰值接口时以 20ms 采样与最大 RSS 估计，CPU 累计计时和资源硬限制保持启用；短暂峰值可能遗漏。
- 无需手动安装 RootFS，使用宿主机 C++ / Java / Python 工具链；支持自动选择并发和手动配置。

运行要求：Linux amd64、cgroup v2 委托、namespace 权限和可执行工作目录。升级时停止服务并保留 config.json；统计兼容模式沿用已有实现，本版未新增引擎评测逻辑。
