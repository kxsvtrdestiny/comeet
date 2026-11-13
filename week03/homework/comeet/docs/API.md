# 当前已实现 API · 第四阶段

统一前缀 /api/v1；响应 `{code,message,data,requestId}`，错误 data=null。鉴权用 `Authorization: Bearer <JWT>`，不把长期 JWT 放 URL。请求体最大 1MiB，未知 JSON 字段和多个对象被拒绝。时间为 UTC Unix 毫秒，JWT 标准时间字段按协议为秒。

| 方法 / 路径 | 权限与实际行为 |
|---|---|
| GET /health/live | 公开；进程存活 |
| GET /health/ready | 公开；实际查询迁移，stage=4、schemaVersion=003_realtime_chat.sql；DB 故障 503，可选媒体/AI 未配置不影响 200 |
| POST /auth/register | 公开；username/displayName/password；201 返回 user，不自动登录 |
| POST /auth/login | 公开；username/password；200 返回 token/expiresAt/user |
| POST /auth/logout | 当前身份；撤销当前 sid，释放预留并关闭其在线业务连接；200 data=null |
| GET /me | 当前身份；id/username/displayName |
| POST /meetings | 当前身份；title 可选，clientRequestId 必填 UUID；201 返回会议，重复同内容返回原会议 |
| GET /meetings | 自己创建或实际参加的会议；status/keyword/cursor/limit（默认 20，1–100） |
| POST /meetings/resolve | 当前身份；meetingNo 支持空格/连字符，规范化后 9 位数字；最小准备信息 |
| GET /meetings/:id/prepare | 当前身份；最小准备信息；不产生参会记录或占席位 |
| GET /meetings/:id | 仅主持人或实际参会成员；其他用户 403 |
| POST /meetings/:id/join | 当前身份；clientRequestId UUID；仅预留 60 秒，返回 sessionId/resumeCredential/expiresAt |

user：`{id,username,displayName}`。
meeting：`{id,meetingNo,title,hostUserId,hostName,status,startedAt,endedAt}`。
preparation：`{id,meetingNo,title,hostName,status}`，不含成员、聊天、笔记或 hostUserId。
list：`{items,nextCursor,hasMore}`，started_at DESC/id DESC 游标分页，keyword 作标题/会议号字面搜索。

注册字段示例（不是内置账号）：

```json
{"username":"student_a","displayName":"同学 A","password":"用户自行选择的密码"}
```

clientRequestId 由客户端 crypto.randomUUID() 生成。创建重试沿用同 ID 和标题，换内容返回 IDEMPOTENCY_CONFLICT。join 同用户/会议/登录 sid/请求重试返回原预留且不延长有效期；不同请求冲突 ALREADY_JOINED，到期原请求 SESSION_EXPIRED，另一个登录 sid 不能接管旧预留。

预留在短事务中检查 active、sid 有效、封禁、同用户唯一性和容量。到期 joining 或绑定登录已到期/撤销的预留，在下一次 join 事务中惰性释放。单连接/短事务令容量计数和插入原子，部分唯一索引再次限制同用户有效会话。预留不写 meeting_members，不获得详情或历史权限。恢复凭证由随机 session UUID 和服务器密钥的域分离 HMAC 推导，保证幂等重试可重现；数据库只存其 SHA-256，不存原始凭证。

JWT HS256 含 sub/sid/iss/aud/iat/exp。每次校验算法、签名、时间、iss/aud 和 auth_sessions；库内不存明文 JWT，默认 8 小时，不做刷新令牌。退出仅撤销当前 sid。登录响应及私有 API Cache-Control:no-store。用户名 ASCII 3–32、规范化大小写唯一；显示名 1–32 Unicode 字符；密码至少 8 字符且 UTF-8 最多 72 字节，bcrypt cost=10。

注册+登录共享每来源 IP 每分钟 30 次；resolve 独立每来源 IP 每分钟 30 次。使用 RemoteAddr、不信任 X-Forwarded-For；429 带 Retry-After:60。单实例有界内存计数，重启重置，后续聊天/AI 限流在对应阶段添加。

业务错误码：VALIDATION_ERROR、UNAUTHORIZED、SESSION_EXPIRED、USERNAME_TAKEN、FORBIDDEN、MEETING_NOT_FOUND、MEETING_ENDED、MEETING_ENDING、MEETING_FULL、ALREADY_JOINED、REMOVED_FROM_MEETING、IDEMPOTENCY_CONFLICT、RATE_LIMITED、MEETING_NUMBER_UNAVAILABLE、DATABASE_UNAVAILABLE、INTERNAL_ERROR。未知端点 404 NOT_FOUND，错误方法 405 METHOD_NOT_ALLOWED。

## 第三阶段新增 REST 与 WebSocket

以下 REST 路径仍在 /api/v1 下，均要求 Bearer JWT；恢复/票据/离会绑定本人、会议和原登录 sid，不能跨账号接管。

| 方法 / 路径 | 输入与返回 |
|---|---|
| POST /meetings/:id/resume | `{sessionId,resumeCredential}`；验证原恢复凭证、宽限和身份，返回恢复会话；健康连接冲突 ALREADY_JOINED，不顶原连接 |
| POST /meetings/:id/ws-ticket | `{sessionId}`；返回 `{ticket,expiresAt}`，30 秒且不超过登录有效期，一次性、同会话新票据替换旧票据 |
| POST /meetings/:id/leave | `{sessionId}`；幂等离会，结束参会区间，广播成员 |
| GET /meetings/:id/members | 主持人或实际参会成员可读；返回当前 online/reconnecting 成员 |
| GET /meetings/:id/messages | 同上；limit 默认50/最大100，beforeId 正整数向前查历史，afterId 非负整数向后补拉，不可同时传；返回 `{items,nextCursor,hasMore}`，items 按 ID 升序 |

