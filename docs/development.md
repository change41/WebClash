# 开发指南

面向参与 WebClash 开发的读者：架构、模块职责、本地联调、测试与发布。部署方式见 [安装与部署](installation.md)，功能使用见 [使用指南](usage.md)。

## 架构与进程模型

```
浏览器 ── /api、/ws ──> WebClash 面板（Go，:7788）
                            │  Clash API（127.0.0.1:9090）
                            └──> mihomo 子进程
```

- 浏览器只与面板通信，不直接接触内核控制器；面板负责鉴权、转发与 WebSocket 数据桥接。
- 面板是唯一的状态持有者：设置、订阅、连接历史、流量桶都在面板进程内，内核可随时替换 / 重启。
- 前端在发行时构建并 `go:embed` 进 Go 二进制；开发时由 Vite 提供热更新，并代理 `/api`、`/ws` 到后端。
- Geo 数据下载（内核 `geoip.dat` / `geosite.dat`、IP 归属库）一律非致命：失败只降级或标记未就绪，不阻塞启动。
- 长时间运行的结构都有硬上限（连接历史 100–20000、流量桶 1440、内核日志 400 行），改动热路径时保持这一约束。

## 仓库结构

```
panel/                     Go 后端（module webclash）
  cmd/webclash/            入口（main.go → cli.Run）
  internal/
    api/                   字面量分段路由、鉴权、静态/SPA、WebSocket 桥接
    bootstrap/             HTTP 服务启动、端口/环境变量解析
    cli/                   run / install / service / password 等子命令
    config/                设置结构、默认值与 Normalize 校验
    core/                  设置编排、订阅激活、连接历史、流量聚合、IP 归属查询
    kernel/                内核下载、启动守护、崩溃重启、日志缓冲
    mihomo/                配置合并生成（yaml.Node）与 Clash API 客户端
    geo/                   内核 geo 数据下载与降级
    geoip/ geoipprobe/     归属数据库管理与连接页查询
    service/               systemd / OpenRC 模板与安装
    webui/                 go:embed 内嵌前端与静态资源表
    buildinfo/             Version / BuildTime 注入
web/                      React 19 + TypeScript + Vite + Tailwind 4
  src/pages/              仪表盘 / 代理组 / 订阅 / 连接 / 规则 / 日志 / 设置
  src/components/         布局与通用 UI（ui.tsx、TrafficChart.tsx）
  src/lib/                api、轮询/WebSocket hooks、内核状态、toast、格式化
  src/types.ts            与后端 JSON 对齐的类型定义
scripts/                  发行构建 Dockerfile、一键安装、发布校验、容器冒烟、GHCR 清理
.github/workflows/        GitHub Actions 构建与发布
docs/                     文档
```

## 本地开发环境

| 依赖 | 版本要求 | 说明 |
| --- | --- | --- |
| Node.js | >= 20.11 | 前端开发与构建 |
| Go | >= 1.26 | `panel/go.mod`；发行镜像使用 Go 1.27 |
| Docker | 任意较新版本 | 推荐用于运行后端、测试与发行构建 |

### 前端

```bash
npm install
npm run dev        # Vite :5173
npm run typecheck  # tsc --noEmit
npm run build      # 构建到 web/dist（含 tsc 检查）
```

`web/vite.config.ts` 已把 `/api` 与 `/ws` 代理到 `http://127.0.0.1:7788`，后端启动后直接开发页面即可。

### 后端

推荐用 Docker 运行后端（部分宿主机 EDR 会拦截直接执行新二进制）：

```bash
docker compose up -d --build      # 面板 :7788，数据在命名卷
docker compose logs -f webclash
```

需要原生调试时，先把前端产物拷入嵌入目录再运行：

```bash
npm run build
rm -rf panel/internal/webui/dist/assets
cp -R web/dist/. panel/internal/webui/dist/
go run ./cmd/webclash run -d ./data -p 7788 --password dev
```

说明：

- `panel/internal/webui/dist/` 在仓库中只保留占位 `index.html`，保证 `go:embed all:dist` 可编译；真实产物由发行构建（Dockerfile / `release:go`）或上面的拷贝步骤填入。
- `run` 是默认子命令，`-p` / `-H` / `-d` / `--password` 分别覆盖端口、监听地址、数据目录与初始密码（等价环境变量 `WEBUI_PORT` / `WEBUI_HOST` / `DATA_DIR` / `WEBUI_PASSWORD`）。
- `--web-dist` 与 `WEBCLASH_WEB_DIST` 目前仅被 CLI 解析，Go 端尚未接入磁盘加载，请以「构建 + 拷贝」或 Vite 代理为准。
- 数据目录由 `-d` / `DATA_DIR` 指定；未指定时默认为 `~/.webclash`，Docker 镜像内固定为 `/data`。目录中包含 `settings.json`、`profiles/`、`kernel/`、`bin/`、`geoip/`。

