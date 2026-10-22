# M1 敏感文档 ACL 与全读取路径

第二批状态：权限优先级已按 [ADR-001](13_M1_FINAL_DECISIONS.md) 定稿，完整 M3 仍未接入旧读取链路。现有 KB 授权 ALREADY_SUPPORTED；M2 只有整洁 KB、逐文档显式批准的 text-only 读取，任何混合/外部/未知文档 HOLD 整库，不据此宣称文件 ACL 已完成。

新增 `access/document_filter.go` 提供零值 DENY_ALL、allow-set 求交和 unknown-derived-source 阻断接口及合成测试；已有驱动尚未接入该接口，也未经统一安全认证。`mcp_blocked` 只表示机器/MCP 路径禁止，不能代替所有接口的员工 ACL；restricted/deny 必须在未来 REST、文件、Wiki、缓存和模型输入统一执行。M2 组件不能把新三张表当作已经保护旧 Python REST 的事实。资源专项见 [15](15_RESOURCE_LINK_CACHE_THREAT_MODEL.md)，调用链和开发顺序见 [16](16_M3_CALL_CHAINS_AND_EXECUTION_PLAN.md)。

## 共用组件与调用顺序

在 `internal/application/access/` 扩展 `ResolveKnowledgeRead`、`ResolveEnterpriseKnowledgeScope`、`AuthorizeDerivedRead`、`AuthorizeOriginalDownload`。传入服务端认证的 Principal/Caller、credential scope、持久化 resource binding、精确 operation；输出只读 grant + policy revisions + provenance + tenant/KB/doc ID。失败/未知来源/空授权集合必须显式 deny；禁止用 nil/空 KnowledgeIDs 代表“授权为空”，因为现有 retriever 的空 slice 代表不加过滤。

顺序：身份及 Key 生命周期 → 有效资源 → 个人/企业分支 → 文档显式拒绝 → 下推候选过滤 → fetch 内容 → 外部 Rerank/LLM 前再次读策略版本 → 输出引用/附件前再校验。不向外部服务提交无权候选，即使最终会隐藏也不允许。请求内授权快照有界使用；下一请求重新查。收紧操作同步 bump revision，授权失效不依赖异步缓存通知。长查询在外部调用前和返回前重验；已经发送的数据不能靠撤销追回。

## 文件及路径覆盖矩阵

| 读取面 | 现有主要文件/函数 | 必要改动与拒绝测试 |
| --- | --- | --- |
| KB 列表/详情/统计 | service/knowledgebase.go:ListKnowledgeBases/FillKnowledgeBaseCounts；repository/portal.go | 企业仅投影可读内容与计数，不暴露受限标题/总量；不给空间角色 |
| 文档列表/搜索/详情/batch | service/knowledge.go:GetKnowledgeByID/GetKnowledgeByIDOnly/ListKnowledgeByKnowledgeBaseID/GetKnowledgeBatchWithSharedAccess；handler/knowledge.go | 区分仅取父关系的内部 lookup 与可返回内容的授权读取；列表 SQL 预过滤，批量逐项检查 |
| 分块/版本/父子邻居/生成问题 | service/chunk.go；repository/chunk.go；router/routes_knowledge.go | chunk→knowledge→KB→tenant 校验；任何扩展候选也重新过滤；猜测 chunk_id 拒绝 |
| hybrid/semantic/keyword | service/knowledgebase_search.go:hybridSearchCandidates/buildRetrievalParams；knowledgebase_search_shared.go；knowledgebase_search_results.go | 所有调用者统一解析服务端 allowed-doc filter；客户端 filters 只能进一步缩小 |
| grep/read_document | mcpserver/tools_retrieve.go/scope.go:knowledgeInScope；agent/tools/search_knowledge.go 及 read_document 实现 | 不能仅授权 KB；检索、整页、context 邻居、单 ID 读取一致 |
| Wiki search/page/index/revisions/backlinks | service/wiki_page.go/wiki_ingest*.go；mcpserver/tools_wiki.go；router/routes_knowledge.go:RegisterWikiPageRoutes | 页/summary/索引和历史版本需源知识完整依赖闭包；不完整依赖拒绝整页，不仅移除引用 |
| FAQ/图谱 | service/knowledgebase_search_faq.go；service/chat_pipeline/search_entity.go；FAQ 服务及图谱工具 | FAQ entry 对应 Knowledge ACL；实体/边/描述需每个来源授权，否则禁用该衍生路径 |
| RAG/Agent/chat/ask | service/chat_pipeline/search*.go；agent/tools/search_knowledge.go；mcpserver/tools_ask.go | search target、外部重排、prompt、缓存、历史会话引用均校验；ask 初期不开放 |
| summary/推荐问题/缓存 | Knowledge summary、chunk questions、custom_agent 建议、聊天/检索缓存 | 缓存键包含用户/Key/operation/scope/policy revision；缓存 hit 仍查有效权限；污染内容清理或重建 |
| 图片/附件/图库 | router/files.go；access/files.go/message_files.go；resource catalog/bindings；service/chunk.go | 资源所有当前绑定文档与来源闭包校验；同一文件多绑定不能借公开绑定泄露受限副本 |
| 原文件/preview/batch-download/export | knowledge.go:GetKnowledgeFile；handler/knowledge*.go；router/routes_knowledge.go | parsed read 与 original download 分开；预览若返回原始字节也按 original 授权；batch 全部受控 |
| raw/KB/message file proxy、artifacts | router/files.go；access/files.go/message_files.go | 当前 KB 绑定不是文档授权；历史 session ownership 不能代替来源当前权限 |
| 匿名 presigned / short link / 存储直链 | router/files.go:presignedFileHandler；/r/:token；文件服务 URL 生成 | 受限资源不签匿名链接；资源策略每次验证；已有 bearer URL/外部对象签名需可验证撤销或等待失效并 HOLD |
| 旧 Python REST | 02 中 31 工具路由和扩展下载面 | 共用 service 授权，不信任 MCPClientType/Header 自报来源；full_access 也不能绕过明确文档拒绝 |

