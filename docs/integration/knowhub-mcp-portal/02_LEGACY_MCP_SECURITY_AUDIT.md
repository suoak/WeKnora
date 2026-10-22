# M0 旧 Python MCP 安全审计

## 生产事实

PASS（配置与静态模块核对）：生产容器为 `WeKnora-mcp`，认证模式 `weknora_api_key`；未配置 `WEKNORA_API_KEY` 进程级 Key。实际模块仍有默认 shared，但本部署已显式覆盖。不得报告“生产正在用共享管理员 Key”。Python 31 个注册工具的部署 AST 与本地一致，模块 SHA256 也一致，虽镜像构建 SHA 较旧。证据见 [production-readonly-20261020.json](evidence/production-readonly-20261020.json) 和 [source-baselines.json](evidence/source-baselines.json)。

ALREADY_SUPPORTED：network_auth_mode、MCPNetworkAuthMiddleware、validate_weknora_api_key、WeKnoraClient._auth_headers 使用 ContextVar 保存调用者 Key/Tenant，并由后端 auth/me 校验；个人 Key 必须给 X-Tenant-ID。SSE 绑定摘要主体，finally 重置上下文。并未实现跨 Portal 范围发现。

生产存在 29 条 user_mcp，其中未撤销且未过期 24 条；tenant Key 共 3 条，其中 full_access 2 条、非 full_access 1 条，均未撤销且未过期。这只证明数据库状态，不证明这些 Key 被哪些客户端使用，不能推断发生了越权。旧 legacy tenant API Key 兼容路径、代理网络、外部客户端配置仍需另外核对。没有复制 Key 值、hash 或员工 ID。

## 风险和优先级

| 编号 | 证据及影响 | 判定 | 缓解设计 |
| --- | --- | --- | --- |
| R1 高 | 文档/分块/搜索/Wiki 读取主要按 KB 授权，Knowledge 无本任务所需 ACL；生产 schema 无相应策略 | NEEDS_DECISION；敏感治理未完成，不是已复现泄露事件 | 共享 access 层 + 所有服务读路径文档策略；混合 KB HOLD |
| R2 高 | 31 工具含 create/delete/model/chat/agent；没有与 Go guardTool 等价的个人八工具服务端白名单 | ALREADY_SUPPORTED（工具存在）；拒绝效果取决于后端 Key 能力 | 个人 Go 固定白名单；旧后端最小 capability；不依赖 denyTools |
| R3 高 | 2 条有效 tenant full_access Key 可潜在访问其空间超出纯检索需要的内容 | NEEDS_DECISION；用途未知 | 业务负责人盘点用途及范围，另行批准缩权，不自动轮换 |
| R4 高 | router/files.go:presignedFileHandler 验证签名/expiry 后直接 GetFile，公共缓存 max-age=86400 | ALREADY_SUPPORTED（现有签名能力）；即时 ACL 撤销缺口 | 敏感资源禁止匿名直链，代理每次核对策略，Cache-Control no-store，处置已有链接/缓存 |
| R5 高 | Wiki、summary、生成问题、图谱、历史 message refs 可含旧文档衍生信息 | NOT_TESTED，未读取真实内容 | 源文档依赖闭包授权；不完整依赖直接 HOLD，重新生成安全衍生资源 |
| R6 中 | shared 模式仍在源码，若未来误配会退回进程级 Key | 生产当前未使用，不擅改运行模式 | 部署验收锁定模式；共享模式仅明确机器用途，不当作员工身份 |
| R7 中 | QueryText、file_path 等仍有业务日志，未来敏感查询/路径可能进入日志 | ALREADY_SUPPORTED（日志代码存在）；实际日志暴露 NOT_TESTED | ACL 流程只记 request/decision/revision，禁止正文、Key、敏感查询进入审计 |

目前未执行任何生产破坏性请求、无权读请求或真实文件下载。未取得 Codex/WorkMate 的生产客户端配置，所谓“21 条 denyTools”实际启用情况 NOT_TESTED。客户端隐藏工具不能阻止自行构造 tools/call 或直调 REST。旧 Python 在当前透传模式下，后端个人 Key 的 capability/成员/KB 检查仍有效，但没有未来文档级拒绝规则可执行，不能宣布敏感收权完成。

