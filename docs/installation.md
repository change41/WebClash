# 安装与部署

本页覆盖 Docker 部署、Linux / macOS 一键安装、系统服务、构建发布与本地开发。安装完成后的配置与日常使用见 [使用指南](usage.md)。

## Docker（推荐）

```bash
docker compose up -d --build
docker compose logs -f webclash
```

首次启动会生成管理密码，可从日志或容器内文件获取：

```bash
docker compose exec webclash cat /data/initial-password.txt
```

打开 <http://localhost:7788>，登录后在「设置 - 安全」中修改密码。

数据（设置、订阅、内核二进制）保存在命名卷 `webclash-data` 中。升级镜像后数据不受影响：

```bash
docker compose build && docker compose up -d
```

也可以直接使用 GitHub Actions 发布的预构建镜像（`linux/amd64` + `linux/arm64`）：

```bash
docker pull ghcr.io/<owner>/webclash:latest
```

内置健康检查会周期访问 `/api/health`，容器状态可直接用 `docker compose ps` 观察。

### TUN 模式

默认关闭 TUN，仅提供混合代理端口（容器内 `127.0.0.1:7890`）。如需 TUN，请在 `docker-compose.yml` 中取消以下注释，并在面板中开启 TUN：

```yaml
cap_add:
  - NET_ADMIN
devices:
  - /dev/net/tun:/dev/net/tun
```

### 对外提供代理端口

容器默认只映射 WebUI 端口。若需把代理端口暴露给局域网，可追加映射，并在设置中开启「允许局域网连接」：

```yaml
ports:
  - "7788:7788"
  - "7890:7890"
```

## Linux / macOS 一键安装（免 Node）

发行版是单个 `webclash` 可执行文件（Go 二进制，内嵌前端页面），目标机器**无需安装 Node.js**。支持 Linux x64 / arm64（`CGO_ENABLED=0` 静态链接，**glibc 与 musl（Alpine）通用**，无需 libstdc++）与 macOS x64（Intel）/ arm64（Apple Silicon，原生 darwin 二进制）。

### 一键脚本

```bash
# 离线（已下载产物）
sh install.sh --local ./webclash-linux-x64.tar.xz

# 在线（产物目录 URL，例如 GitHub Releases 的 download 目录）
sh install.sh --base-url https://github.com/OWNER/REPO/releases/download/v0.1.0 --version v0.1.0

# 非 root，仅安装二进制到用户目录
sh install.sh --local ./webclash-linux-x64 --dir ~/.local/bin --no-service

# macOS（仅安装二进制，无服务）
sh install.sh --local ./webclash-darwin-arm64.tar.gz --dir ~/.local/bin --no-service
```

脚本自动检测系统（Linux / macOS）与架构（`uname -m`）；Linux 额外检测内核版本（建议 ≥ 4.18）与服务管理器（systemd / OpenRC）。校验 sha256 后原子安装二进制，Linux 上 root 且检测到 systemd / OpenRC 时自动执行 `webclash install`；macOS 仅安装二进制。

| 参数 | 说明 |
| --- | --- |
| `--local <文件>` | 使用本地产物（离线安装，优先） |
| `--base-url <URL>` | 产物下载目录（含 `webclash-*` 与 `SHA256SUMS`） |
| `--version <TAG>` | 发布版本（在线模式用于提示） |
| `--mirror <前缀>` | GitHub 加速前缀（如 `https://ghfast.top/`） |
| `--dir <目录>` | 二进制安装目录（默认 `/usr/local/bin`） |
| `--data-dir <目录>` | 服务数据目录（默认 `/var/lib/webclash`） |
| `--port <端口>` / `--host <地址>` | WebUI 监听端口 / 地址（默认 `7788` / `0.0.0.0`） |
| `--service` / `--no-service` | 强制 / 禁止安装系统服务 |
| `--dry-run` | 只打印检测与将执行的动作 |
| `-y, --yes` | 跳过交互确认 |

### 直接运行

```bash
./webclash run -p 7788 -d /var/lib/webclash       # 前台运行（run 可省略）
./webclash run -p 7788 --password '示例密码'      # 首次启动指定管理密码
./webclash version --json                         # 版本 / 架构 / 运行时（runtime=go, static=true, embedded=true）
./webclash password -d /var/lib/webclash          # 查看首次启动生成的管理密码
```

