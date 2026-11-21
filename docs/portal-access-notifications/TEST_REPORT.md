# 门户申请与消息通知交付报告

日期：2026-11-21。分支：`feature/portal-access-notifications`。基线：GitHub main `11dce08198b928fa5caf6648fe0304fc3440d5e7`。草稿 PR：[suoak/WeKnora #233](https://github.com/suoak/WeKnora/pull/233)。main 合并 SHA：无，尚未合并。

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

完整 repository 包的 Windows 限制涉及 `TestFeishuLegacyFormalMigrationChain/sqlite`、`TestKBDeletionMetadataScope/sqlite` 的临时数据库文件清理，以及 `TestRetiredFileMissingBasenameKeepsSymlinkDirectoryProtection` 的链接创建权限。没有为使这些旧用例通过而修改相关业务实现。更广的 Linux App/lint/兼容性检查仍通过 PR checks 持续运行，最终状态以 PR 为准。

前端命令包含 `notificationTarget.test.ts`、`portalState.test.ts`、`portalContracts.test.ts`、`PortalIntentFlow.test.ts`、`PortalHardening.test.ts`、`IpdKnowledgeMap.test.ts` 和 `localeKeyAudit.test.ts`。浏览器测试使用明确标注的合成 API，不计作真实双账号验收。

## 数据库和交付边界

两套 `003050_user_notifications` up/down 已提交，操作与数据损失说明见 [MIGRATION.md](MIGRATION.md)。源码审计见 [AUDIT.md](AUDIT.md)，文件清单见 [CHANGE_MANIFEST.txt](CHANGE_MANIFEST.txt)。

| 交付项 | 当前状态 |
| --- | --- |
| 生产已发布空间申请开关清单 | 未取得；已提供只读核查 SQL，未连接生产数据库 |
| 指定测试环境部署版本 | 未部署；环境目标/访问方式待明确。本地静态预览不等于测试部署 |
| Owner / 普通用户两个真实账号通过与拒绝验收 | 未执行，待指定测试环境及账号获取方式 |
| main 合并 | 未执行，须先完成验收 |
| 生产发布 | **未部署**；未修改生产数据，未批量开启旧空间 |

需提供测试部署说明或配置路径、两个测试账号的安全获取方式及生产只读核查连接方式，才能继续完成环境验收。无需在聊天或仓库保存密码。生产操作仍遵守需求中的批准条件。
