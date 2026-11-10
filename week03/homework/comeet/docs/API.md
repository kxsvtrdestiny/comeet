# 当前已实现 API · 第二阶段

统一前缀 /api/v1；响应 `{code,message,data,requestId}`，错误 data=null。鉴权用 `Authorization: Bearer <JWT>`，不把长期 JWT 放 URL。请求体最大 1MiB，未知 JSON 字段和多个对象被拒绝。时间为 UTC Unix 毫秒，JWT 标准时间字段按协议为秒。

| 方法 / 路径 | 权限与实际行为 |
|---|---|
| GET /health/live | 公开；进程存活 |
| GET /health/ready | 公开；实际查询迁移，stage=2、schemaVersion=002_accounts_meetings.sql；DB 故障 503，可选媒体/AI 未配置不影响 200 |
| POST /auth/register | 公开；username/displayName/password；201 返回 user，不自动登录 |
| POST /auth/login | 公开；username/password；200 返回 token/expiresAt/user |
| POST /auth/logout | 当前身份；撤销当前 sid，释放其 joining 预留；200 data=null |
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

未实现 /ws、ticket、resume、媒体 token、聊天、离会/会控/结束会议、历史内容、笔记或 AI。测试临时 DB 可以设置 ended/ending/ban/member fixture 验证权限，正常链路不插入假参会历史。