内嵌形态默认数据目录为 `~/.webclash`；`-d` 或环境变量 `DATA_DIR` 可覆盖。

### 安装为服务

```bash
sudo ./webclash install                                  # 自动检测 systemd / OpenRC
sudo ./webclash install --service-manager openrc --port 7788
sudo ./webclash install --user webclash --tun            # 非 root 运行并保留 TUN 能力（用户需已存在）
webclash status                                          # 0 运行 / 3 未运行 / 4 未安装
sudo webclash start | stop | restart
sudo webclash uninstall                                  # 卸载服务，保留数据
sudo webclash uninstall --purge --yes                    # 彻底清除数据与配置
```

- systemd：unit 位于 `/etc/systemd/system/webclash.service`，环境变量唯一来源 `/etc/webclash/webclash.env`；`KillMode=mixed` 保证退出时先优雅停止 mihomo；默认授予 `CAP_NET_ADMIN/NET_RAW/NET_BIND_SERVICE` 并允许 `/dev/net/tun`。
- OpenRC：`/etc/init.d/webclash` + `/etc/conf.d/webclash`，日志 `/var/log/webclash.log`。
- 升级：用新二进制再次 `webclash install`（原子替换，数据不变）后 `restart`。
- 配置优先级：**命令行参数 > 环境变量 > settings.json > 内置默认**。

### macOS 原生运行

macOS 使用原生 darwin 二进制（Apple Silicon 选 `webclash-darwin-arm64`，Intel 选 `webclash-darwin-x64`），**仅前台运行，不安装系统服务**（launchd 暂未支持）。

```bash
# 下载并解压（gzip，macOS 原生支持），前台运行
tar -xzf webclash-darwin-arm64.tar.gz
./webclash-darwin-arm64 run -p 7788 -d ~/.webclash

# 或用一键脚本仅安装二进制到用户目录（安装后命令名为 webclash）
sh install.sh --local ./webclash-darwin-arm64.tar.gz --dir ~/.local/bin --no-service
```

- `version --json` 在 macOS 上 `platform=darwin`、`libc=n/a`、`serviceManager=none`。
- 内核下载在 macOS 直接可用（自动选择 `mihomo-darwin-<arch>` 资产）。
- **Gatekeeper / EDR 可能拦截**新下载的 `webclash` 与面板下载的 `mihomo`：首次运行若被拦截，请在「系统设置 - 隐私与安全性」中允许，或把二进制加入安全软件白名单；受限终端建议改用 Docker 部署。
- TUN 模式需要 `/dev/net/tun` 与相应权限，macOS 原生形态默认不提供，建议使用混合代理端口或 Docker。

## 构建与发布

### 本地构建发行版

```bash
npm run release:go            # 在 Docker 内构建全部 4 个目标（linux+darwin × x64/arm64），产物输出到 dist-go/
npm run release:go:linux      # 仅 linux/amd64 + linux/arm64
npm run release:go:darwin     # 仅 darwin/amd64 + darwin/arm64
npm run release:verify        # 容器内冒烟 + systemd/OpenRC 生命周期 + geo + 体积/RSS + darwin 编译/格式/vet 验证
```

构建需要 Docker（BuildKit），原生交叉编译无需 QEMU。产物：`dist-go/webclash-linux-{x64,arm64}`（`.tar.xz`）、`webclash-darwin-{x64,arm64}`（`.tar.gz`）、`SHA256SUMS`、`install.sh`。单文件约 8–9 MB（Go 二进制，内嵌前端），面板常驻内存约 40 MB 量级（连接历史、流量桶与日志均有上限，长时间运行稳定）。受限网络可传 `--build-arg APT_MIRROR=mirrors.aliyun.com`（及 `APK_MIRROR` / `GOPROXY`）替换包源。

### GitHub Actions 自动发布

仓库内置 `.github/workflows/release.yml`：

