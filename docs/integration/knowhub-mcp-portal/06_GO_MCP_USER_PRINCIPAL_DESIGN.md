# M1 个人 Go MCP 主体设计

第二批状态：主体边界按 [ADR-001](13_M1_FINAL_DECISIONS.md) 定稿；未新增个人 HTTP/MCP 路由。本地现有 `/mcp/:endpoint_id` 为 ALREADY_SUPPORTED，保持兼容。

`enterprise_retrieve` 已成为可显式申请、必须与 retrieve 配对的个人 Key 能力；默认 capabilities 不增加，旧 Key 无回填，所有非 user_mcp 主体都不能消费此公共能力。M2 要求显式可信 Principal+真实 Caller+当前 DB owner/account/key 状态，并注入企业资格 verifier；缺失或关闭均拒绝。公共 Grant 不转换为通用 KBAccess，不替换 caller/execution tenant、不生成成员关系，也不提供文件/管理方法。能力类型扩展不代表 Go 用户入口已上线。

## 接入与复用

拟增加静态 `/mcp/user`，GET/POST/DELETE 使用 Streamable HTTP 同一个 mcpserver.Server/handler/catalog。静态路由优先注册，预留 user 标识并验证与既有 endpoint_id 不冲突；不创建第二个 MCPServer。团队继续 MCPEndpointAuth + mcp_ token。个人仅接受既有 user_mcp Key 的 Bearer，不接受 URL token、tenant/platform Key、团队 token 或客户端自报 user_id。

从 middleware/auth.go 抽出共用 Key 查验与 owner/account 状态解析；个人 MCP 全局入口不要求客户端指定 X-Tenant-ID，范围仅由后端枚举。旧 REST 保持当前要求 X-Tenant-ID 的语义。发送任意 X-Tenant-ID 不能扩大入口范围：个人路由拒绝此选择或仅作为已授权范围内的缩小提示，最小版选择拒绝并提示无须此 header。员工 ID 只来自 Key.OwnerUserID，不采信工号/邮箱/身份 Header。

现有 `Caller` 以单个 tenant 为中心，新 `UserMCPPrincipal` 保留真实 UserID/KeyID/credential scope，独立表示 workspace-free 请求。不能用平台管理员/CanAccessAllTenants 或合成 endpoint 用户代替；对每个个人私有目标从实时 membership 构建限定 Caller，原 UserID/KeyID 不变。企业目标使用绑定该用户、操作、资源和版本的 enterprise read grant，不能伪造其为目标空间成员。

## 三项最小扩展

1. `types/principal.go`、`types/context_helpers.go`：新增可信个人 MCP 主体，定义不可从外部构造的 grant。与现有 endpoint context 分开；执行 tenant 切换不改变主体。
2. `mcpserver/server.go`：抽象当前 endpointFromContext / guardTool / filterTools / rate-limit key / last-used touch / resource URL rewriting 为认证主体适配。团队保留现有行为；个人 touch 复用个人 Key usage 服务，不能误写 endpointRepo。URL 重写转向能验证个人身份的资源代理，不复用团队 endpoint capability 链接。
3. `mcpserver/scope.go`：allowed/select/knowledgeInScope 接收授权目标，复用工具与 SearchTargets。目标保存 resource TenantID、KBID、授权来源（public/personal/both）、operation 和 revision；来源合并但不丢失私有限制。

## Key 兼容与准入

AuthenticateAPIKey 现有 hash lookup、revoked_at、expires_at 校验复用；每次 tools/list/call、文件 GET 重验 owner active、个人 scope、实时 membership/共享授权/Portal 发布策略。旧 Key 无 enterprise_retrieve，私有范围不变；selected-empty 必须拒绝。删除共享关系、离开空间、用户停用、Key 轮换/撤销都在后续请求生效。组织不是使用入口的前提，只有已有组织共享的个人分支需要组织关系。

服务器初期固定八工具：list_knowledge_bases、search_knowledge、grep_chunks、list_documents、read_document、wiki_search、wiki_read_page、wiki_index。ask/add_document/update_document/delete_document 在 tools/list 隐藏，并在 tools/call 无条件拒绝。不能将现有 DefaultMCPEndpointTools 直接用于个人入口，也不能只按 Destructive=false 推断只读。

限流至少按 KeyID + UserID 分开计数，复用现有限流器并限制总 fan-out/并发/每空间时限；匿名无效 Key 不输出资源信息。审计记录 user/key ID、tool、授权来源、数量、decision/reason、耗时和 request ID，不记录 token/正文/prompt。MCP usage 现有 client type 和 invocation tracking 继续兼容，不能由客户端 type 改变权限。

## 验收与回滚

合成 A–F 主体验证错误/过期/吊销 Key、用户失活、伪造 header、旧 Key 不扩权、机器 token 混用、撤销、并发 Caller 不串、八工具直调拒绝、限流恢复/断线重连。必须有实际 Codex/WorkMate/Claude Code 隔离客户端连接和资源读取证据；本批 NOT_TESTED，不宣称上线。

回滚仅禁用新 `/mcp/user` 和前端入口，保留既有团队路由、旧 REST 语义与已生效 ACL。公共能力不能自动迁移到旧 Python Key 配置。
