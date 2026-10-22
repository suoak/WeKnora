# 本轮需求符合性与验收边界

原 b470 联合候选、5468 V1、独立飞书补丁及历史失败证据均保留。本轮只在 `knowhub-v1-wiki-dynamic-20261022` 开发，不将文件 SHA256 当作 Git commit。最终状态以本轮冻结 Manifest 和集中验收证据为准，本文不构成上线批准。

最终状态（2026-10-22）：完整候选 c2f260317fc151fb、业务源码 b702755c 已完成隔离合成联合验收，所有集中命令退出0。精确证据以 `V1_WIKI_DYNAMIC_FINAL_DELIVERY.md`、`V1_WIKI_DYNAMIC_TEST_RESULTS.md` 和 `evidence/v1-wiki-dynamic/delivery-gates-c2f260317fc151fb.json` 为准。真实SID、正式模型、真实飞书云端及生产积压继续NOT_TESTED，历史WorkMate FAIL不改写。

| 要求 | 已实现源码 | 当前证据与剩余门禁 |
|---|---|---|
| 单来源 HOLD/revoked 不拖垮健康范围 | `access/portal_mcp.go`、`portal_diagnostics.go`、`service/portal_mcp.go`、`repository/knowledgebase_read_isolation.go` | 实际 PG/ParadeDB BM25/向量/Hybrid/grep、健康直接读取、HOLD拒绝、partial及最终 APP/Nginx/Worker 联合验证通过 |
| 显式请求范围只能收窄 | Resolve 保留不可变 selected 集合，受限来源仅从 outstanding 集合移除 | 受限 KB 排首位的 SQLite 回归；未知 KB 继续拒绝，空请求授权结果不退化成全库 |
| 请求期间来源撤权 | 个人 MCP 仅对八个只读操作允许一次重新解析并重试；第一次输出整份丢弃 | 实际 PG 测试在 Embedding HTTP 边界撤销一个来源，验证健康私有 KB 仍返回 partial；全局身份、未知错误不重试 |
| REST 列表和检索隔离 | 只读 KB 批量解析逐 KB 验证，响应 `Warning` / 部分范围标记 | 严格写入和通用批量函数不放宽；实际APP健康读取/列表partial通过，受限直接读取及历史引用拒绝通过 |
| 模型输入 | 原个人候选前精确过滤、输出前 grant 复验保留；Wiki 服务端输入闭包在 Chat/Embedding 边界验证 | 未证明输入范围的旧 RAG/Agent 保留租户级保守拒绝，不能宣称所有历史模型路径都已实现部分降级 |
| 安全回滚 | `next_rollback_build.py`、`next_rollback_policy.py`；硬化补丁基底加3037正式迁移材料 | 同一未过期旧HMAC、REST、历史诊断、功能关闭、备份恢复及返回新候选9项门禁通过；镜像/证明绑定许可，不是生产操作授权 |
| 八工具只读白名单 | `mcpserver/personal.go` 复用三个 Wiki schema；服务端白名单与注册一致 | 八工具真实HTTP、账号/Key鉴权、PG调用和最终候选实际Codex八工具及正文语义检查均通过 |
| Wiki 无写入目录 | `service/portal_mcp_wiki.go`、`repository/PersonalWikiStore` | 不调用 GetIndex/GetIndexView；缺目录时返回有界目录/空结果，不创建页面。SQLite/PG 行数断言已通过 |
| Wiki 来源闭包 | `wiki_mcp_provenance.go`、真实 Wiki 仓库 Create/guardedWikiWrite、Wiki 批处理生成上下文 | 必须可信服务端生成证明，绑定完整输入 KB；多来源全部有效、全部授权才读。未知旧页不补证明；混合未知页隐藏 |
| Wiki 版本与并发 | 3037 Wiki 原始写入失效触发器、KB 生成输入栅栏、已有文档 epoch、正文摘要 | 原始写入及 ABA 不能恢复旧证明；finalizing→completed 不单凭时间误判内容变化。输出前重新核对页面发布状态、正文和全部来源 |
| Wiki 历史队列输入 | `wiki_mcp_generation_context.go`、真实 MAP/Finalize 调用边界 | 个人MCP启用时，不可信撤回正文停止生成并保持HOLD；目录重建忽略旧队列正文/标题，改用当前已证明页面及文档。未配置/关闭个人MCP的既有生成行为保留；真实模型生成质量未验收 |
| Wiki 体验及上限 | slug 重复消歧、精确 KB ID、literal 搜索、offset/limit、正文截断、15秒操作/30秒请求、服务端8个并发请求上限 | 超限429并可重试；单 KB 最多200页、最多128 KB、文档闭包最多2048；超过能力明确部分/受限，不声称无限目录或完整 M3 文件治理 |
| 动态“我的全部知识” | `TenantAPIKey.PersonalScopeMode`、`GetUserMCPTenantScope`、PersonalMCPAuth、创建 API/UI | 新 Key 明确 dynamic_all；每请求重新验证真实 OwnerUserID、有效用户、最新成员关系、有效资源、Key 状态和能力；非成员不取得私人 KB |
| 历史受限 Key | 缺省 fixed、模式不可原地修改、重连接保留原模式 | SQLite/实际PG及APP两员工新增成员、撤权、历史fixed不扩权；UI创建/重新查看/轮换与原日期权限不变通过 |
| Portal 默认动态公开 | 原 Published resolver 保留，不依赖逐 KB approved Grant | 实际管理路由发布新空间、后建KB/索引文档自动发现，退发布保留私人授权均通过；无重复审批或成员关系 |
| 飞书 Wiki/Drive | 原稳定 KID、原子索引发布、fencing、持久拒绝、Cursor 完成收据保持 | 本轮核心写入文件与b470一致；最终候选实际APP/Worker+PG合成全生命周期、旧来源恢复、失败保旧、撤权、Cursor和原子切换/race重跑通过 |

## 迁移与部署限制

3033—3036 既有迁移不修改；新增3037保存显式 Key 模式和 Wiki 证明/输入栅栏，历史 Key 默认 fixed，历史 Wiki 不生成证明。正式迁移必须在停止接收请求、重启 APP 连接池后进入新版本；跨 DDL 继续使用旧 SELECT * 缓存不属于支持的热迁移方式。3033→3037、允许条件的 down/up 已在隔离 PG/SQLite 验证；存在动态 Key 时 down 必须拒绝。部署回滚保留3036与3037，不默认执行 down。

兼容回滚会暂时失去新版个人 Go MCP 产品能力，但保留撤权读取保护；不能退回已证实旧 HMAC 返回200的原 RC3 Reader。生产实例、停更开关、凭据和网络均不修改。

## 仍需正式环境验证

真实 SID、正式模型、飞书云端权限事件和生产积压没有用合成测试替代。未知云端权限继续 HOLD。WorkMate 历史 tools/call FAIL 保留并暂缓；本轮使用 Codex 进行八工具实测。完整 Wiki 云端模型生成质量、超过上述上限的规模及全部历史衍生资源治理不作已完成声明。
