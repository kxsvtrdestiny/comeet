# Comeet · 第七阶段

第一至第五阶段及第六阶段本次7项人工检查已由用户确认通过。第六阶段剩余自动/进程检查已核对；本次仅新增DeepSeek纪要、任务/重试、保存查看与Markdown导出，等待第七阶段人工验收。不进入第八阶段、不公开部署。

**运行要求保持：Go >=1.26.8，Node.js 24。完整 Windows 更新命令、双账号检查与媒体边界见 [第七阶段运行与验收](docs/第七阶段运行与验收.md)。保留原 server/.env 和 server/data。**

源码分支 stage7-review（基于 stage6-room-fixes），项目在 week03/homework/comeet。更新或最终提交学校仓库时只取 **comeet 文件夹**，不改其他作业。第一至第三阶段分支保留。

## Windows 更新源码

先在原前端、后端终端分别 Ctrl+C。下载 https://github.com/kxsvtrdestiny/comeet/archive/refs/heads/stage7-review.zip ，解压后把新版 week03/homework/comeet 内部文件覆盖到旧 comeet 目录，**保留 server/.env 和 server/data，不清空旧目录**。重新安装依赖，不沿用旧 node_modules/dist。已有001–004迁移保留；本阶段启动自动追加尚未执行的005、006及007迁移，无需清库或手工运行SQL。

以下命令按用户现有目录 `D:\comeet-stage1-review\week03\homework\comeet` 编写；文件夹名带 stage1 不影响代码版本。若换了解压位置，替换命令路径即可。Pages 独立源码 ZIP 只含 comeet 文件夹。

工具：Node.js24（验证24.22.1/npm11.9.0）、Go1.25.3；SQLite纯Go，无需CGO、SQLite CLI或Docker。Web新增精确锁定 livekit-client2.23.0，其余版本见锁文件。

## 获取与填写 LiveKit（只在你本机操作）

1. 在 https://cloud.livekit.io/ 注册/登录，创建项目。
2. 在项目设置/API Keys中获取项目服务URL、API Key和API Secret。界面名称可能调整，以控制台为准；额度与费用由自己的服务账户决定。
3. 在**后端目录**用记事本打开配置：

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\server
notepad .env
```

保留已有 JWT_SECRET 等内容，找到并填写以下四行（通常同一主机名，分别使用https和wss，以控制台地址为准）：

```dotenv
LIVEKIT_API_URL=https://你的项目.livekit.cloud
LIVEKIT_WS_URL=wss://你的项目.livekit.cloud
LIVEKIT_API_KEY=控制台的API_Key
LIVEKIT_API_SECRET=控制台的API_Secret
```

保存并重启后端。**不要发密钥到聊天、截图或GitHub，不填web/.env，也不添加VITE_密钥变量。** .env.example只是模板，不在模板里填写真实秘密。浏览器只得到短期媒体token和可达的WS地址。

没有配置或配置不完整时，Go仍能启动，设备预览仍可检测，文字会议和聊天继续工作；准备页/会议室会明确提示媒体未配置。服务状态“已配置”仅代表配置形式完整，不表示网络/密钥已联通。私有部署及可选本机LiveKit步骤见 docs/LiveKit.md。

## 终端 A：后端 PowerShell（保持开启）

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\server
$env:GOPROXY = "https://goproxy.cn,direct"
go mod download
go run ./cmd/server -env-file .env -init-env
go run ./cmd/server -env-file .env
```

`-init-env`仅创建缺失文件/空JWT密钥，保留已有合格密钥及其他配置，不会打印秘密。看到server_start、stage=6后保持终端运行。后续启动通常只执行最后一行。显式-env-file才读文件，系统环境优先；不要旋转JWT_SECRET或删除data。

