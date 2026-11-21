# 门户申请与消息通知交付报告

日期：2026-11-21。分支：`feature/portal-access-notifications`。基线：GitHub main `11dce08198b928fa5caf6648fe0304fc3440d5e7`。PR：[suoak/WeKnora #233](https://github.com/suoak/WeKnora/pull/233)。报告在合并前形成，main 合并 SHA 以该 PR 的 mergeCommit 及最终交付记录为准。

## 实现

- 保留原访问弹窗布局，区分可申请、待审批、成员暂停及空间关闭申请。授权空间沿用直接进入流程，原因要求非空且不超过 1000 个 Unicode 字符，并拒绝不可见控制字符；审批意见应用相同长度和内容校验。
- 新建配置前后端默认允许申请，支持管理员显式关闭；省略开关不会重置已保存的选择。既有空间不批量开启。
- 新增持久化用户通知。申请创建事务通知全部有效 Owner，审批事务通知申请人并包含结果、空间及审批意见。数据库事件唯一约束和原审批状态转换共同实现幂等；通知失败会回滚业务写入和审计。原 AuditLog 保留。
- 通知收件箱跨空间按当前用户查询，禁止 API Key；无当前空间的登录用户也可读取本人的消息。请求参数不能选择其他接收用户。
- 门户标题栏和 platform 原铃铛统一使用 `NotificationBell`，始终显示；站内未读数字与邀请数量分开。提供消息列表、未读过滤、分页、单条/全部已读、本人申请历史及取消。保留原邀请接受/拒绝对话框。
- 点击通知仅将已知业务类型映射到固定站内路由，并重新检查有效成员/Owner 资格后切换空间；不接收任意 URL。
- 复用 App 的两分钟轮询，登录立即刷新，提交/审批后刷新，登出停止并清空缓存；在途旧用户请求不能恢复通知缓存。没有新增 SSE、WebSocket 或重复定时器。
- 同步远端 main 时保留其最新文档统计授权边界，没有扩大公开元数据或知识内容访问权限。

## API

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/api/v1/me/notifications?page=1&page_size=20&unread=false` | 本人消息列表；page_size 1–100，page 1–1000000 |
| GET | `/api/v1/me/notifications/unread-count` | 本人跨空间未读总数 |
| POST | `/api/v1/me/notifications/:notification_id/read` | 本人的单条消息已读；他人或不存在消息返回 404 |
| POST | `/api/v1/me/notifications/read-all` | 本人全部已读 |

复用已有 `GET /me/tenant-access-requests` 和 `POST /me/tenant-access-requests/:request_id/cancel`，仅补客户端封装及空间/审批人展示名称，不另建申请历史 API。

## 自动化验证

| 项目 | 结果 |
| --- | --- |
| 六个 Go 包相关回归 | PASS：service、repository、handler、router、database、middleware |
| 前端相关单元/回归与语言键检查 | PASS：62 项 |
| Vue 类型检查 | PASS |
| Vite 生产构建 | PASS；本机 Less worker 超时后用 `preprocessorMaxWorkers: 0` 完成构建 |
| SQLite 真实迁移器 up/down/up | PASS：3049 → 3050 → 3049 → 3050，历史配置/阶段/申请摘要一致，外键检查为零，默认值与通知唯一约束符合预期 |
| 浏览器交互、移动端深色、未读分页收缩 | PASS：3 项；包括移动端抽屉尺寸、导航遮挡、关闭操作与最终截图复核 |
| PostgreSQL up/down/up | PASS：[通知迁移 CI](https://github.com/suoak/WeKnora/actions/runs/44889167475) |
| PostgreSQL 完整新安装和历史库升级治理 | PASS：[升级治理 CI](https://github.com/suoak/WeKnora/actions/runs/44889167472) |
| 本机完整 database 包 | PASS；包含迁移编号、治理清单、SQLite 升级与回滚 |
| 本机完整 repository 包 | 未全通过：两项旧 SQLite 用例清理时文件仍被占用，一项旧符号链接用例缺少 Windows 创建链接权限；本次相关定向测试通过 |

后端命令选择 `Portal|AccessRequest|Notification|SQLiteMigrationsCreateVersionedSchema|SQLiteEnterprisePublicationRealRunnerUpgradeDownUp`。覆盖新申请通知、多个有效 Owner、停用 Owner 过滤、通知失败回滚、通过/拒绝与审计、重复审批幂等、申请资格、长度/控制字符校验、默认值/显式关闭、个人消息认证/API Key 拒绝、分页/已读、跨空间及跨用户隔离。

本机默认 MinGW 8.1 不能链接现有依赖。使用已有 MinGW 16.1 后，服务包仍受一份无关 DuckDB 测试静态库 ABI 问题影响，因此本地定向验证使用 Go 临时 overlay 排除了 `extract_data_table_summary_test.go`，生产源码及所有 Portal 测试保持原样。正常 Linux CI 不使用 overlay。本地结果不是完整仓库测试全通过的声明。

完整 repository 包的 Windows 限制涉及 `TestFeishuLegacyFormalMigrationChain/sqlite`、`TestKBDeletionMetadataScope/sqlite` 的临时数据库文件清理，以及 `TestRetiredFileMissingBasenameKeepsSymlinkDirectoryProtection` 的链接创建权限。没有为使这些旧用例通过而修改相关业务实现。应用提交 `e21fd835d9f6252ea9a23a342a9e3badb9958476` 的 Linux App、lint、前端、升级治理、Portal gate、RBAC 与兼容性检查全部通过，3 项条件检查跳过，没有失败项。完整后端测试、构建、Docker 镜像和启动检查由 [App CI](https://github.com/suoak/WeKnora/actions/runs/44890420736) 通过，不使用本机临时 overlay。

前端命令包含 `notificationTarget.test.ts`、`portalState.test.ts`、`portalContracts.test.ts`、`PortalIntentFlow.test.ts`、`PortalHardening.test.ts`、`IpdKnowledgeMap.test.ts` 和 `localeKeyAudit.test.ts`。浏览器测试使用明确标注的合成 API，不计作真实双账号验收。

## 数据库和交付边界

两套 `003050_user_notifications` up/down 已提交，操作与数据损失说明见 [MIGRATION.md](MIGRATION.md)。源码审计见 [AUDIT.md](AUDIT.md)，文件清单见 [CHANGE_MANIFEST.txt](CHANGE_MANIFEST.txt)。

| 交付项 | 当前状态 |
| --- | --- |
| 生产已发布空间申请开关清单 | 已只读取得：17 个已发布空间，1 开、16 关；“概念2”关闭申请，4 个有效 Owner |
| 测试环境部署版本 | 已部署至测试主机的独立实例，源码 `e21fd835d9f6252ea9a23a342a9e3badb9958476`，schema 3050、dirty=false |
| Owner / 普通用户两个真实账号通过与拒绝验收 | PASS：16 项实际浏览器/HTTP 检查；新建持久化密码账号，不使用 API mock |
| main 合并 | 已完成验收，按需求推进 PR 合并；实际合并状态和 SHA 以 PR 记录为准 |
| 生产发布 | **未部署**；未修改生产数据，未批量开启旧空间 |

## 实际部署与双账号验收

独立项目为 `knowhub-portal-e21fd835d9f6`，位于测试主机的新建目录，只将前端发布至主机 loopback `33531`，通过 SSH 转发访问。PostgreSQL、Redis、文件目录及网络均独立，未覆盖现有测试服务。新配置默认开放、显式关闭返回 409、审批前内容访问受拒、匿名消息访问返回 401，均已实测。

两名账号通过正常密码登录页登录；A 是目标空间 Owner，并有测试发布配置所需的系统管理员资格，B 是普通用户。实际验证了：B 在门户申请；重复 pending 被拒；非 Owner 审批被拒；A 从消息进入审批页拒绝并填写意见；B 收到拒绝通知并看到历史；B 再次申请；A 从消息批准；B 收到通过通知并进入空间；B 获得 Viewer、可读取测试文档并在文档列表按名称搜索；已授权重复申请被拒；他人消息标记返回 404；重复审批不重复通知；全部已读及刷新后状态持久化。

最终数据库只读复核：该申请人 1 条 rejected、1 条 approved、有效 Viewer 1 条；两类结果通知各 1 条，均已读；重复事件 0；审计包含申请 2、通过 1、拒绝 1、成员新增 1。浏览器截图已复核。

最终运行 `playwright.real.config.ts`：1 passed，16 项检查全部 true，证据在本机私有目录 `E:/AIData/CodexTemp/portal-access-notifications/real-browser-r2`。首次尝试将无分块的标题测试数据用于全局分块检索，失败记录保留在 `real-browser`；改为正确的文档名称搜索后，使用新申请账号重跑整个流程，未删除首次授权或修改产品代码。

账号是真实持久化测试身份，未测试企业员工 SSO。知识内容为明确准备的合成手工文档，本次检索验收范围是标题关键词，不声明解析、向量生成或外部模型检索通过。测试二进制使用与标准 App CI 一致的无原生 AnyDoc 编译配置。

应用二进制 SHA256：`f52ab193974c34324311c794b42b234b7cff93cd69210c1153e551a41cfe8eff`。应用镜像 `sha256:e03b00f2f3556df9d23ef1dd9d19b368731c35996ecaf4c1ac61eb739b2b5653`；前端镜像 `sha256:cee1f485e443bf2bf27725268ae9ec31b14bedda0e1ce87a19f74d10f331d01c`。部署及数据库复核证据保存在本机私有目录，账号密码不提交仓库。
