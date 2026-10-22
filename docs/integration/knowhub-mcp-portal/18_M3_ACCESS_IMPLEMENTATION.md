# M3 文档权限实现与边界

本批是默认关闭的隔离实现。**不能据此验收完整 M3，也不能开放混合敏感 KB。** 实际生产业务 Handler、所有检索驱动、历史资源和外部同步权限尚未全部接通；未认证入口在新隔离屏障内拒绝，在未启用屏障的原应用中仍是待修复风险。实际结果见 [22](22_M3_TEST_RESULTS.md)，交付与开发顺序见 [23](23_THIRD_BATCH_DELIVERY.md)。M0 不重做；此前报告和原始证据保留。

## 已实现源码

| 源码 / 函数 | 行为 | 范围与限制 |
| --- | --- | --- |
| `internal/types/document_access.go` | read/search/file 精确操作、主体授权记录 | 无管理或成员能力 |
| `access/document_access.go`: `DocumentReader.Authorize`, `DocumentReadGrant.Revalidate`, `ReadText` | 复用 M2 政策快照、实时身份/Key、审批/版本/文档拒绝；不生成通用 KBGrant | 只接受可信 web_user；API Key 限 user_mcp；默认关闭，nil verifier 拒绝 |
| `access/document_base.go`: `LiveDocumentBaseVerifier.CheckDocumentReadBase` | 重新加载成员、个人 Key tenant/KB scope，复用 `ResolveKB`、现有共享查询 | 不信任旧角色、空 selected scope 或缓存 KBGrant；企业作用域、文件收口 verifier 未映射则拒绝 |
| `repository/enterprise_access.go`: `LoadDocumentReadSnapshot`, `resourceVersion` | 与 M2 共用政策事实；读私有文档不要求 Portal 已公开 | M2 原公共 Reader 保持整 KB 保守 HOLD |
| `repository/document_access.go`: `ChangeDocumentSubjectGrant` | 精确用户/文档/操作/版本，revision CAS、撤权优先、同事务审计 | 必须经过 `access.ChangeDocumentSubjectGrant` 和独立业务 authority；不提供生产审批 API |
| `middleware/document_read_boundary.go`: `Outer`, `Inner` | 认证前阻断未认证路由，认证后入场授权，最多 8MiB JSON 缓冲并在输出前重验 | 默认无路由认证；禁止流式 Flush/Hijack，不能把缓冲当模型输入守卫 |
| `router/router.go`: 可选 `DocumentReadBoundary` 注入 | Outer 在匿名文件等路由之前，Inner 紧随已有 Auth | 未注册 DI provider，无环境开关、无生产启用；启用且无认证映射时整个隔离路由器拒绝 |

`DocumentBaseVerifier` 是现有权限事实的适配接口，不是第二套成员表。测试用 synthetic verifier 不能代替正式企业资格与业务角色映射。普通 full_access、Tenant、Platform、mcp_ 机器身份不能借新 Reader 读取被拒文档。没有读取 Grant 可以用于管理操作。

HTTP 屏障的无权限事实实现位于 `access/httpboundary/boundary.go`，middleware 文件是别名适配，避免测试依赖回环。Grant 同时绑定 Caller 工作空间/角色、credential context 与公共/个人路径，不能在另一个即便独立允许的认证上下文中复用。主体撤权采用冲突时原子更新的 revoked tombstone；首次审批和首次撤权同时插入也不能因 INSERT DO NOTHING 让撤权丢失。审批仍必须 CAS，审计失败回滚整个授权变更。

## 权限决策

| 文档状态 | 企业公共路径 | 个人路径 | MCP / API Key |
| --- | --- | --- | --- |
| approved + ordinary + inherit + local + 当前版本 | 空间/KB 同样批准、Portal published、实时员工资格、受限凭据作用域均成立才允许 read/search | 仍可经现有 KB 权限读取 | 还须 live retrieve + enterprise_retrieve；旧 Key 不扩权 |
| approved + ordinary + inherit，公共条件不成立 | 拒绝公共读取 | 要求当前 KB/成员/共享/Key scope | 不获得跨空间公共能力 |
| approved + restricted | 不公开 | exact user + operation grant，当前文档版本，且原 KB/Key 权限通过 | 同样检查，不允许 full_access 绕过 |
| approved + mcp_blocked | 不走公共分支 | 可信 Web 主体仍需显式操作授权和原 KB 权限 | 全部 API Key 拒绝；不读取 X-From-MCP、UA 或来源 Header |
| deny / revoked / pending / 缺记录 / 未知分类 / 解析未完成 | 拒绝 | 拒绝 | 拒绝 |
| 非 local、历史 verified、飞书/GitLab 未接通实时源权限 | HOLD | 新 Reader 中 HOLD | HOLD |

