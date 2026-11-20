# WebClash

基于 [mihomo](https://github.com/MetaCubeX/mihomo)（Clash Meta）内核的 Web 管理面板。参考 OpenClash 与 Clash Verge 的使用习惯，把内核下载、配置生成、进程守护和 Clash API 代理都收敛到一个 Go 服务中。后端是单个 Go 二进制（内嵌 React 前端），支持 Linux 与 macOS，目标机器**无需 Node.js 运行时**，也可以直接用 Docker 部署。

```
浏览器 ──HTTP / WebSocket──> WebClash（Go，内嵌前端）
                                 │  Clash API / WebSocket（127.0.0.1:9090）
                                 └──> mihomo 内核（子进程）
```

- 前端通过面板的 REST API 与 WebSocket 访问数据，浏览器不直接接触内核控制器。
- 设置、订阅、内核与运行数据都存放在一个数据目录中，备份 / 迁移只需拷贝该目录。
- 内核控制器只监听 `127.0.0.1`，面板负责鉴权、转发与数据流桥接。

## 特性亮点

- **仪表盘**：主机 CPU / 内存 / 负载、实时速率与连接数、24 小时流量图（悬浮查看任意时间点数值）、内核状态与当前订阅流量配额。
- **代理组**：按策略组展开节点，Selector 组点击切换，支持单节点 / 整组延迟测速、折叠与搜索。
- **订阅配置**：URL 订阅与本地 YAML，每个订阅独立设置自动更新间隔与测速参数，一键激活并热重载。
- **规则 / 规则集**：按顺序查看规则与策略；支持全局或按订阅追加扩展规则并优先命中；规则集内容在线预览、过滤、复制与导出。
- **连接**：活跃连接实时流与已关闭连接历史，支持 IP 归属展示、全局搜索、单条 / 全部关闭。
- **日志**：内核日志实时流，级别筛选、关键字过滤、暂停 / 自动滚动、导出。
- **设置**：运行模式、混合 / HTTP / SOCKS 端口、端口认证（统一为所有代理端口设置认证）、TUN、DNS（fake-ip）、控制器、内核版本与镜像、连接历史上限。
- **外观**：深色 / 浅色主题切换，首次访问跟随系统偏好，选择保存在浏览器本地。
- **内核管理**：自动下载 / 更新 mihomo，崩溃自动拉起，启动前准备 Geo 数据。
- **安全**：随机初始密码、scrypt 哈希、HMAC 签名 HttpOnly 会话、登录限速，控制器仅绑定本机。

## 技术特点

- 单文件发行：Go 静态二进制（`CGO_ENABLED=0`，glibc / musl 通用）+ `go:embed` 内嵌前端。
- 四平台交叉编译：linux / darwin × amd64 / arm64，无需 QEMU；macOS 提供原生二进制。
- 数据自包含：设置、订阅、内核与 Geo 数据统一在数据目录（Docker 使用命名卷）。
- 前后端分离开发：React 19 + Vite + Tailwind；生产环境由 Go 直接托管内嵌资源。

## 文档

| 文档 | 内容 |
| --- | --- |
| [安装与部署](docs/installation.md) | Docker、Linux / macOS 一键安装、系统服务、构建发布（含 GitHub Actions）、本地开发 |
| [使用指南](docs/usage.md) | 功能说明、日常操作、默认值与数据目录、Geo / IP 归属、常见问题、安全说明 |
| [开发指南](docs/development.md) | 架构与模块职责、本地联调、测试与冒烟、调试技巧、构建发布与代码约定 |

## 快速开始

```bash
docker compose up -d --build
docker compose exec webclash cat /data/initial-password.txt
```

打开 <http://localhost:7788>，使用初始密码登录并在「设置 - 安全」中修改。更多部署方式（一键脚本、系统服务、macOS 原生运行）见 [安装与部署](docs/installation.md)。