## 工具与 REST 路由完整盘点

机器可读的 31 对应关系及源码行号见 [legacy-tool-routes.json](evidence/legacy-tool-routes.json)。路径均以 `/api/v1` 为前缀；同名方法不是同义授权。

| 工具 | REST 方法与路径 | 授权核查点 |
| --- | --- | --- |
| create_tenant / list_tenants | POST /tenants；GET /tenants | 平台/租户边界，个人 Key 不获管理权 |
| create_knowledge_base / list_knowledge_bases | POST /knowledge-bases；GET /knowledge-bases | manage_kbs 与 retrieve 分开 |
| list_shared_knowledge_bases | GET /shared-knowledge-bases | 当前组织共享关系及角色上限 |
| get_knowledge_base / delete_knowledge_base | GET /knowledge-bases/{id}；DELETE 同路径 | ResolveKB；删除需管理边界 |
| hybrid_search | POST /knowledge-bases/{id}/hybrid-search | 所有 KB/KnowledgeIDs 预过滤及模型调用前检查 |
| create_knowledge_from_file / create_knowledge_from_url / create_knowledge_from_text | POST /knowledge-bases/{id}/knowledge/file、/url、/manual | ingest + KB 范围；文件上传路径已有安全解析 |
| update_knowledge_from_text | PUT /knowledge/manual/{id} | ingest + 所属 KB 写权限 |
| list_knowledge | GET /knowledge-bases/{id}/knowledge | 列表标题、metadata 也须 ACL |
| get_knowledge / delete_knowledge | GET /knowledge/{id}；DELETE 同路径 | 资源父 KB；需要新增文档策略 |
| create_model / list_models / get_model | POST /models；GET /models；GET /models/{id} | 模型管理/配置敏感字段投影 |
| create_session / get_session / list_sessions / delete_session | POST /sessions；GET /sessions/{id}；GET /sessions；DELETE /sessions/{id} | 会话 ownership/Key persona，不能作为纯只读检索 |
| chat / agent_chat | POST /knowledge-chat/{session_id}；POST /agent-chat/{session_id} | 会话与 Agent 完整工具链，不在初期八工具内 |
| list_agents / get_agent | GET /agents；GET /agents/{id} | read_agents、Agent 配置投影 |
| list_chunks / delete_chunk | GET /chunks/{knowledge_id}；DELETE /chunks/{knowledge_id}/{chunk_id} | 父文档绑定 + 文档策略，不仅 KB |
| wiki_search / wiki_read_page / wiki_index_view | GET /knowledgebase/{kb_id}/wiki/search、/pages/{slug}、/index | 页全文、summary、ChunkRefs 依赖闭包 |

后端主要路由定义：internal/router/routes_knowledge.go（KB、文档、分块、FAQ、Wiki）、routes_auth_tenant.go（账号/空间/个人 Key）、routes_agent.go（Agent）、routes_chat.go（会话/聊天）、routes_infra.go（模型等设施），结合 router.go 的 Register 调用及组织共享注册核对。所有精确 APIKey gate 以注册源码为准，不能从 Python 动词推断有权。

扩展读取面：GET /knowledge/{id}/preview、/download；POST /knowledge-bases/{id}/knowledge/batch-download；GET /chunks/by-id/{id}、分块 revisions；GET /knowledge/batch、/search；跨 KB POST /wiki-search；FAQ /entries、/search；图库 /images、/gallery-config；/files、KB files、message files、artifacts、/r/:token、/api/v1/files/presigned。完整修改面及验收见 [05](05_SENSITIVE_DOCUMENT_ACL_DESIGN.md)。

回滚原则：未来文档 ACL 生效后不能退回无 ACL 的旧后端继续暴露 REST；应关闭新公共入口并保留安全读取守卫，必要时受控隔离受限 KB。生产凭据缩权、签名链路变更须另行批准。本批无生产写入。
