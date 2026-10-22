# V1候选合入前代码审查

本记录是执行者的源码自审与测试证据交叉核对，不冒充独立人员审批。GitHub检查及实际PR审查结果须在最终交付补充。未通过必需CI不得合入。

| 检查面 | 核查位置 | 结论/保留边界 |
|---|---|---|
| 个人真实身份 | `internal/middleware/personal_mcp_auth.go::PersonalMCPAuth`，`repository/enterprise_access.go`及账户verifier | 每次验证user_mcp、OwnerUserID、retrieve、用户active；dynamic_all查询有效membership；机器/平台Key不能冒用；不将账号检查称为实时在职 |
| 公共和私人范围 | `access/portal_mcp.go::Resolve`，`access/portal_published.go`，`service/portal_mcp_composition.go` | PublishedDefault实际启用；公共来源不依赖旧approved；独立私人授权保留；动态模式显式选择，历史fixed不改；>2048整KB拒绝，非完整敏感文档ACL |
| 请求/输出边界 | `mcpserver/personal.go::Handler/guard/call` | 仅八个只读工具，tools/list与call一致；30s、1MiB、并发8；重新核对授权；仅来源收紧可丢弃旧响应后重算一次，身份/数据库错误不容错扩大 |
| 真实候选检索 | PG/SQLite检索器与`service/portal_mcp_models.go` | allow-set绑定资源真实tenant/KB/document；空集不全库；模型按KB tenant获取，不静默跨模型；外部输入前/输出前复验；真实PG及spy断言保留 |
| 普通链接文本 | `access/portal_text.go`与相关测试 | 剥离目标URL保留正常文本；未经授权文件/预览/对象链接不泄漏；不删除真实资源授权 |
| Wiki证明 | `service/portal_mcp_wiki.go::PersonalWiki`、`repository/wiki_page*` | 仅发布页+完整来源+持久generation证明；旧/混合/撤权/未知来源拒绝；重复slug消歧；index读取无创建副作用；上限和截断显式 |
| 飞书原子切换 | `repository/feishu_legacy_atomic.go::Begin/Prepare/Publish/Receipt/validateLegacyCursorReceipts`、staging与replay | 稳定KID；准备和发布分开，事务发布canonical Chunk+PG索引；generation/fence验证；完成收据前不推进Cursor；不以HTTP临时异常当可信删除 |
| 重试/恢复 | `repository/datasource_execution.go`、`datasource_repo.go::CreateIfNoRunning`、`service/datasource_execution.go` | 数据库锁及generation为权威；Redis不是授权事实来源。AUTO_MIGRATE=false不运行启动清理，但真实同步准入能回收已过期执行；人工时间注入与真实等待区别记录 |
| 来源持久拒绝 | `types/feishu_legacy_read_scope.go`、REST Knowledge/Chunk/文件及rich搜索调用点 | HOLD/revoked读取面一致，full_access不绕过；healthy来源partial可用；同步功能关闭不卸载读取保护 |
| Key存储/生命周期 | `service/tenant_api_key.go`、user_mcp handler/repository及前端生命周期 | 有AES才可恢复加密存储，无AES不回退明文；本人reveal；rotation CAS，旧secret失效；旧expiry/scope不变；配置不将Secret写入Codex TOML |
| 迁移/回滚 | 3034～3037及正式runner；3037多页修复；硬化回滚策略 | 保留3033定制，不补造3032；真实恢复/升级验证；3037拒绝默认、不生成旧Wiki证明；不默认down；禁止已知不安全旧RC3 |
| UI/路由/代理 | 原`/platform/mcp-access`、`MCPAccessKeys.vue`、`personalMcpConfig.ts`、Nginx精确路径 | 左侧入口保留；名称+默认无限期；真实`/api/v1/mcp/personal`、Bearer、POST及streaming；保留历史凭据管理，不自动升级；最新修复只换等值主题token，无行为变化 |
| OA/Portal/个人飞书并行代码 | 与e62b17比较，源码保留而非旧文件覆盖 | 未重写SID/OA或整合整个上游main；原有导入/Docx/Sheet/Bitable测试由完整Go及前端全套覆盖；真实云端未获批不推断PASS |
| Secret与材料 | frozen Manifest、已知私有值扫描、JWT/Key模式扫描、逐文件SHA | 私有env、DBdump、原始签名URL及解密凭据不入Git；原历史材料保留；Git树行尾归一化需要单独记录，不能把文件SHA称为commit |

当前PR可采用默认关闭的代码集成边界；真实模型、真实飞书云端与生产积压仍为启用生产能力的门禁。混合KB完整文件ACL、Wiki全量来源补全、WorkMate及未来MCP写入不纳入本PR新增范围。未发现需要为MVP重新引入HR Provider或逐KB审批的理由。
