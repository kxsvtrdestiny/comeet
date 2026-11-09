# 当前已实现 API（第 1 阶段）

统一响应 `{code,message,data,requestId}`，requestId 为服务端生成 UUID，另通过 X-Request-ID 返回。失败 data 为 null，Content-Type 为 application/json。没有账号、JWT 或会议端点。

| 方法 | 路径 | 结果 |
|---|---|---|
| GET | /api/v1/health/live | 200 OK，data.status=alive |
| GET | /api/v1/health/ready | 数据库可查询迁移版本时 200；故障 503 DATABASE_UNAVAILABLE |

ready 示例（字段来自实际配置与数据库，不是会议数据）：

```json
{"code":"OK","message":"success","data":{"database":"ready","schemaVersion":"001_framework.sql","mediaConfigured":false,"aiConfigured":false,"stage":1},"requestId":"服务端生成的 UUID"}
```

媒体/AI configured 仅检测必要配置齐全（AI 还需启用），不验证真实服务。未知路由 404 NOT_FOUND，已存在路由错误方法 405 METHOD_NOT_ALLOWED。请求体限制 1MiB。当前只支持开发同源代理，ALLOWED_ORIGINS 已读取，跨域策略及 WS Origin 校验待业务接入时实现。

完整未来契约在开发规格中，不能将其当作已实现 API 清单。
