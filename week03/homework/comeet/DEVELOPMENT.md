# 开发约定与阶段边界

当前依据为 docs/开发规格-v2.0.md，完整替代此前要求。本阶段只建立工程基础。用户明确验收第 1 阶段通过后才可开发第 2 阶段；报错、截图或“继续修复”均不算下一阶段授权。

目录：web 使用 React 19.2.0、TypeScript 5.9.3、Vite 7.3.8、React Router 7.18.5、Lucide 0.548.0；server 使用 Go 1.25.3、chi 5.2.3、godotenv 1.5.1、modernc SQLite 1.39.1。锁定 package-lock.json、go.mod/go.sum，不引入尚未使用的后续依赖。

当前无 AGENTS.md，仓库起始为空。全部项目文件在本作业目录；无需另建 worktree，不修改其他作业。初始化时未执行 commit 或 push；用户随后明确授权推送，现已推送 stage1-review 与 gh-pages，公网状态待用户开启/验证。

## 本地工作流

Windows 完整命令见 README。API 路径集中 `/api/v1`，未来业务 WS 为 `/ws`，Vite 已配置 ws:true。前端 services/api.ts 定义与 Go Health DTO 一致的响应。页面可公开查看骨架，不产生用户/会议记录。健康状态具有请求中、失败、重试和真实成功状态。

配置来源为系统环境；可用 `-env-file .env` 显式加载（系统优先）。相对 DATABASE_PATH 基于进程工作目录。SQLite 单连接，启用 WAL、foreign_keys、busy_timeout=5000；编号 SQL 通过 embed 编译进 Go，迁移事务内写入 schema_migrations，重启不重复应用。第 1 迁移只建 app_metadata，不预建业务表。

新增迁移另建编号文件，不能修改已经应用的迁移。不要跨数据库事务调用外部服务。SQLite 文件、node_modules、dist、日志、截图临时输出和真实 .env 被忽略；docs/screenshots 为本次实际界面证据，可保留。

## 检查方式

Go 的 config 测试验证系统优先、显式文件和错误参数；store 测试验证重复启动不重复迁移、数据保留及 PRAGMA；httptest 验证 health、统一错误、关闭数据库返回 503。E2E 访问真实 Vite 和 Go，检查 7 条路由在 4 个宽度加载/刷新、无横向溢出、密码显示和禁用状态、health 成功和网络失败后重试。网络失败场景仅阻断一次浏览器请求，不以 mock health 冒充 Go。

云环境 Go 在 /workspace/toolchains/go/bin，缓存位于 /workspace/.cache，PATH 和缓存只影响当前环境，不写进 Windows 启动要求。运行自动浏览器测试时可设 CHROMIUM_PATH=/usr/bin/chromium 复用 Linux Chromium。`SKIP_GO_HEALTH=1` 仅用于后端未安装时的局部排障；最终验收检查不得设置该变量。

## 后续阶段（均未授权开发）

2：账号及会议 REST；3：业务 WS 与文字会议；4：设备和 LiveKit；5：共享/一次静音/移出/结束；6：历史、笔记及稳定性；7：DeepSeek；8：最终回归。后续记录应维护 docs/阶段验收.md，并区分自动、真实服务、用户实机结果。

## 用户补充的界面与最终交付约束

全站删除装饰性宣传标语，使用直接的功能标题和必要状态说明。当前阶段保留必要的未开放提示，全部阶段完成后必须清理用户界面的阶段编号、骨架/预览文字、临时导航，保留真实功能错误和配置说明。最终只将 comeet 文件夹交付到学校仓库的指定位置。详见 docs/交付检查.md，最终回归须逐项验证。
