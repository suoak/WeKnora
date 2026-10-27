# 双身份架构与兼容迁移设计

状态：设计 + 类型契约已实现；配置、迁移、路由、worker 和前端尚未接入。此文中未来字段/API 不是当前已上线能力。

## 身份与配置

单企业应用保留一个加密配置。保留旧 AuthMode 只供旧客户端/旧任务兼容；新增版本化 capabilities：application_enabled、personal_oauth_enabled，分别控制两种身份。enabled 继续作为总开关。新运行时还需显式 server rollout 开关；新开关全部默认 false。环境变量不能由浏览器覆盖，也不能绕过原 application first-create、测试 KB allowlist、engine availability。

旧配置无 capabilities 时按原逻辑解释，绝不凭升级自动打开另一个身份。管理员显式迁移成 V2 后，双开关独立设置，AuthMode 不再决定后台任务身份。未知版本/模式必须拒绝，不能 fallback。V2 导入请求必须提交明确 auth_mode / connection_id；缺失、失效或禁用不能自动改用另一身份。

已添加 `internal/types/feishu_access.go`：

- FeishuCredentialRef v1：kind、connection_id、app_id、owner_id、open_id、grant_epoch、config_version；不包含 Token；私有 epoch/open_id/config version 不序列化。
- FeishuAccessCapabilities：rollout 与两个独立能力开关，零值全部关闭；ResolveMode 只接受显式已启用身份。
- Validate 对 user 引用绑定 caller、open_id 和 grant_epoch；application 引用拒绝携带用户 owner/open_id/epoch。它只是服务器加载后结构校验，不是访问授权。

本轮这些类型没有接入请求或 worker，因此现有 AuthMode 行为完全保留，**不能声称已经支持双身份导入**。没有新增第二套 Token provider、同步器或发布器。

## 连接生命周期

现有 FeishuConnection 按 user_id 唯一，只存 OAuth。后续扩展时复用同一表：添加连接 UUID、identity_kind、schema_version、status、generation/revocation_epoch 与配置身份版本；OAuth owner 保留，应用连接由服务器关联企业配置，不把 tenant token 复制到每个员工连接。连接引用只允许服务器加载，禁止客户端构造 open_id、owner 或 epoch。服务内取 token 必须继续验证调用者、活跃用户、企业成员、KB grant 和 persisted owner。

连接状态 pending → active → expired/revoked/disconnected。refresh 使用 PostgreSQL 行锁或等价 fencing，只更新密文及有效期，不改变授权 epoch；重新授权产生新 epoch，使旧 attempt 不可继续。Disconnect 删除/失效本人的 state 和连接、增加 epoch、标记本人来源需处理；不得恢复任何 hold/paused/revoked 来源。服务端撤销与本地断连分开实现/审计。

OAuth state 绑定当前 KnowHub JWT、配置版本及一次性 claim；回调仍走 /feishu/callback 与受认证后台 POST。生产要求 HTTPS，HTTP 仅 loopback。回调完成前、连接提交时、资源读取前和发布提交时重查版本/开关。管理员改 AppID 或 Secret 不得静默接管旧 open_id；旧连接重新授权。共享配置变更可以按现有策略安全拒绝旧 attempt，恢复需明确重新预检，不自动续跑。

OA user_id 与飞书 open_id 的当前关系是“当前 KnowHub 会话完成该 OAuth 授权”的绑定，**不是已证明同一员工**。需要企业 tenant_key、可信员工标识映射或独立账号关联确认，并处理一个飞书身份被多个 KnowHub 用户绑定的冲突；不按名字、邮箱猜测归属，不跨应用复用 open_id。

## 数据库兼容迁移（尚未执行）

不照搬上游迁移编号。PR-B 在最新 migrations 清单上分配新版本，先做 expand，再双读，最后单独 contract：

1. 给 feishu_connections 添加可空 connection_id、identity_kind、schema_version、status、generation 等；旧行解释为 user_oauth/v1。分批填充 UUID 和类型，校验 owner/app/open_id、密文可解读性与既有 grant epoch；不重新生成用户 token、不复制 token 到日志。
2. `(user_id)` 旧主键保持，第一期应用引用指向企业配置而非插入空 user_id 的连接行；若后续确需多应用/多连接，独立迁移到连接 UUID 主键和 `(app_id,user_id,identity_kind)` 唯一约束，先确认所有旧 worker 的 ON CONFLICT(user_id) 已兼容，再移除旧唯一性。不得一步改主键导致旧代码不可回滚。
3. 给个人来源/attempt 可空添加 credential_ref_version、connection_id、connection_generation；不修改现有 auth_mode、application_id、grant_epoch、app_config_version 的含义。历史行兼容适配器使用原字段，不通过“字段缺失”改选身份。
4. V2 batch/item 表单独新增（见 05），外键关联现有 import/attempt，不重建 Knowledge。历史 Wiki/Drive 凭据继续走旧 datasource 密文配置；显式管理员升级来源后才使用 application credential reference。
5. 发布新 reader/worker 之前，不写旧 worker 不能识别的新任务；复用现有 payload version 和独立 application task type，增加版本协商门禁。回滚时禁用 V2 admission、暂停其调度，不丢弃或改写 cursor，旧任务按原版本消费。

验证 PostgreSQL/SQLite up/down、历史空字段、双 reader、旧 worker 拒绝新 payload、重复迁移和并发回填。rollback 不自动 drop 含凭据/任务记录的列或表；先确认没有活跃 lease、outbox 和引用，再单独评审清理。生产本轮不执行任何迁移。

## 统一读取和发布路径

resource selection → credential reference resolver → 当前身份 ACL/导出预检 → 现有 PersonalRecovery.prepare → attempt/claim/lease → 现有 candidate/manifest/outbox → 既有原子发布/index/wiki。

运行时 PR-B 必须替换所有全局模式检查，特别是 authorizeLocal、mutate 与 application publication fence；以来源/attempt 的 auth_mode 检查对应 capability，并继续检查 config version、grant epoch、app/open_id。只改 Authorize 或 UI 不足以成立。

Wiki/Drive connector 暂保留各自目录发现和 cursor；以后通过同一个凭据 provider 注入身份，不把 user token 写入 datasource config。禁止 fallback tenant token，禁止由 worker payload 自带 user token。每个 API 请求包括图片/导出轮询都重查身份；源撤权与本地发布提交之间依旧受原有 fencing 约束。

## 个人知识发布门禁

现有模型未提供足以直接断言“只给本人可见”的 KB 标识，且 KB write grant 并不代表源个人文档已获共享发布批准。因此 PR-B 不能把“我是 KB creator”当作私人 KB 证明。

先建立由服务端验证的私人可见性或源内容共享批准记录，绑定 owner、KB、真实资源、revision、目标可见范围、批准人及有效期；提交和发布时都重新检查。无批准的个人资源只允许进入已证明私有的暂存/KB；若系统尚不具备可证明私人 KB，则拒绝个人导入 admission，OAuth 连接本身可以单独验收。共享/Portal/MCP 可见性改变必须使旧批准失效或要求重新审批。不得因本轮功能开关打开而将个人文档直接发布到团队 KB。

管理员 UI 后续展示一套企业凭据、两个独立开关、HTTPS callback；个人 UI 展示本人连接状态/过期/断连；ImportDialog 显式选择个人或应用身份，并分别显示可用状态与来源。新协议未接入前不展示伪可用功能。