表内文件路径共同前缀 `internal/`；部分目录包含多个拆分文件，实际存在清单以 repository 文件索引为准。实现阶段在引用的 service 入口及内部内容 fetch 处做调用图复核，不能只修改路由 middleware。后台任务需显式系统作业权限并保留受限来源标签，不伪装普通用户。

## 候选前过滤与引擎矩阵

ALREADY_SUPPORTED：types/RetrieveParams 的 KnowledgeIDs、ExcludeKnowledgeIDs、KBIDs；SearchTarget 已带 TenantID 与文档范围。源码已有 PostgreSQL、SQLite、ES7/8、OpenSearch、Qdrant、Milvus、Weaviate、Doris、TencentVectorDB 的 KnowledgeIDs 过滤构造。知识 ID 过滤的存在不是文档 ACL 已实现，也不是所有检索模式已实测。

新增 `AuthorizedDocumentFilter` 必须可表达 ALL / ALLOW_SET / DENY_ALL 且默认 DENY_ALL。授权集合来自可信服务，不是客户端 query 或普通 metadata。执行前强制验证目标 tenant/KB、filter 状态和版本。文档白名单大时分批或索引侧安全 join，达到引擎上限不得截断后退回全库。内容填充、FAQ 重试、neighbor enrichment、grep、Wiki 都必须遵守同一过滤。

每个实际启用引擎以合成 canary 文档验证：无权 canary 在 Rerank/LLM spy 的输入中从未出现；scope 空集返回空且不发无过滤请求；并发权限隔离；撤销后下一次请求拒绝。任一引擎/模式不能下推可靠过滤，该混合 KB 不得公开，应独立索引/KB隔离或停用此模式；最终响应后过滤不能作为通过依据。

## 衍生内容和签名链接

Wiki 当前有 ChunkRefs，但不能预设它覆盖全文生成的所有源知识，包括概念页、手工编辑、旧 revision、链接扩展与摘要。无法证明来源闭包的历史 Wiki 不进入企业八工具范围；工具可以被列出，但按 KB 返回明确 policy_not_ready，不能把八工具白名单理解为每个 KB 已可用。

收紧 ACL 时先同步禁止原文和衍生资源读取，再异步清索引/缓存并重建；索引清理失败继续拒绝。匿名 capability URL 没有员工身份，不能实现“仅 D 可读”的员工 ACL。需停止为受限内容生成匿名链接，现有链接通过受控代理资源阻断、到期、对象存储措施及缓存 purge 完成收口。已有已下载内容无法撤回，不在“及时生效”的保证范围。

## 外部同步

service/datasource_service.go 保存 data source/source_resource_id 等 metadata；feishu_personal.go 在下载前验证当前导入者 ACL（代码注释与 Resolve 调用），替换文件保留 import identity。这属于 ALREADY_SUPPORTED 的导入授权，不代表其他员工有权。缺少企业读者完整 ACL 映射和持续撤销传播，NOT_TESTED。

上述替换文件路径是审计 HEAD 的行为。生产 e62b17e2d 的个人导入对已有记录更新另有 ErrPersonalUpdateDisabled 和 recovery 分派路径，legacy 同步另有暂停守卫；应在生产候选基线继续核查其来源/版本/权限保留，不以审计 HEAD 的替换逻辑代替生产事实。Wiki ETag 并发依赖检查也不等价于读者 ACL。

源 ACL 未知→pending_review；源删除、失权→同步阻断并 bump revision；同步失败不能更新为“仍可公开”；重新导入/新 ID 关联旧可信来源记录，保留最严格限制，身份冲突拒绝。Metadata 可用于查源，不可直接授予访问。来源管理员需审查应用授权范围、可信 source revision 和失权 SLA，未知历史保持受限。

数据迁移只通过有审计、幂等、保留原文件/引用/版本/标签/同步关联的应用流程；生产不得手工移动数据库行。回滚不能移除仍在生效的拒绝规则。验收矩阵见 09。
