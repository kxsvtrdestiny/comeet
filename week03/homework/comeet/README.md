> 最终源码分支：`final-review`。用户已明确授权推送该分支用于下载；不部署应用。
> [下载最终源码ZIP](https://github.com/kxsvtrdestiny/comeet/archive/refs/heads/final-review.zip) · [备用直链](https://codeload.github.com/kxsvtrdestiny/comeet/zip/refs/heads/final-review)
> ZIP中只取 `week03/homework/comeet`，升级保留原server/.env和server/data。

# Comeet

面向2–6人的课程会议应用：账号与会议权限、成员/聊天、音视频与屏幕共享、主持人会控、历史记录、个人笔记，以及基于聊天和已保存笔记的DeepSeek纪要与Markdown导出。

**用户已确认真实 DeepSeek 成功生成与人工核对，以及当前版本的 Windows、物理设备和真实媒体复验通过。** 第一至第六阶段已有人工确认与自动检查继续保留，验收记录已更新。本云自动测试与用户实机验收分别记录，不扩大为所有设备、网络或费用场景均验证。完整系统未公开部署；用户已另行授权将最终源码推送final-review用于下载。

- [幻灯片式技术方案](方案.md)：用 `---` 分页，每页一个主题，含真实运行截图与Mermaid架构图；可直接在支持Mermaid的Markdown阅读器查看。
- [最终检查结果与人工验收清单](docs/最终验收与交付.md)
- [阶段历史证据](docs/阶段验收.md)、[DeepSeek私有配置与诊断](docs/第七阶段运行与验收.md)

## 交付目录与数据保留

仓库项目位置为 `week03/homework/comeet`。最终需要提交的应用内容全部在此文件夹内，不改其他作业。当前源码交付分支为final-review；远端旧分支/旧ZIP不包含最终收尾。目标学校仓库尚未指定，本次只推送现有仓库的新分支。

```text
comeet/
  方案.md                 Markdown幻灯片式方案
  README.md / DEVELOPMENT.md
  web/                    React源码、package-lock.json、lint和浏览器测试配置
  server/                 Go源码、go.mod/go.sum、.env.example
    migrations/           001–007增量迁移，随程序内嵌
  docs/                   配置、验收、API与截图
  tests/                  浏览器测试和进程恢复探针
```

**升级前在两个终端Ctrl+C停止前后端，备份原 `server/.env` 与整个 `server/data`。** 覆盖新版源码时保留它们，不清空旧comeet目录。停服务后再复制数据目录，避免漏掉SQLite WAL/SHM文件。

启动会自动执行尚未应用的001–007迁移并记入schema_migrations；不修改旧迁移、不删库。已有账号、会议、聊天、笔记、媒体清理检查点及纪要任务保留。本次收尾不增加迁移。health的schemaVersion为007_deepseek_summaries.sql；stage=7表示业务已实现到纪要，第八阶段为交付检查，不是新增业务能力。

## Windows 安装与首次启动

要求：**Node.js 24、Go >=1.26.8**（以server/go.mod为准），Chrome/Edge。SQLite使用纯Go驱动，不需要另装SQLite服务、Docker或C编译器。

下面完整路径沿用你的目录；若项目直接放在 `D:\comeet`，将命令分别替换为 `D:\comeet\server` 与 `D:\comeet\web`。

终端A，后端PowerShell：

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\server
go version
# 已有.env时保留；仅全新安装复制模板：
if (!(Test-Path .env)) { Copy-Item .env.example .env }
go mod download
# 只补缺失/空JWT密钥，不覆盖有效旧配置、不打印秘密：
go run ./cmd/server -env-file .env -init-env
go run ./cmd/server -env-file .env
```

看到 `server_start` 后保持窗口开启。程序默认监听127.0.0.1:8080，数据在server/data。`-env-file .env`必须显式传入；操作系统环境变量优先于文件。不要因为更新源码重新生成JWT_SECRET。

终端B，前端PowerShell：

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\web
node --version
npm.cmd ci
npm.cmd run dev
```

打开 **http://localhost:5173**。不要混用127.0.0.1；PUBLIC_ORIGIN与ALLOWED_ORIGINS需匹配。两个终端都保持开启。重装依赖后重启Vite，再在浏览器Ctrl+F5。

## LiveKit配置：只编辑后端私有.env

从自己的LiveKit项目控制台获取服务地址、Key、Secret，分别填写：

```dotenv
LIVEKIT_API_URL=https://你的项目.livekit.cloud
LIVEKIT_WS_URL=wss://你的项目.livekit.cloud
LIVEKIT_API_KEY=仅在本机填写
LIVEKIT_API_SECRET=仅在本机填写
```

Go通过HTTPS访问管理API，浏览器通过WSS/WebRTC连接SFU。地址、Key、Secret必须属于同一项目。缺配置仍可使用文字会议；设备默认关闭，不擅自采集。健康状态“已填写”不代表管理API或媒体链路已经联通。

密钥不放web/.env、不使用VITE_密钥变量、不上传聊天或Git。浏览器只获得后端绑定当前在线会话的短期媒体token。具体网络与设备说明见 [LiveKit](docs/LiveKit.md)。localhost可作为采集安全上下文；跨电脑使用普通HTTP局域网IP不保证设备采集，需要正确HTTPS和Origin配置。本次未进行该部署。

## DeepSeek配置：不含语音转写

在同一个server/.env中设置，Key自行在平台获取，不需要发送给开发者：

```dotenv
DEEPSEEK_ENABLED=true
DEEPSEEK_BASE_URL=https://api.deepseek.com
DEEPSEEK_MODEL=填写账户可用且支持JSON输出的模型ID
DEEPSEEK_API_KEY=仅在本机填写完整密钥
DEEPSEEK_TIMEOUT_SECONDS=90
DEEPSEEK_MAX_OUTPUT_TOKENS=2048
DEEPSEEK_MAX_SOURCE_CHARS=20000
DEEPSEEK_MAX_SOURCE_BYTES=80000
```

占位文字不能原样使用。**模型ID不是Tracking ID；密钥不是页面带星号的展示值。** 用户此前通过 `/models` 查到deepseek-flash及deepseek-v4-pro；模型可用不代表生成结构已通过校验。安全查询步骤和输入预算见 [第七阶段说明](docs/第七阶段运行与验收.md)。

保存后重启Go。未配置时清楚提示、不生成假纪要，仍可导出原始记录。入口在“会议记录 → 详情 → DeepSeek会议纪要”；仅会议完全结束后主持人可确认生成。会将本场真实聊天和已保存手动笔记发送至DeepSeek，可能产生费用；不包含未保存草稿、音频或语音转写。

模型与输入预算从普通页面隐藏，后端限制、来源校验和导出中的模型/生成时间仍保留。模型结果需要人工核对。AI_INVALID_RESULT会显示安全规则/字段位置；旧失败任务不会被新代码改写，不能连续点击重试期待自动修复。

## 双账号运行与主要操作

A使用Chrome，B使用Edge或独立隐私窗口，分别注册/登录。A创建会议，B用同一9位会议号加入。戴耳机避免同室回声。可先仅聊天，再明确开启设备。

- 准备页分别开启麦克风/摄像头、选设备；进入会议前释放预览，会议室重新采集并发布。
- 聚焦/宫格切换、点击来源固定主画面；自动优先共享，手动固定不被抢占，停止共享后回退。共享完整显示、不镜像。
- 成员/聊天/笔记共用面板；切换与关闭保留未保存笔记草稿，保存后刷新恢复。冲突要明确选择服务器版或保留本地再保存。
- 主持人可一次静音、移出、结束；成员可自己再开麦。移出/结束先确认，媒体失败不会报成功；未完成项在成员面板/详情可重试。
- 个人离会不结束整场会议。结束后在历史查看真实参会区间、聊天、笔记和纪要；知道会议号或仅预留不授予历史访问权限。

完整端到端人工检查见 [最终清单](docs/最终验收与交付.md)，上述真实服务与当前实机复验已获用户确认。

## 停止与重启

在终端A/B分别按 **Ctrl+C** 停止。重新运行各自的启动命令即可，无需再次init-env或删除data。修改后端.env后必须重启Go。

普通后端重启保留登录、成员恢复凭证、聊天和笔记；业务断线默认45秒恢复宽限。超过期限需明确重新入会。生成中的纪要在退出/重启后标记失败或结果未确认，由用户明确重试；不承诺中断请求未被上游计费。

## 自动检查与构建

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\server
go test -count=1 ./...
go vet ./...
go mod verify
go build -o .\data\comeet.exe ./cmd/server
cd ..\web
npm.cmd ci
npm.cmd run lint
npm.cmd run typecheck
npm.cmd run build
npm.cmd audit
```

Go测试使用临时数据库，不改日常运行库。Linux额外执行race；Windows运行race需要额外C工具链，不作为普通安装要求。lint覆盖web/src，使用ESLint/TypeScript推荐规则；SDK动态适配允许显式any，暂未启用React Hooks专门规则，不能将lint通过理解为全部行为已验证。

浏览器普通套件会创建测试账号/会议，且未配置AI用例要求测试服务没有真实Key。建议使用隔离实例：先停止日常8080后端，在server目录复制 `.env.example` 为 `.env.test`，对该文件运行init-env，并设置该终端 `$env:DATABASE_PATH = "./data/automated-check.db"`，以 `go run ./cmd/server -env-file .env.test` 启动。模板保持LiveKit为空、DEEPSEEK_ENABLED=false。日常.env/data不删除；测试完成关闭这个终端，再用原终端/原.env启动。

保持隔离后端和前端运行，在第三个终端：

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\web
npx.cmd playwright install chromium
npm.cmd run test:e2e
# 只有配置真实LiveKit且浏览器网络可达时，单独执行：
npm.cmd run test:media
```

普通套件包含测试fixture的设备/媒体生命周期和AI成功/失败状态，不冒充真实SFU或DeepSeek。独立媒体套件也使用虚拟设备，不能代替物理设备听感。实际结果与限制见最终检查记录。

## 常见问题

| 现象 | 检查方法 |
|---|---|
| go mod download超时/stream error | 是依赖下载链路问题；可在当前PowerShell临时设置 `$env:GOPROXY="https://goproxy.cn"` 后重试，不删库、不盲目清整个缓存 |
| npm脚本被PowerShell策略拦截 | 使用npm.cmd/npx.cmd，无需修改全系统执行策略 |
| 8080/5173占用 | 停止旧服务，只启动一套；Vite固定5173，不能静默换端口 |
| 页面还是旧版 | 确认覆盖正确comeet目录，停止Vite、npm.cmd ci、重启并Ctrl+F5 |
| 权限拒绝/设备占用 | 浏览器站点设置和Windows隐私允许采集；关闭占用程序，手动切换设备或仅聊天 |
| 听不到声音 | 检查输出/浏览器静音、对方麦克风；浏览器拦截自动播放时点击启用声音 |
| 长期“结束中” | 看失败步骤和诊断编号；核对Go到LiveKit管理API链路，恢复后从原待处理项继续；不手改ended、不删库 |
| AI未配置/请求被拒绝 | 核对启用、完整Key、模型ID和基址，重启Go；不要填写Tracking ID |
| AI_INVALID_RESULT | 记录规则、字段位置、诊断编号；不能据此认定Key错误，不能把未校验结果算成功 |
| 输入超限/429 | 保留后端预算；缩短造成超限的笔记或导出原记录，按提示稍后重试，不自动截断或无限调用 |

本次仅按用户授权推送final-review源码，不公开部署应用。历史GitHub Pages仅是旧静态预览，不能代表本次完整应用已经上线。真实服务与当前实机复验已由用户确认；目标学校仓库提交仍等待用户指定与授权。
