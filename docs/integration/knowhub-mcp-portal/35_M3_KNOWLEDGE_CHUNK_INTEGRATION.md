# 第五批 Knowledge / Chunk 业务读取接入

本报告对应第五批首个子批：容量、Windows 回归、核心读取接入。**M3 整体 HOLD；不开放 Portal，不启用生产策略。** 最终测试退出码和候选指纹以 [38](38_FIFTH_BATCH_DELIVERY.md) 及机器可读 evidence 为准。

## IMPLEMENTED：真实服务与授权事实

| 源码 / 函数 | 本批接入 |
| --- | --- |
| `access/document_read_scope.go::AuthorizeKBReads / GuardDocumentReadQuery` | 复用既有 DocumentReader、live verifier 与持久化 ACL；不创建第二套 ACL；opaque allow-set 精确绑定 principal、原 Caller、Key、owner Tenant、KB；空集 DENY_ALL，上限 256，异常拒绝 |
| `service/document_read_access.go::authorizeStoredDocument` | canonical 资源定位后取得 Read Grant；取得 Grant 后重新读取真实所属租户的资源，再复验，防止旧内容配上新审批 |
| `knowledge.go` 的 detail/only/list/paged/batch/shared-batch | 实际服务方法接入；分页 SQL 先限制 owner Tenant、KB 和 allow-set，再 COUNT/OFFSET；batch 仅返回获准文档，不混用旧 Grant |
| `chunk.go` 的 detail/only/list/paged/parent | 从持久化 document→KB→Tenant 验证归属；仅返回获准、启用、当前 text Chunk；精确 document/Chunk 直接读取不绕过 ACL |
| `document_repository_hold.go` | 拦截 GetRepository 读旁路；已认证 Chunk ID batch / neighbor 委托受控组件；未知读取接口返回拒绝，原写接口仍由既有调用者负责准入 |
| `access/document_read_output.go` | 原授权证明保留到输出，绑定可信主体与 Key；响应前重新加载现行策略、资源版本与凭据，不用旧缓存授权 |
| `middleware/document_core_reads.go` | 复用已有 8 MiB JSON buffer，准入与 flush 前检查；失败丢弃正文，no-store，拒绝文件/流式/未知输出 |
| `router/document_reads.go / routes_knowledge.go`、Knowledge/Chunk handlers | 接入现有五条路由及现有 Handler；保留 normal Auth 与 APIKeyRouteAuthorizer，不赋予空间成员/KB 管理权限 |
| `container.go` | 显式提供 `NewDocumentReadPolicy(false,nil)` 并装配三个服务 decorator 和边界；没有客户端开关或生产 enable 配置 |
| `types/session.go::StringArray.Scan` | 修复 SQLite TEXT 被忽略导致实际 Key 能力丢失；接受 string / []byte，NULL 清空，非法类型或 JSON 返回错误且不保留旧 scope；不新增 Key 能力 |

五条认证路由：

```text
GET /api/v1/knowledge/:id
GET /api/v1/knowledge/batch
GET /api/v1/knowledge-bases/:id/knowledge
GET /api/v1/chunks/:knowledge_id
GET /api/v1/chunks/by-id/:id
```

## 权限和输出限制

所有 HTTP 主体通过现有 `middleware.Auth`、实际 API Key service/repository 和现行成员关系解析；测试只对员工与审批资格使用合成 verifier。`X-From-MCP`、User-Agent、伪造 UserID / Grant Header 均不提供权限。

因为同一 Key 无法可靠区分旧 Python 与 REST，`mcp_blocked` 对所有 Key 读取一致拒绝；授权 Web 用户单独验证，可读取其具有显式授权的该类文档。普通 tenant full_access 机器主体不转成员工，不绕过明确拒绝。现有 Key 不自动获得 enterprise_retrieve。

返回字段采用白名单：文档标识、真实归属、已授权标题、文件名、folder、类型和处理状态/时间；Chunk 返回当前 text、标识、归属、序号和 revision 号。**不返回任意 metadata、源 URL、存储路径、文件链接、生成摘要、profile、关联标签或历史 revision 正文。** 标签和关系写入的版本覆盖尚未完成，tag filter 与标签读取保持 HOLD。不是靠移除拒绝断言扩大通过范围。

保护模式下，未认证内容路由（含内容写操作）、Wiki/FAQ/graph、文件/HMAC、`/r/:token`、IM callback、聊天、引用、Go MCP 和模型接口保持 HOLD，在进入其 Handler 前阻断。无关身份/租户/OA/飞书管理路由维持原规则；**不适合在完整生产应用打开此试点模式**。默认关闭时原有业务路径保持原行为，既有完整套件需实测验证。

唯一 MCP 前缀例外是非内容读取的 `POST /api/v1/mcp/usage`，继续经过原 Auth 和路由能力门禁；它不提供文档/正文读取 Grant。

## 实际测试设计与风险

`internal/testutil/documentaccess/fixture.go` 用正式迁移器从空库初始化 3035，合成租户 1/99、A/B/C/D、审核主体和 Portal。不放宽 CHECK/FK；user_mcp Key 使用真实 schema 的 NULL tenant + tenant-scope 子表。正常 HTTP 使用随机临时 Key 的真实 hash 查库，日志不写密钥明文。

服务测试覆盖：A 非目标成员公共读取；B Web/原私人 Key；C 无显式授权拒绝；D 授权和撤销；selected scope、已撤销 Key、机器主体、未知来源/缺审核拒绝；空集与 COUNT；20 个并发请求；原 grant/scope 复用拒绝；直接 SQL 更新保留 updated_at 仍失效；内容在取 Grant 前更新并重新审批时只返回新内容。

实际路由测试覆盖五条正向读取、ID 猜测、分页 count、batch、不可信 Header、真实 Key scope、匿名资源 HOLD，以及 Handler 已生成正文后撤销 D 授权，flush 前丢弃正文。新增 Portal 取消发布的 HTTP 回归以及 Chunk batch/parent/neighbor 的真实仓库测试；最终状态见 [38](38_FIFTH_BATCH_DELIVERY.md)。Python tagged 测试使用实际核心服务，未用此前手写 adapter 代替本批证据。

边界：shared KB/agent 的真实复杂私人授权、全部 Worker、标签/衍生依赖、性能、全部请求取消与流式场景尚未完整认证；不得将核心五条路由通过视为全部 REST/Agent 通过。SQL 3035 的 Knowledge/Chunk epoch 复用既有机制，本批没有改迁移链、3033 或生产数据。

## 回滚

保留现有 worktree 和所有未提交内容，不执行 reset/clean。默认关闭装配即为运行期隔离边界；本批无数据库变更需 down。代码级回退必须按本批 changed-source manifest 与第四批快照逐文件审查，尤其保留此前 M2/M3/SSE 修复，不能整体覆盖旧版本。
