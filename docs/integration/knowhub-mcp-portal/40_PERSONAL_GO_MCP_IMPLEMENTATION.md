# 第六批个人 Go MCP 与动态范围

## IMPLEMENTED 源码

| 文件 / 函数 | 行为 |
| --- | --- |
| `repository/portal_mcp.go::ListPortalMCPCandidates` | 按实际 tenant、KB 目录枚举，排除 deleted/temporary、非 active 租户；最多 129 条用于 128 个目标上限拒绝；枚举本身不授权 |
| `access/portal_mcp.go::PortalMCPResolver.Resolve` | 校验可信用户、user_mcp、retrieve、企业资格及数据库中最新 Key；公共分支复用 M2 整库安全 Grant，额外复用 M3 live Read/Search；敏感/未知 KB 不作为公共目标 |
| `document_base.go::PrivateWorkspaceContext` | 服务端选择员工实际成员资格和 Key tenant-scope，整合额外已授权空间；不使用用户自报 user_id，不从公共 Grant 创建成员 |
| `PortalKBTarget.Revalidate / SearchContext / GuardPortalRetrieval` | opaque 租户/KB/文档/操作/主体/Key 范围；公私授权独立；空集、超限、未知引擎、越权 IDs 拒绝；每次加载当前策略与版本 |
| `service/portal_mcp.go` | 协调现有 driver/repository；联合搜索、元数据补全、list/read；仅白名单文本和受控 knowhub 引用，模型前/后及结果输出前验证 |
| `middleware/personal_mcp_auth.go` | Bearer → 现有 API Key Authenticate → 真实 owner → 现有 Auth；无显式 workspace 时从 Key 持久化 scope 选择默认，调用者指定 workspace 仍经原 Auth 校验 |
| `mcpserver/personal.go` | 复用既有 Go MCP 库及只读工具 schema，独立 stateless Streamable HTTP server，工具调用前/后复验，无 workspace 机器上下文冒充 |
| `router/routes_personal_mcp.go`、`router.go` | 独立入口 `/api/v1/mcp/personal`，支持 POST/GET/DELETE；现有 `/mcp/:endpoint_id` 团队端点不改 |
| `container.go` | 注入 `NewDisabledPersonalServer`，默认关闭；仅隔离测试构造 enabled server 和可信测试 Provider |

生产组合没有启用开关、正式企业资格 Provider 或自动 Key 扩权；新增能力不是仅由客户端 Header 开启。仍需正式审核、资源分类和配置审查才能另行评估启用。

## 工具目录差异

| 原计划 | 当前个人入口 |
| --- | --- |
| list_knowledge_bases、search_knowledge、grep_chunks、list_documents、read_document | 暴露这五个只读工具，现有工具 schema 复用；直接 tools/call 也受服务端范围限制 |
| wiki_search、wiki_read_page、wiki_index | 不暴露；持久化来源权限未闭环，猜名称调用同样拒绝 |
| ask / 上传 / 修改 / 删除 | 不注册，不允许 tools/call |

搜索默认合并当前合格公共 KB 和独立私人 KB，不要求员工先创建团队 MCP。个人 Key 无 enterprise_retrieve 时只保留其已有合格私人分支，不自动增权。public-kb 在 Portal 撤回后可对有独立成员/Key 权限的用户继续作为 private KB，但不会因此给非成员开放。

目录、文档及正文均先经范围和实时 Grant；read_document 不按猜测 ID 绕过未公开 KB。批量/分页只在授权集合中进行。返回来源包含实际 TenantID、空间、KB、文档/Chunk 与 `knowhub:tenant:...` 引用，不能兑换匿名文件访问。文件路径、URL、原始附件、图片和任意 metadata 不返回。

## 限制及回滚

MVP 上限：目录 128 KB、每 KB 256 文档、单模式候选 100、最终 30 条、read 最多 1000 当前 Chunk 后分页。超过上限拒绝，未截断后当全库；容量和性能不是生产规模保证。共享 agent、部分敏感私人 KB、Wiki 和长文档完整适配仍 HOLD。

未修改数据库迁移、生产 Python、个人 Key 创建流程或团队端点行为。本批回滚以保持默认 disabled 为运行边界，源码按第六批 manifest 与完整第五批快照逐文件恢复；不得 reset/clean 或覆盖前三至五批未提交成果。测试结果和候选标识见 [44](44_SIXTH_BATCH_TEST_RESULTS.md)。