消息：`{id,clientMessageId,senderId,senderName,content,createdAt}`。成员含 userId/sessionId/displayName/state/isHost；“本人”由前端当前 userId 判断。仅入会预留不获得成员/消息权限。

WebSocket 地址 `/ws?ticket=一次性票据`（不在 /api/v1 下）；只允许 ticket 查询参数，长期 JWT 不放 URL。升级前严格校验 Origin 白名单，再原子消费票据；同票据第二次失败。升级本身不写参会记录。客户端须在 5 秒内发送：

```json
{"v":1,"type":"room.join","requestId":"客户端UUID","payload":{}}
```

随后客户端可发送 chat.send（payload 为 `{clientMessageId:UUID,content:纯文本}`）、room.leave（payload 为 `{}`）、ping。统一客户端帧字段 v/type/requestId/payload；服务端事件增加 eventId/meetingId，响应带 requestId。服务端事件包括 room.snapshot、member.joined/member.updated/member.left（均含完整成员列表）、chat.created、chat.ack、pong、error。

room.snapshot 含 meeting/members/sessionId/generation/latestMessageId。先注册订阅再取高水位；前端按 afterId 分页补至该水位，同时合并期间收到的实时消息。冷进入显示最近50条，可加载更早历史；不能以最近50条替代断线补齐。

消息限制 2000 Unicode 字符且不超过8KiB，整个帧最多16KiB；每连接每分钟最多120次聊天请求。服务端决定发送人和时间；SQLite 提交后才广播和 ACK。唯一键为会议/用户/clientMessageId；重试始终沿用原 ID，返回原服务端消息，不再次广播。前端按服务端 ID 去重，8秒未确认可用原 ID 重试，纯文本渲染不执行 HTML。

控制 Ping 每10秒、等待 Pong 5秒；写超时5秒，有界64条发送队列，慢连接断开不阻塞房间。JWT 到期/撤销会关闭连接。非主动断开进入45秒恢复宽限，重试不延长宽限；前端按1/2/4/8/10秒并加抖动退避，超时停止并显示重新加入。主动离会结束旧区间，重新加入创建新会话。主持人个人离开不等于结束会议。

启动把旧 online 标为 reconnecting；保留原已断线起点，清理过期会话并结束区间。正常退出保留恢复机会，不结束会议。generation 和连接指针保护旧连接回调，防止误删新连接；原会话恢复不新增参会区间。消息及区间持久化在003迁移，票据不持久化，重启须申请新票据。

第四阶段实现媒体 token/音视频，未实现共享、会控/结束会议、笔记或 AI。不记录密码、JWT、票据、恢复凭证和聊天正文日志。测试 fixture 不进入正常数据链路。

## 第四阶段媒体接口

GET /api/v1/media/config：已登录可读，返回 `{configured:boolean}`，不返回Key/Secret。形式完整不是服务探活。

POST /api/v1/meetings/:id/media-token：Bearer JWT，输入仅 `{sessionId}`。检查该会话归属当前user/sid/会议、会议active、未封禁、真实online及活动peer/generation，未正式入会返回409 BUSINESS_NOT_CONNECTED；缺配置503 MEDIA_NOT_CONFIGURED，文字业务不受影响。成功返回 `{url,token,expiresAt,identity,room}` 并Cache-Control:no-store；服务端不记录token。

JWT HS256，iss=LiveKit API Key，sub=参会sessionId，name/metadata取服务端；video grant仅roomJoin、指定内部会议room、camera/microphone发布和订阅。canPublishData/canUpdateOwnMetadata=false，无roomAdmin/共享/建房权限。TTL最大5分钟且受请求JWT/库内登录期限约束。具体授权有效期及已建立连接边界见LiveKit.md。第四阶段不调用管理API，不声称token过期会自动踢出现有连接。


## 第五阶段共享与会控

媒体 token 新增 screen_share 发布源（不含 screen_share_audio），仍只绑定服务器生成的当前在线参会 sessionId，无 roomAdmin。

- POST `/api/v1/meetings/:id/members/:userId/mute`：主持人一次静音；请求 `{ "requestId": "UUID" }`，服务端查询麦克风 TrackSid。没有麦克风返回 409 MICROPHONE_NOT_PUBLISHED；不接受客户端 sid/muted/role，不远程开麦。
- POST `/api/v1/meetings/:id/members/:userId/remove`：同上请求，持久封禁本场并终止目标会话，完成媒体移出后才返回 OK。
- POST `/api/v1/meetings/:id/end`：同上请求，主持人可以离会后调用。先 ending，媒体清理成功才 ended。重试使用原 requestId，刷新后也可使用持久操作 ID。
- GET `/api/v1/meetings/:id/controls`：仅主持人，返回 pending 数组 `{ id, action, targetUserId }`，支持刷新后重试。

成功 data 为 `{operationId,state:"completed"}`。普通成员 403 HOST_REQUIRED；媒体依赖失败 502 MEDIA_CONTROL_FAILED、缺配置 503 MEDIA_NOT_CONFIGURED；均不当作成功。移出后 join/resume/ws-ticket/media-token 拒绝；ending/ended 同样拒绝加入/恢复/授权。重复成功操作幂等，operationId 与其他参数冲突返回 409 REQUEST_CONFLICT。已提交业务状态不会因媒体失败回滚。

目标成功静音收到 `member.muted`，payload `{message:"主持人已将你静音，你可以自行开麦"}`。移出收到 REMOVED_FROM_MEETING，结束中收到 MEETING_ENDING 并停止媒体；主持人保留结束重试入口，其他成员返回工作台。Go→LiveKit 使用有界超时和明确撤销截止时间，真实旧 token 撤销以所用 Cloud 服务验收为准。