显式文档拒绝先于空间/KB 开放及个人授权。个人附加 Grant 的撤销移除该附加路径；它不把一份独立批准的公共文档改成用户级 deny。若业务需要用户级拒绝覆盖公共授权，须另作明确决策；当前没有该授权入口，也不启用真实企业公共路径。文件不继承公共 read/search，必须有 file 操作授权、现有文件权限适配及旧链接收口证明。

## 3035：内容变化使审批失效

新增 PostgreSQL/SQLite 正式 `003035_document_access`，仅新增 `document_access_epochs`、`document_subject_grants` 及触发器；不修改 3033 旧列、不回填公共审批、不修改 Key 能力。`generation + incarnation` 进入现有 resource_version 哈希。migration down/up 使用新 incarnation，旧审批不会因计数重置复活。

knowledges/chunks 的 INSERT、UPDATE、DELETE 均由数据库触发器更新代次；移动 Chunk 同时影响旧/新文档；PG 另处理 TRUNCATE。即使旧 Worker、Raw SQL、批量写入不更新 updated_at/content_revision，也会失效。缺 epoch、触发器缺失/禁用、未支持数据库均 HOLD。DBA 禁用触发器、直接改 epoch、恢复整库和副本一致性不在普通应用写入保证内：必须隔离、换 incarnation 并重新审核，不能带旧授权运行。

| 写入类别 | 核对入口 | 授权版本结果 |
| --- | --- | --- |
| 上传 / URL / 手工创建 | `service/knowledge_create.go`: CreateKnowledgeFromFile/URL/Manual/Passage | 最终 knowledges/chunks DML 受触发器约束 |
| 解析、重新解析、旧 Worker | `service/knowledge_process.go`: ProcessDocument, ReparseKnowledge, ProcessKnowledgeListReparse | 不依赖 Worker 主动维护权限版本 |
| 手动编辑 / Chunk 更新、恢复版本 | UpdateManualKnowledge；`repository/chunk.go` Update/Revert 路径 | 所有行更新均失效，包括元数据/启停变更 |
| 数据源同步 / 批量 FAQ 导入 | `datasource_service.go` processSyncAttempt/processSyncStreaming；knowledge_faq_import/batch | SQL 内容变化失效；源端仅权限变化仍 HOLD，未实现 webhook/可信源 ACL |
| 删除、恢复、移动、克隆 | `repository/knowledge.go` Delete/HardDelete/Update；knowledge_clone_move.go | 删除/恢复/新 ID 有新代次；不存在的资源拒绝 |
| Wiki/图谱/历史消息、图片/附件、对象存储原地替换 | 独立表、外部对象或衍生资源 | **不声称被文档触发器完整覆盖**；需依赖证明/内容哈希；旧路径 HOLD |

窄范围静态清单见 [write-entrypoints](evidence/batch3/write-entrypoints.log)、[raw-writes](evidence/batch3/raw-writes.log)。它们是调用图证据，不是逐个真实 Worker 端到端通过证明。

## 尚需决策及风险

NEEDS_DECISION：正式审批角色与企业员工资格来源；企业凭据目标范围适配；用户级拒绝语义；文件历史链接/CDN 收口授权。没有将 Portal 管理员或空间 Owner 自动映射成审核人。

未改造的业务 Handler/直接仓储调用仍可能绕过新组件；默认未启用的新组件不会自动保护原应用。新屏障只有在完整应用显式注入、各安全路由逐一认证后才构成入口限制。不得先给现有 rich-response Handler 添加一条“允许”认证就开放 KB；须完成内容字段、关联资源、分页计数、外部模型输入和来源依赖核对。

生产影响：无。回滚：保持未注册；源码可逐项回退。隔离库回滚先停止读/写 Worker，再 down 3035；业务内容不删除，但主体授权元数据会被移除，应先导出审计/策略备份。重新 up 后全部旧文档审批重新验证，不允许承诺无损恢复旧 Grant。

## 正式迁移来源门禁

当前分支缺少生产 3033 对应迁移文件，直接把正式 runner 指向当前 migrations/versioned，从 3033 升级会报 no migration found for version 3033。该测试的 NoError 断言保留，实际 FAIL。另用冻结生产迁移来源 + 原样 3034/3035 的测试专用组合验证正式 up/down/up：PASS，不等于当前分支可直接升级生产。没有补假的 3033 空迁移、修改原有飞书迁移或执行生产迁移。此项必须在后续生产分支来源对齐后再次验收。