### Go 测试与 vet

```bash
docker run --rm -v "$PWD/panel:/src" -w /src \
  -v wc-go-mod:/go/pkg/mod -v wc-go-cache:/root/.cache/go-build \
  -e GOPROXY=https://goproxy.cn,direct \
  golang:1.27-bookworm sh -c 'go test ./... && go vet ./...'
```

## 后端开发

### 路由与中间层

`panel/internal/api/server.go` 使用字面量分段路由（而非 `http.ServeMux`），路径段按 `/` 精确匹配，`{id}` 形式捕获路径参数：

- `s.add(method, pattern, handler)` 注册路由，GET 会自动登记 HEAD；
- `matchAPI` 不匹配时回落到静态资源 / SPA，`..`、编码异常等路径不会被 301 重定向；
- `ServeHTTP` 统一做 32MB 请求体限制与登录校验，`publicAPI` 之外的 `/api/*` 与 `/ws/*` 都要求会话；
- 路径参数用 `api.PathParam(r, "id")` 读取。

新增接口的推荐顺序：`core` 实现业务 → `api` 写 handler（`writeJSON` / `writeError`）→ `server.go` 注册路由 → `web/src/types.ts` 补类型 → 页面调用 `api()`。

### 设置与配置生成

- 设置模型在 `panel/internal/config`：`DefaultSettings`、`NormalizeSettings`（对任意输入做类型/范围收敛）、`ToPublic`（隐藏 `passwordHash` / `auth.secret`）。
- `core.SaveSettings` 的语义是「持久化 → 生成运行时配置 → 已运行则热重载，失败回退重启」；`auth` 段不允许通过该接口修改，密码走 `ChangePassword`（会轮换会话密钥使旧会话失效）。
- `controller secret` 轮换（`RotateControllerSecret`）会重启内核。
- 生成的 `data/kernel/config.yaml` 由订阅原文 + 面板设置合并而来（`panel/internal/mihomo/config.go`），DNS / 端口 / TUN / 控制器等字段以设置为准；配置中的 geo 引用缺失时由 `internal/geo` 做降级剥离。
- 扩展规则按「当前订阅 → 全局 → 订阅原文 → MATCH」顺序插入：`BuildConfig` 先写入全局规则，`core.buildCurrentConfig` 再对活跃订阅前置一次；规则保存在 `settings.json` / `profiles.json`，不写入订阅原文。保存时通过 `config.ParseCustomRulesStrict` 严格校验（大写类型、至少三段、拒绝 `MATCH`、按首次出现去重），并基于最终生成配置校验策略目标（内置策略或 `proxies` / `proxy-groups` 名称）；非法规则在修改内存状态前返回 400，并指出具体规则与缺失策略。

### 内核生命周期

`panel/internal/kernel` 负责下载（自动匹配平台与架构）、启动等待控制器就绪、异常退出后 3 秒拉起（1 分钟内最多 5 次）以及日志环形缓冲。修改相关逻辑时注意：启动失败不能拖垮面板进程，日志与状态必须可观测。

### 连接历史与流量聚合

- `core/conn_history.go` 每秒拉一次 mihomo `/connections`（`RequestInto` 类型化解码，避免二次 JSON 编解码），按 `id` 差分记录已关闭连接，环形缓冲上限由设置决定。
- `core/traffic.go` 用累计 `uploadTotal/downloadTotal` 差分出 1 分钟平均速率桶（1440 个桶覆盖 24h），只保存在内存；计数器回退、时钟回拨与超过 90 秒的间隔都会重新建立基线。
- 流量快照是**稀疏**的：只有产生过流量的分钟才会返回点。前端 `TrafficChart` 据此自适应时间窗口与聚合粒度（1 分钟起，最多约 96 个绘图点）。

### IP 归属

`internal/geoip` 支持 ip2region（省市 / 运营商）与 mmdb（国家 / 地区），查询完全离线；`core.GeoIPLookup` 对域名先做缓存解析再查库。连接页同时查询源 IP 与目标 IP，以「源归属 → 目标归属」展示；源 IP 为回环 / 内网地址时由前端标注为「本机」/「局域网」。注意 fake-ip：连接元数据里的 `destinationIP` 可能是伪造地址，真实目标在 `remoteDestination`，连接页与归属查询以 `remoteDestination` 优先。

### WebSocket

`api/ws.go` 处理 `/ws/connections`、`/ws/logs` 升级：连接间隔会被钳制到 500–5000ms，日志按级别过滤。WebSocket 与 REST 走同一套会话鉴权。

### 错误与 JSON 约定

- 业务错误用 `util.NewHttpError(status, message)`，`writeError` 输出 `{"error": "..."}`；
- HTTP 响应字段统一 camelCase，与 `web/src/types.ts` 一一对应；
- 用户可见的错误信息使用中文，与现有文案风格保持一致。

## 前端开发

