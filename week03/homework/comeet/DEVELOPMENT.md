# 开发约定 · 第五阶段

依据 docs/开发规格-v2.0.md。第一至第四阶段已由用户确认验收通过；当前仅授权第五阶段共享与会控，完成后停止等待验收，不开发第六阶段或 DeepSeek。截图、报错、继续修复不算下一阶段授权。

所有项目文件在 week03/homework/comeet，未改其他作业。Web 使用 React 19.2.0、TypeScript 5.9.3、Vite 7.3.8、React Router 7.18.5、Zustand 5.0.8；Go 1.26.8、chi 5.2.3、jwt/v5 5.3.0、x/crypto 0.43.0、modernc SQLite 1.39.1。精确依赖以锁文件为准。

server/internal/auth：bcrypt/JWT/持久会话/撤销；meeting：会议号、对象权限、游标、幂等创建、原子预留；http：鉴权、中间件、稳定错误码、JSON DTO、限流；store/migrations：单连接 SQLite、WAL/外键/超时、内嵌迁移。003 为追加迁移，不改已应用的 001/002，不清空已有数据。

web/services：类型、fetch、认证失效与请求；stores/auth：Zustand 管理当前标签 sessionStorage 登录，不重复缓存会议列表；Protected：刷新验证 /me、到期计时、登录返回地址；pages：真实账号/工作台/详情/受限准备。纯文本渲染，不注入 HTML。Pages 模式禁用业务，不填假数据。

-init-env 只为缺失/空本地密钥生成随机值，保留其他配置及既有合格密钥，不打印秘密。生产读系统环境；显式 -env-file 才读文件。事务不调用外部网络。恢复凭证由随机 UUID 与 HMAC 派生以复现幂等重试，库内只存 hash。joining 仅预留，第三阶段 room.join 成功后写成员和参会区间。

Go testing/httptest 使用临时真实 SQLite，覆盖认证/对象权限/竞争/重开数据库；race 在 Linux 验证，Windows 编译不依赖 CGO。Playwright 真正请求 Go，创建 e2e/ui 前缀测试账号及会议，不能当成正常链路种子或推荐人工固定登录。Pages 产物本地 Hash 路由检查不等同公网发布。结果见 docs/阶段验收.md；Windows 具体命令在 README 和手工测试中。

当前已有 WS/成员/持久聊天/离会/重连，第四阶段已有媒体代码，未开发会控/结束、AI。测试 fixtures 可设置 ended/ending/ban/member 来测权限，不代表运行中实现了后续业务。无 Key 时不声称第三方真实联调通过。

不再加宣传标语或英文眉题，保留功能标题与必要状态。全部阶段完成后清理 UI 阶段编号、骨架/预览文字和临时导航，保留真实权限、配置、错误和 AI 来源提示；阶段历史文档保留。最终只将 comeet 文件夹放进学校仓库指定位置，不附带 week03/homework 或源仓库外壳、不改其他作业。见 docs/交付检查.md。

源码 stage4-review，第一阶段 stage1-review 保留；此前授权的本仓库源码与测试站 push 用于本阶段交付，学校仓库操作仍按最终指定目标执行。

第三阶段 internal/realtime 管理一次性票据、连接 generation、成员/聊天事务和重启恢复；单读/单写、有界发送队列，网络等待不持有全局锁。45 秒恢复宽限不因重试/重启反复延长；旧连接回调不能删除新连接。前端 room.ts 按 epoch 丢弃旧回调，先订阅快照高水位再分页补拉，消息原 ID 重试。详细协议见 API，实际检查见阶段验收。

第四阶段新增realtime/media.go及在线会话媒体授权，使用已有jwt/v5按官方LiveKit字段签名。客户端锁定livekit-client2.23.0。设备准备用独立gUM、代次取消迟到结果；进入前停止准备轨道，SDK重新采集。MediaClient以当前参会sessionId为唯一身份，StrictMode延迟启动、终止标记、串行断开、有限监听和detach清理；音视频错误不终止业务WS。deviceTools负责AnalyserNode与试听AudioContext释放。

真实媒体自动测试单独test:media；普通test:e2e不要求第三方Key，错误分支测试有明确故障注入。当前云Chromium策略禁止非代理WebRTC UDP，真实媒体传输待验证；不能写音视频已联调通过或绕过组织策略。物理设备/Cloud/跨网络验收待用户，第四阶段完成后停止。

## 第四阶段会议室界面调整（用户已确认验收通过）

新附件 `docs/Comeet会议室界面修改说明.md` 覆盖原深色/仅宫格约定；运行与人工清单见 `docs/会议室浅色改版验收.md`。真实会议室浅色、默认聚焦、稳定 session+source 本地选画面、成员/聊天默认收起、连接详情与设备对话框。`services/roomView.ts` 为纯 UI 选择规则，不调用媒体连接/发布。媒体 `MediaClient`、WS `RoomClient` 与后端保持原实现；音频统一由独立 AudioSubscriptions 挂载一次。`app/room.css` 在旧全局样式之后加载，限定 live-room 避免改变已验收的其他页面。共享仅预留来源类型，尚无采集/发布。不得自动宣布第四阶段验收通过或进入第五阶段。


## 第五阶段实现约定

以本节和 docs/第五阶段运行与验收.md 为当前状态；上文第四阶段的“未开发/待验收”属于历史边界，用户本次已明确验收并授权第五阶段。Go1.26.8、官方 server-sdk-go/v2 2.19.0（含明确 RevokeTokenTs），精确传递依赖见 go.mod/go.sum。屏幕仅发布视频，不发布系统声音；不远程开麦。媒体来源按 session+camera/screenShare 分开，成员列表不重复人数。自动选择共享优先且稳定，手动选择优先级更高。

004 新迁移只加 media_grants 与 moderation_operations，保留已有数据。控制入口 REST 认证并核对 host_user_id，静音由服务端取 Microphone TrackSid；移出先ban/终止业务再媒体清理；结束 active→ending→媒体移出/删房→ended。失败返回非2xx、持久 pending，可从会控结果和会议详情重试；启动执行有界重试，不做第六阶段定时自动结束。网络等待不持 realtime mutex 或 SQLite 事务。一次静音按 UUID 去重，移出/结束按业务目标合并。

分享生命周期测试明确模拟采集和发布传输，普通 E2E 使用真实 Go/SQLite/WS。真实第三方测试在 test:media，包括旧 token 直接重入验证；缺少服务配置时必须记待验证。第四阶段用户验收事实和本阶段云测试证据分别记录，不把前者扩大为本阶段真实 SFU 通过。不要自动进入下一阶段。
