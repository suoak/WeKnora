# M0 上游与 Fork 差距

比较对象：审计 HEAD、上游稳定 v0.8.3、上游实时 HEAD、生产 app SHA、生产 Python SHA，详见 [00](00_BASELINE_AUDIT.md)。本批没有 merge/cherry-pick。比较命令 `git diff --stat <SHA> HEAD -- <paths>` 与 `git show <SHA>:<path>`；摘要见 evidence/source-baselines.json。

| 项目 | 上游/本地事实 | 状态 | 开发判断 |
| --- | --- | --- | --- |
| Go MCP server 与 12 工具 | 上游已有；本地保留 server.go/toolCatalog、scope.go、tools_retrieve/ask/wiki/ingest | ALREADY_SUPPORTED | 扩展主体，复用同一 server 和 handler |
| tools/list 与 tools/call | filterTools + guardTool；本地 TestCallHiddenToolIsRefused | ALREADY_SUPPORTED | 个人模式也必须使用调用时白名单 |
| ask 默认勾选 | types/mcp_endpoint_tools.go:DefaultMCPEndpointTools 选全部非 destructive 工具 | ALREADY_SUPPORTED | 默认 9 项，不得标为严格只读；不改团队兼容语义 |
| Portal | 本地 repository/service/types/router/frontend 自有定制；upstream 对应 Portal repository 不存在 | ALREADY_SUPPORTED | 不回退、不把 discoverable 改成 readable |
| 个人 user_mcp | 本地 OwnerUserID、独立 scopes、lifecycle、client type、usage 定制 | ALREADY_SUPPORTED | 不新建 Token/员工表 |
| access | 上游及本地已有 Caller/KB grant/组织共享/文件边界 | ALREADY_SUPPORTED | 直接扩展，禁止绕过或用 execution tenant 替代 Caller |
| 动态企业公开授权 | 无已批准 Portal→KB→Document 的完整解析 | NEEDS_DECISION | 发布策略、可信历史审核、新资源准入 |
| 文档级 ACL | 现有类型、生产 schema 未提供本任务闭环 | NEEDS_DECISION | REST/检索/文件/Wiki/缓存统一实现 |
| 个人 Go HTTP 入口 | 当前 router 只挂 /mcp/:endpoint_id，认证为合成机器主体 | NEEDS_DECISION | /mcp/user 独立认证；保留现有机器路线 |
| 检索基础 | 多 KB/共享 tenant/embedding key/store group/RRF/rerank/部分失败已有 | ALREADY_SUPPORTED | 补 ACL 前置、跨模型 rank 融合和质量验证 |
| Python 定制 | 生产/local 模块摘要相同，和上游模块摘要不同；透传身份及 workspace ContextVar 已有 | ALREADY_SUPPORTED | 不以镜像旧 SHA 推断代码缺失，也不重做 Python 工具数量 |
| 上游新差异 | HEAD 有 tools_annotations_test、tools_read_document_test 等审计 HEAD 没有的测试，并有读取工具变化 | NOT_TESTED | 逐项评估，不能全量拉取当安全修复 |
| 原工作树未提交共享重校验测试 | internal/mcpserver/shared_scope_revalidation_test.go、service/mcp_endpoint_shared_revalidation_test.go；MCP UI options 文件 | NOT_TESTED（本 worktree 未纳入） | 后续先评审原改动，避免重复实现/覆盖 |
| 生产 RC3 飞书/Wiki 差异 | personalRecovery、来源同步暂停守卫、个人导入更新限制、Wiki 依赖 ETag 冲突检查 | ALREADY_SUPPORTED（生产提交源码）；开关实效 NOT_TESTED | 保护已有 RC3 定制，不能把审计 HEAD 当部署候选；详见 00 |

上述“已有”是源码功能判定。部署只确认指定版本、模式、表结构，客户端行为和生产权限验收没有 PASS。风险、生产影响和回滚统一见 02/11；测试实测在 09，后续文件级任务见 12。
