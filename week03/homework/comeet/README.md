# Comeet · 第三阶段

前两阶段已由用户明确验收通过。本次只完成第三阶段：WebSocket 成员、持久聊天、离会与业务重连；等待第三阶段人工验收。**不含音视频、共享或 AI。** 界面沿用已验收样式，未加入宣传标语。

源码位于仓库的 `week03/homework/comeet`。第三阶段源码分支为 `stage3-review`；第一阶段分支保留。最终交付学校仓库时只放 **comeet 文件夹**，不带当前仓库外层目录、不改变其他作业，详见 docs/交付检查.md。

## Windows 更新已有项目

以下命令按用户当前目录 **D:\comeet-stage1-review\week03\homework\comeet** 编写。如果实际解压目录不同，应替换路径；stage1-review 这个本机文件夹名不影响程序版本。

1. 在原前端、后端两个终端分别按 Ctrl+C 停止。
2. 下载 stage3-review 源码 ZIP，全部解压。打开新版的 week03/homework/comeet，将其文件复制到上述旧项目位置，替换同名源码。**不要先清空旧目录；保留 server/.env 和 server/data。** 不复制 node_modules/dist 等旧依赖产物到新版。
3. 按下列终端 A/B 命令运行。新迁移 003_realtime_chat.sql 自动执行，不删除既有数据库，不需要手动运行 SQL。

下载地址：https://github.com/kxsvtrdestiny/comeet/archive/refs/heads/stage3-review.zip

GitHub 源码 ZIP 保留仓库的 week03/homework/comeet 路径；Pages 提供的独立源码包只含 comeet 文件夹。两种情况都将 comeet 内部文件更新到既有目录，保留 .env/data。

## 工具版本

Windows 安装 Node.js 24（本次验证 24.22.1，npm 11.9.0）与 Go 1.25.3。新开 PowerShell 检查 `node --version`、`go version`。SQLite 使用纯 Go modernc 驱动，无需 SQLite CLI、CGO、Docker、LiveKit 或 DeepSeek Key。

Web：React/ReactDOM 19.2.0、React Router 7.18.5、Vite 7.3.8、TypeScript 5.9.3、Zustand 5.0.8、Lucide 0.548.0。后端版本由 go.mod/go.sum 锁定，包括 jwt/v5 5.3.0、x/crypto 0.43.0 的 bcrypt。依赖审计结果见阶段验收记录，不能用旧截图代替当前结果。

## 终端 A：后端（PowerShell，保持开启）

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\server
# 中国大陆网络访问默认 Go 下载源超时时，仅对本窗口切换；保持校验开启
$env:GOPROXY = "https://goproxy.cn,direct"
go mod download
# 自动从 .env.example 创建缺失的 .env，并仅为缺失/空 JWT_SECRET 生成私有随机密钥
# 若已有合格密钥则保留，不会重新生成，不会在输出中显示密钥
go run ./cmd/server -env-file .env -init-env
go run ./cmd/server -env-file .env
```

看到 `server_start` 且 stage 为 3，保持终端运行。地址为 127.0.0.1:8080。不要把生成的 .env 上传或复制给别人。密钥一经生成需保留，重启更换密钥会使已有 JWT 失效。`-init-env` 检测到已有非空但不足 32 字节的密钥时会报错，不会擅自覆盖，需要用户自行更换合格私有密钥。

显式 `-env-file .env` 才加载文件，进程系统环境优先；生产只读系统环境，不自动读取 .env。缺少有效 JWT_SECRET（32+ 字节）时启动会给出初始化提示，不能用仓库中的公开默认密钥。可选媒体/AI 未配置不阻止本阶段运行。

首次使用也执行上述相同步骤。后续一般只需在 server 目录执行最后一条启动命令，不用重复生成密钥；有依赖变化才重新下载。

## 终端 B：前端（另开 PowerShell，保持开启）

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\web
npm.cmd ci
npm.cmd run dev
```

npm ci 完成后才执行启动命令。若下载停滞可用 `npm.cmd ci --verbose` 查看原因，不执行 npm audit fix --force。5173 严格固定；端口占用应先停止自己之前的服务。

浏览器打开 **http://localhost:5173**。未登录应转到 `/login`。首次去 `/register` 自己注册账号，无内置账号、默认密码或演示会议。成功后登录，工作台按当前用户从 SQLite 加载真实会议。

用 Chrome 普通窗口注册账号 A，再用 Edge 或独立隐私窗口注册账号 B；不能把同浏览器同会话的两个普通标签当作独立账号环境。密码至少 8 个字符，UTF-8 不超过 72 字节。