- 技术栈：React 19、react-router-dom 7、Vite 7、Tailwind 4；无全局状态库。
- 登录态在 `App.tsx` 顶层维护，未登录直接渲染 `Login`；`setUnauthorizedHandler` 在任何 401 时统一退回登录页。
- 数据获取：`usePolling`（定时轮询、可开关）与 `useWsJson`（自动重连的 WebSocket）；内核状态由 `KernelProvider` 共享。
- `api()` 统一拼 `/api` 前缀、处理 401 与错误信息；`lib/format.ts` 提供速率、字节、时长等格式化。
- 页面在 `App.tsx` 注册路由、`components/Layout.tsx` 注册导航；通用交互组件（Button、Input、Badge、ConfirmDialog、EmptyState、Section 等）放在 `components/ui.tsx`。
- 图表组件保持纯展示 + 内部派生数据（如 `TrafficChart` 的时间窗口 / 聚合 / 悬浮查找都在 `useMemo` 内完成），避免在渲染期间做重计算。
- 界面文案使用中文；样式复用 Tailwind token（`ink-*`、`line` 等）。深色 / 浅色主题通过 `html.light` 重映射同一套颜色变量实现，新增组件优先使用现有 token，避免写死颜色。

新增页面步骤：`pages/Xxx.tsx` → `App.tsx` 路由 → `Layout.tsx` 导航 → `types.ts` 类型 → 需要新接口时按后端的「新增接口顺序」补全。

## 测试与冒烟

- 后端：Docker 内 `go test ./...`；涉及并发/热路径的改动建议再加 `go test -race ./internal/core/...`；跨平台编译检查可用 `GOOS=darwin GOARCH=arm64 go vet ./...`。
- 前端：`npm run typecheck` 与 `npm run build`；构建通过只代表类型与打包正确，交互变更需人工在浏览器验证。
- 容器冒烟：`scripts/test/` 下提供冒烟脚本（如 `smoke.sh`），在无 Node 的容器里验证 `version --json`、启动、健康检查、鉴权与静态资源。
- 冒烟约定：使用隔离的临时容器与数据卷（`wc-*`）、端口 ≥ 7799，不要影响正式实例（默认 `webclash`、端口 7788）与其数据卷。

## 调试

- 面板日志：`docker compose logs -f webclash`，或在「日志」页查看内核输出；内核崩溃/重启原因会写入面板日志。
- 生成配置：`data/kernel/config.yaml`；修改设置后对比该文件确认最终生效值。
- 设置文件：`data/settings.json`（包含密码哈希与会话密钥，勿外传）。
- 直连控制器（仅本机）：从 `settings.json` 读取 `controller.secret` 后请求 `127.0.0.1:9090`，例如 `/connections`、`/proxies`、`/version`。
- 常见坑：
  - fake-ip 模式下 `destinationIP` 不是真实 IP，排查连接时看 `remoteDestination`；
  - 流量历史与连接历史都在面板内存中，重启面板即清空；
  - 静态资源文件名带哈希且长期缓存，改完前端需重新构建并确认加载了新 bundle；
  - 部分宿主机 EDR 会杀掉新生成的可执行文件，运行验证统一放到 Docker。

## 构建与发布

发行链路为「前端构建 → 拷入 `panel/internal/webui/dist` → 交叉编译静态二进制（`CGO_ENABLED=0`）」：

```bash
npm run release:go       # 四平台产物输出到 dist-go/（Docker BuildKit）
npm run release:verify   # 校验产物与校验和
```

- 版本与构建时间通过 `-ldflags -X webclash/internal/buildinfo.*` 注入；
- `dist-go/` 产出 4 个二进制、linux `tar.xz`、darwin `tar.gz`、`SHA256SUMS` 与 `install.sh`；
- GitHub Actions 在推送 `v*` 标签时自动构建并创建 Release，同时推送 `ghcr.io/<owner>/webclash:<版本号>` 与 `:latest` 多架构镜像；手动 dispatch 仅构建并推送版本号镜像；`cleanup-ghcr.yml` 每周清理 `dev-*` 镜像（保留最近 3 个）；细节见 [安装与部署](installation.md#github-actions-自动发布)。

## 代码约定

- Go：包注释说明职责与来源（部分包是原 Node 实现的 1:1 移植，语义、错误文案与字节契约保持一致）；提交前 `gofmt`；热路径使用类型化 JSON 解码与有界缓冲。
- 前端：函数组件 + hooks；类型集中在 `types.ts`；避免为一次性需求引入抽象；组件内注释只写非显而易见的约束。
- 安全：不要提交真实密钥、订阅链接或 `settings.json`；控制器只监听本机；新增接口默认纳入鉴权，只有登录/健康检查例外。
- 文档：行为或部署方式变化时同步更新 `docs/`，保持三份文档（简介 / 安装 / 使用 + 本开发指南）互链。
