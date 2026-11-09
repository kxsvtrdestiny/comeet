# Comeet · 第 1 阶段

以 [开发规格 v2.0](docs/开发规格-v2.0.md) 为当前开发依据。**只完成工程骨架和界面基础，等待用户验收，未开始第 2 阶段。**

项目路径：仓库根目录下 `week03/homework/comeet`。当前可以查看工作台、登录/注册、准备页、深色会议室和历史页骨架，以及真实 Go + SQLite 健康状态。尚无账号、真实会议、聊天、音视频或 AI；按钮禁用并标注开放阶段，没有测试账号需要创建，也没有演示会议数据。

## Windows 首次运行（PowerShell）

安装 Node.js 24（本次实际验证 24.22.1 / npm 11.9.0）和 Go 1.25.3。安装后重新打开 PowerShell，先运行 `node --version`、`npm --version`、`go version` 确认可用。无须 Docker、CGO、SQLite CLI、LiveKit 或 DeepSeek 密钥。

终端 A：从本仓库根目录执行以下命令，启动后保持开启。

```powershell
cd .\week03\homework\comeet\server
Copy-Item .env.example .env
# 仅首次下载依赖；保留 go.sum 校验
 go mod download
 go run ./cmd/server -env-file .env
```

`Copy-Item` 仅用于首次创建，已有 `.env` 请勿覆盖。默认监听 `127.0.0.1:8080`；相对数据库路径基于 server 工作目录，自动创建 `server/data/comeet.db`。终端看到 JSON 日志 `server_start` 后继续。

终端 B：另开 PowerShell，同样从本仓库根目录执行，启动后保持开启。

```powershell
cd .\week03\homework\comeet\web
npm ci
npm run dev
```

浏览器打开 **http://localhost:5173**。5173 严格固定，已被占用会报错而不会悄悄换端口。前端 `/api` 与 `/ws` 代理到 `127.0.0.1:8080`；当前 WS 只配置代理，尚无业务端点。

打开主页“开发环境状态”，应显示“数据库初始化正常”、迁移 `001_framework.sql`、媒体和 AI 未配置。这些可选配置缺失不会阻止当前阶段启动。

## 后续启动、停止和重启

后续启动无需重新复制 `.env`，一般也无需重新安装依赖：终端 A 在 server 目录运行 `go run ./cmd/server -env-file .env`，终端 B 在 web 目录运行 `npm run dev`。更新锁文件后再运行 `npm ci` / `go mod download`。

两个终端分别按 **Ctrl+C** 停止。重新执行对应启动命令即可重启，不要删除 data 目录。停止 Go 后点击首页“重新检查”会显示连接错误，重新启动后再次检查应恢复正常。

## 路由与本阶段验收

| 路由 | 当前内容 |
|---|---|
| `/` | 工作台骨架、真实 health 状态 |
| `/login`、`/register` | 表单外观和密码显示切换；提交禁用 |
| `/meetings/preview/prepare` | 准备页骨架，不申请设备权限 |
| `/meetings/preview/room` | 会议室骨架，可切换成员/聊天空态 |
| `/history`、`/history/preview` | 后续功能说明，无假记录 |

`preview` 只是路由检查参数，不是数据库中的会议。当前路由不设置登录保护，真实鉴权在第 2 阶段实现。

人工检查见 [手工测试](docs/手工测试.md)，实际自动结果见 [阶段验收](docs/阶段验收.md)。Windows Chrome/Edge 实机效果必须由用户确认；Linux Chromium 自动检查不能替代 Windows 实机验收。

## 检查与构建

server 目录：

```powershell
go test -count=1 ./...
go vet ./...
go mod verify
go build -o .\data\comeet.exe ./cmd/server
```

web 目录：

```powershell
npm run typecheck
npm run build
# 可选浏览器回归：先保持 Go / Web 两个服务运行
npx playwright install chromium
npm run test:e2e
```

Go 构建产物包含迁移文件；Web 产物在 web/dist。生产静态资源托管、HTTPS、认证与业务能力尚未交付，不能将本阶段骨架当作完整会议产品部署。

## 配置与安全边界