## 本阶段如何操作

A 创建会议后进入准备页，点击“进入会议”；B 用自己的账号查询同一个 9 位会议号，点击进入。准备页本身不占席位，WebSocket 正式入会后才记录成员和参会区间。会议室支持真实成员列表、主持人/本人标识、双向纯文字聊天、历史加载、原消息重试和主动离会。

短暂掉线显示重连状态，45 秒宽限内自动申请新票据恢复同一参会区间并分页补齐消息；超过宽限后停止，按页面提示重新加入。复制同一会话的标签不会顶掉原健康连接。主持人个人离开不结束会议。账号退出会撤销当前登录，并关闭其业务连接。

工作台不再显示开发诊断，需查看时进入左侧“设置 → 服务状态”。麦克风、摄像头、共享仍禁用，本阶段无需任何媒体/AI Key，也不会申请设备权限。

## 停止与重启

两个终端分别 Ctrl+C。重启时终端 A 在 server 目录执行 `go run ./cmd/server -env-file .env`，终端 B 在 web 目录执行 `npm.cmd run dev`，浏览器刷新。保留 .env 和 data；正常重启不会删除用户、会议、聊天或未过期登录会话。测试业务恢复时只重启终端 A，保持 B 和浏览器开启，在 45 秒内恢复后端；不要更换密钥、清数据库或重启前端。超过恢复宽限则手动重新加入。尚未正式入会的预留只有 60 秒。

## 终端 C：可执行检查（PowerShell）

保持 A/B 服务运行，另开终端：

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\server
go test -count=1 ./...
go vet ./...
go mod verify
go build -o .\data\comeet.exe ./cmd/server

cd D:\comeet-stage1-review\week03\homework\comeet\web
npm.cmd run typecheck
npm.cmd run build
npm.cmd audit
# 可选真实浏览器回归，会在当前数据库创建名称带 e2e/ui 前缀的测试账号和会议
npx.cmd playwright install chromium
npm.cmd run test:e2e
```

Go 接口测试自己创建临时数据库，不使用用户运行数据；不需要服务 A/B。Playwright 则需要 A/B 运行并真正请求其 API，不使用 mock 账号或假会议。Windows 未安装 C 编译器时无需强行运行 race；Linux 云端实际 race 结果见记录。

## GitHub Pages 与本机区别

https://kxsvtrdestiny.github.io/comeet/#/ 仅为静态界面预览，无法运行 Go/SQLite。静态页面禁用账号提交和创建能力，明确未连接后端，不展示假 health 或假会议。**第三阶段账号、权限、持久化必须在 localhost 的完整前后端上验收，不能在 Pages 上验收登录。** Pages 是否实际完成公网部署以用户浏览器为准；本云环境对 github.io/管理 API 有访问限制。

web 目录的 `npm.cmd run build:pages` 使用 HashRouter 与 /comeet/ 资源前缀。普通 `npm.cmd run build` 保持 BrowserRouter。云端内网开发可用 `npm run dev:cloud`，这不是生产托管或公网部署。

## 常见问题

- JWT_SECRET 报错：在终端 A 执行上面的 -init-env 命令，不要直接复制别人的密钥；已有有效值不会旋转。
- health 失败：确认终端 A 保持开启，访问 http://localhost:8080/api/v1/health/ready。预期 schemaVersion 为 003_realtime_chat.sql。
- 注册重复用户名：大小写规范化后仍唯一；换名字或用原账号登录。
- 登录错误统一提示用户名或密码错误；不会说明账号是否存在。
- 详情 FORBIDDEN：当前账号既不是主持人，也没有正式参会记录；仅查询会议号只能看准备信息，正式入会后才有成员及聊天读取权限。
- ALREADY_JOINED / MEETING_FULL / SESSION_EXPIRED：预留接口的重复会话、容量或过期保护；不应通过清空数据库来“修好”。
- 429：账号和会议号查询有基础限流，稍后重试；不盲信 X-Forwarded-For。
- 网络下载失败：检查 Go/NPM 的下载源与本机网络，不关闭 TLS、GOSUMDB 或锁文件校验。
- 页面像旧版本：确认已覆盖新版 web/src、web/package.json 和 package-lock.json，停止前端后 npm.cmd ci，再启动并 Ctrl+F5。

报错请提供终端 A/B/C 中哪一个、当前目录、执行命令、错误输出和访问路径。不要包含 .env、Key、密码或 JWT/token。

详细 API、阶段结果及 8 项人工检查分别见 docs/API.md、docs/阶段验收.md、docs/手工测试.md。只完成第三阶段，用户验收通过前不进入第四阶段。