- **触发方式**：推送 `v*` 标签自动发布；或在 Actions 页面手动 `workflow_dispatch`（可填版本号，默认 `dev-<sha>`，仅构建并保留工作流产物，不创建 Release）。
- **流程**：构建前端并 `typecheck` → 交叉编译 4 个平台二进制（linux/amd64 上执行 `go test ./...`）→ 生成 `tar.xz` / `tar.gz`、`SHA256SUMS` 与 `install.sh` → 标签触发时创建 GitHub Release（自动生成 release notes）。
- **Docker 镜像**：同一工作流构建 `linux/amd64` + `linux/arm64` 镜像并推送到 GitHub Container Registry：`ghcr.io/<owner>/webclash:<版本号>`；推送标签时同时更新 `:latest`，手动 dispatch 只推版本号标签。GHCR 包首次创建后默认继承仓库可见性，公开仓库需在包设置中把可见性改为 public。
- **镜像清理**：`.github/workflows/cleanup-ghcr.yml` 每周一自动删除旧的 `dev-*` 镜像，只保留最近 3 个；`latest`、正式版本号以及带混合标签的版本不受影响。可在 Actions 页面手动 dispatch，自定义保留数量并用 dry-run 先预览。
- **Release 资产**与本地 `dist-go/` 一致，可直接配合 `install.sh --base-url` 在线安装。

```bash
git tag v0.1.0
git push origin v0.1.0
```

工作流使用 `GITHUB_TOKEN` 创建 Release（声明了 `contents: write`）。若仓库或组织策略限制了默认令牌权限，需在「Settings - Actions - General」中允许工作流写权限。

## 本地开发

架构、模块职责、测试与调试的完整说明见 [开发指南](development.md)；本节只列最短上手路径。

前端要求 Node.js >= 20.11；后端 Go 模块要求 Go >= 1.26（`panel/go.mod`），发行构建镜像使用 Go 1.27。

```bash
npm install
npm run dev        # Vite :5173（/api、/ws 已代理到后端 :7788）
npm run typecheck  # TypeScript 类型检查
npm run build      # 构建前端到 web/dist
```

后端在 Docker 中运行（部分宿主机 EDR 会拦截直接执行新二进制）：

```bash
docker compose up -d --build   # Go 后端 :7788
```

后端单测 / vet 可在 Docker 中执行：

```bash
docker run --rm -v "$PWD/panel:/src" -w /src \
  -v wc-go-mod:/go/pkg/mod -v wc-go-cache:/root/.cache/go-build \
  -e GOPROXY=https://goproxy.cn,direct \
  golang:1.27-bookworm sh -c 'go test ./... && GOOS=darwin GOARCH=arm64 go vet ./...'
```

常用环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `WEBUI_PORT` | `7788`（设置值） | WebUI 监听端口 |
| `WEBUI_HOST` | `0.0.0.0`（设置值） | WebUI 监听地址 |
| `WEBUI_PASSWORD` | 空 | 首次启动使用的管理密码（`run --password` 等价） |
| `DATA_DIR` | `<项目>/data` | 数据目录（内嵌单文件默认 `~/.webclash`） |
| `WEBCLASH_WEB_DIST` | `<项目>/web/dist` | 预留：磁盘前端覆盖（当前版本未接入，联调请构建后拷入 `panel/internal/webui/dist`） |

## 目录结构

```
.github/workflows/   GitHub Actions 构建与发布
panel/
  cmd/webclash/      启动入口（main.go）
  internal/
    api/             REST 路由 + 鉴权 + 静态/SPA + WebSocket 桥接
    core/            配置 / 订阅 / 设置编排 + 连接历史 / 流量聚合
    kernel/          内核下载与进程生命周期
    mihomo/          配置合并生成（yaml.Node）、Clash API 客户端
    geo/             内核 geoip.dat / geosite.dat 下载与非致命降级
    geoip/ geoipprobe/  连接页 IP 归属查询与归属数据库下载
    service/         systemd / OpenRC 安装与模板
    cli/             子命令解析 / 分发 / 退出码
    webui/           go:embed 内嵌前端（dist/）
web/src/            React 19 + Vite + Tailwind 面板
scripts/            发行构建（release-go.Dockerfile）、一键安装（install.sh）、验证与 GHCR 清理脚本
data/               运行时数据（settings.json、profiles/、kernel/、bin/、geoip/）
```