所有后端配置均列在 server/.env.example。`-env-file .env` 显式加载文件；不传此参数只读系统环境，既有环境变量优先于文件。生产只读系统环境，不自动找 `.env`。端口、origin、正整数及布尔参数会在启动时校验。JWT_SECRET 当前未使用，到第 2 阶段再生成用户自己的私密值并强制校验；不要使用公开示例密钥。

LiveKit 在第 4 阶段配置 API URL、WS URL、Key 和 Secret；DeepSeek 在第 7 阶段配置启用开关、模型和 Key。当前 health 的 configured 只表示字段齐全，不执行上游连接。媒体 SDK、JWT、WS、Zustand 等依赖会在对应阶段按需加入，不预装后续业务。任何真实密钥都不能放进 VITE_ 变量或提交到仓库。

后续两账号测试需等待第 2 阶段自己注册 A/B，第 3 阶段再验证双端文字互通，第 4 阶段验证真实音视频。当前不提供假账号或假互通。

## 常见问题

- `go` 不是命令或显示 `Unknown option`：执行 `Get-Command go`，应指向 Go 安装目录的 go.exe，调整 PATH 后重开终端。
- npm 脚本被 PowerShell 策略阻止：用 `npm.cmd ci`、`npm.cmd run dev`，不必降低系统执行策略。
- 端口占用：关闭你之前启动的同端口服务后重试；不要随意终止其他进程。
- health 失败：检查终端 A、访问 `http://localhost:8080/api/v1/health/ready`，再检查终端 B 的代理日志。
- 数据库目录不可写：把项目放在用户可写目录，或在 `.env` 调整 DATABASE_PATH。不要以管理员运行掩盖路径问题。
- 依赖下载失败：保留 TLS、校验和验证，检查 Go/NPM 网络可达性。此云环境默认 Go 代理的 storage.googleapis.com 下载曾被拦截；本次通过对应源码仓库版本及 Go 校验完成安装，普通 Windows 网络不需要该云环境的缓存路径。
- 生产环境深层路由刷新需要静态托管回退 index.html；本阶段 Vite 开发服务器已提供回退。

报告问题时提供：访问路径、预期/实际现象、浏览器与 Node/Go 版本、脱敏后的两端终端输出。截图/日志不要包含 `.env` 内容、Key、Secret 或 token。

## 云端网页预览

Windows 本机仍使用 `npm run dev`。云环境预览使用 `npm run dev:cloud`，监听 `0.0.0.0:5173`，供平台预览代理连接。Go 保持内部监听，由 Vite 代理 API，无须公开 8080。该命令只用于可信开发环境，不代表生产部署。

云端预览必须通过平台提供的预览卡片打开；本机直接输入 localhost 不会连接到云端。平台入口是否实际可用需要用户复测，云端 HTTP 检查不能替代浏览器入口验收。

## GitHub Pages 第一阶段静态测试站

源码分支为 `stage1-review`，静态站分支为 `gh-pages`。用户已授权本次推送。站点预期路径为 `https://kxsvtrdestiny.github.io/comeet/`，是否实际上线以阶段验收记录为准，不能仅凭推送成功认定部署成功。

在 web 目录运行 `npm run build:pages`，生成 `artifacts/pages`；使用 `/comeet/` 资源前缀和 HashRouter，支持 `#/login`、`#/meetings/preview/prepare`、`#/meetings/preview/room` 等路由刷新。该模式明确提示静态预览不连接后端，并提供源码 ZIP。普通开发及默认生产构建不受该模式影响。

GitHub Pages 无法运行 Go/SQLite，因此静态站不能代替完整第一阶段 Windows 验收。LiveKit、DeepSeek 和账号功能仍未实现。若仓库尚未开启 Pages，请在 GitHub 仓库 Settings → Pages → Build and deployment，选择 Deploy from a branch，分支 gh-pages、目录 /(root)，保存。该操作与源码 push 分开。

源码已推送后可直接下载：https://github.com/kxsvtrdestiny/comeet/archive/refs/heads/stage1-review.zip 。Pages 开启与访问步骤见 [网页测试](docs/网页测试.md)。
