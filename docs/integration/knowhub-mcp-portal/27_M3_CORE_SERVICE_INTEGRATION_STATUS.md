# 第四批：M3 核心服务接入状态

本批采用用户允许的 P0-A/P0-B 阶段交付。完成候选基线移植和 P0 修复；**没有完成 M3 实际业务读取链接入，M3 整体 HOLD**。本报告说明已实现组件和仍未实施的业务调用链，不能作为开放批准。

## 已实现与重新验证的范围

复用 `internal/application/access/document_access.go::DocumentReader`、`document_base.go::LiveDocumentBaseVerifier`、`document_search.go::GuardDocumentRetrieval/SearchDocuments`、`document_resources.go`，以及 repository 的实时策略、subject grant、epoch/CAS、事务审计；没有重新开发 Go MCP、个人 Key 或检索引擎。

Go 合成集成后端 `document_python_integration_test.go::TestDocumentM3PythonHTTPCompatibility` 使用真实 TCP、Gin boundary、持久化合成 Key/ACL、SQLite 实际检索驱动和实际 Python MCP。它认证的是这个有限适配器，不是 `internal/handler/knowledge.go` 等 rich Handler。

## 实际业务缺口

`internal/router/router.go` 的 `DocumentReadBoundary` 仍为 optional DI 参数，没有生产 provider 或业务认证路由清单。`NewDocumentReader` 没有接入生产 Knowledge/Chunk 服务构造器，普通既有调用不会自动获得新 DocumentGrant。不能把默认关闭误写为旧 REST 已全面执行文档 ACL。

以下业务接入未在本批修改，均为 HOLD：

- `internal/handler/knowledge.go` 的 GetKnowledge、ListKnowledge、GetKnowledgeBatch、SearchKnowledge；详情/分页/数量/标题/摘要/标签/搜索补全均需服务级授权与候选前过滤。
- `internal/handler/chunk.go` 的 GetChunkByIDOnly、ListKnowledgeChunks、ListChunkRevisions，以及关联、neighbor、revision 读取；需要验证真实 chunk 所属 tenant/KB/document，不能信任参数所属关系。
- Knowledge/Chunk service 和 repository 的内部读取入口；须显式区分 Web 用户、受限 Key、公共 Grant、私人权限和可信内部任务。新受保护流程缺少授权上下文必须拒绝，不能调用 legacy fallback。
- RAG、Agent、异步任务、引用和流式输出：当前实际生产链没有统一传递/复验 DocumentGrant。尚不能证明模型请求和最终响应均阻止未授权内容。

## 下一步开发顺序

1. 在默认关闭开关内提供可信主体适配器和 DocumentReader DI；员工/审批 verifier 仍用隔离合成实现，正式映射未决时 nil=deny。
2. 从 canonical document/chunk detail 服务入口接入授权，并覆盖 list/batch/count、关联/版本、标题摘要标签，返回前复验；内部任务使用显式权限上下文，禁止管理员替身。
3. 接入 PostgreSQL/ParadeDB 的真实候选前 allow-set，再处理多 KB、grep、向量及 Hybrid。不能先向模型发送候选再后过滤。
4. 接通 RAG/Agent 的模型网络和响应边界，覆盖并发、取消、重试、缓存与运行中撤销。
5. 最后处理持久化衍生 lineage、文件 broker 实际存储/HTTP、旧匿名 URL/CDN、来源 ACL。

每步独立负向测试后才扩充 certified routes；只接受服务端产生的精确 tenant/KB/document/operation/principal 上下文，不能因旧 Web/Key 兼容而临时放开缺失上下文。

## 业务决策及生产影响

NEEDS_DECISION：企业审批角色正式映射、企业员工可信来源、用户级 deny 与企业/个人授权冲突语义。现有 SystemAdmin/Space Owner 不能自动成为企业发布审核人。当前实现在显式文档拒绝、未知来源和待审核情况下拒绝，未配置生产公共授权。

生产影响为零；没有接入生产，也未公开历史 Portal 知识。局部组件测试 PASS 不解决上述 rich Handler 绕过风险。回滚本地可选接入和组件不会改变现有生产；未来核心服务集成必须另做审阅与隔离验收。