## 终端 B：前端 PowerShell（另开，保持开启）

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\web
npm.cmd ci
npm.cmd run dev
```

等npm ci完成再启动，不执行npm audit fix --force。浏览器访问 **http://localhost:5173**，不要混用127.0.0.1（Origin白名单必须匹配）。只要在localhost就可采集设备；普通HTTP局域网IP不保证安全上下文，跨电脑测试需要正确HTTPS配置。

A使用Chrome，B使用Edge或独立隐私窗口，各自登录。两端戴耳机，避免同房间扬声器啸叫。A创建会议，B按同一个9位会议号加入。

## 本阶段操作

- 准备页默认不采集。分别点开启麦克风/摄像头，独立授权，可选择设备，摄像头预览不发布。说话观察电平，点击扬声器试听播放约1.5秒音调，也可停止。设备标签授权前可能为空。
- 开关/设备选择相互独立；拒绝或缺设备不阻止“仅聊天参会”。关闭设备不会自动重新打开，设备断开后提示更换，不主动采集替代设备。
- 点击进入时先停止准备页采集，再由会议室按已选开关创建一套SDK轨道；仅聊天入口关闭两项采集意图。
- 正式业务入会后申请短期媒体token，连接LiveKit；分别显示业务和媒体状态。宫格按真实业务成员显示，摄像头/麦克风状态来自SDK轨道事件。
- 会议底部独立开关麦克风/摄像头；右侧“音视频设置”可切换设备、查看电平和重试媒体。浏览器阻止声音时点“点击启用声音”。本地视频始终静音，远端每条麦克风只挂载一个音频元素。
- 业务短暂重连不会重复连接媒体；媒体故障不关闭业务聊天。45秒业务恢复到期/登录失效会停止媒体。SDK自身媒体重连失败可手动重试。
- 离会按钮、返回工作台、路由跳转、登录失效、关闭/刷新页面会释放媒体；正常页面浏览器返回后若从缓存恢复会议页，会刷新验证会话。离会不等于结束整场会议。
- 本阶段已启用屏幕共享和主持人一次静音/移出/结束；DeepSeek 仍未开发。工作台不展示开发诊断，左侧设置可读真实服务状态。

## 终端 C：自动检查（另开 PowerShell）

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\server
go test -count=1 ./...
go vet ./...
go mod verify
cd D:\comeet-stage1-review\week03\homework\comeet\web
npm.cmd run typecheck
npm.cmd run build
npm.cmd audit
```

Go测试用临时SQLite，不改运行数据。Windows不要求安装C编译器跑race；云端Linux结果另记。可选浏览器检查会创建真实测试账号/会议，须保持A/B服务运行：

```powershell
cd D:\comeet-stage1-review\week03\homework\comeet\web
npx.cmd playwright install chromium
npm.cmd run test:e2e
# 仅当LiveKit配置完整、浏览器媒体网络可达时执行真实媒体检查
npm.cmd run test:media
```

test:e2e覆盖前三阶段与设备错误分支，媒体token缺配置的测试通过测试内故障注入稳定验证；test:media单独连接真实LiveKit，浏览器使用虚拟设备，不代表物理听感或跨网络验收。当前云Chromium策略禁止非代理WebRTC UDP，本次真实媒体传输未打通，详见阶段验收。

## 停止、重启与排障

终端A/B分别Ctrl+C停止。只重启后端时，在终端A的server目录执行 `go run ./cmd/server -env-file .env`，保持前端和浏览器；业务45秒内可恢复，媒体状态单独观察。不改.env/data。修改LiveKit配置后需要重启后端，会议室的音视频设置可重试媒体连接。

- 媒体未配置：检查server/.env四项、地址协议和是否重启后端；配置不完整/非安全公网地址会视为未配置。
- 媒体失败：检查项目URL、密钥是否同一项目、电脑网络/VPN/防火墙、LiveKit控制台连接记录。先保证文字连接成功；健康检查不测试LiveKit可达性。
- 权限拒绝：浏览器地址栏站点设置→允许麦克风/摄像头，然后点击重试开启。Windows设置→隐私和安全性→摄像头/麦克风也需允许桌面应用。
- 设备占用/缺失：关闭占用程序或连接设备，刷新列表/更换设备，或仅聊天。
- 听不到：点启用声音、检查系统输出/浏览器是否静音、对方是否开麦；只有支持setSinkId的浏览器提供扬声器切换，不支持时用系统默认。
- 新源码未生效：确认覆盖src及package-lock.json，停止前端，npm.cmd ci后重启，浏览器Ctrl+F5。
- Pages https://kxsvtrdestiny.github.io/comeet/#/ 只作静态界面预览，不运行Go、SQLite、业务WS或LiveKit，必须本机验收。

当前具体双账号清单见 docs/第七阶段运行与验收.md，历史清单见 docs/手工测试.md。**第七阶段完成后停止，等待真实DeepSeek人工验收，不进入第八阶段或公开部署。** 最终全部阶段结束后清理阶段提示，只交comeet文件夹。


第六阶段本次界面及结束会议修复、诊断步骤、三尺寸与双账号验收清单见 [会议室修复说明](docs/第六阶段会议室修复.md)。第六阶段本次7项已由用户确认通过；当前第七阶段仍待真实DeepSeek人工验收。


DeepSeek Key仅填写server/.env。未配置时不调用模型、不展示假纪要，仍可导出原始记录；任何纪要页面与导出均注明“不含语音转写”。具体私有配置、账户模型查询、输入预算、失败分类和真实验收步骤见[第七阶段运行与验收](docs/第七阶段运行与验收.md)。
