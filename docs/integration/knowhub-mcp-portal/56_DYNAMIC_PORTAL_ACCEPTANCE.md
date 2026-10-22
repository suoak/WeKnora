# 第七批动态公共 MCP 验收记录

以 [55 最终产品规则](55_DYNAMIC_PORTAL_PUBLIC_REQUIREMENTS.md) 为当前业务基准。此报告持续汇总新候选实测结果；不把旧审批模型下的 PASS 继承为新模式的 PASS。

## 已实现源码

| 路径 / 函数 | 当前行为 |
| --- | --- |
| `repository/enterprise_account.go` / `KnowHubPersonalUserVerifier` | 现有 KnowHub 个人账号有效性；不提供实时在职证明 |
| `service/portal_mcp_composition.go` / `NewPortalPersonalKnowledge` | 现有 APP、真实仓储、Key、成员/共享、模型及检索 registry 装配；缺配置关闭 |
| `mcpserver/personal_config.go` / `NewConfiguredPersonalServer` | 同 APP 的只读个人 MCP；不启动独立服务 |
| `access/enterprise_public.go`、`portal_published.go` | Published 默认授权，不依赖 approved；有效已索引文档动态纳入 |
| `access/portal_mcp.go` / `Resolve` | 公共和独立私有授权取并集；每请求和敏感边界重新校验 |
| `repository/portal_mcp.go` | 动态候选 KB，不保存 48 KB 静态名单 |
| `retriever/postgres/portal_scope.go` | 真实候选 SQL 校验 tenant/KB/Chunk/Published/显式拒绝；不依赖最终结果过滤 |
| `repository/portal_public_exception.go` / `SetPortalKBPublicBlock` | KB 禁止例外及恢复默认；CAS 与事务审计，不写 approved |
| `service/portal_public_scope.go`、`handler/portal.go`、`routes_portal.go` | Portal Owner/明确配置管理员的范围与例外管理；个人 Key 无管理权限 |
| `PortalPublicKnowledge.vue`、`SystemPortalSettings.vue`、`TenantInfo.vue` | 默认动态范围展示与简单 KB 禁止例外 |
| `MCPAccessKeys.vue` | 已有个人 Key 生命周期、明确企业能力、统一连接配置与连接检查 |

上表相对 `internal/application/` 或 `frontend/src/` 的路径应结合候选 manifest 查看。不是完整 M3 文档级 ACL 声明；Wiki/FAQ/Graph、任意文件下载及混合敏感文档治理不在本批完成范围。

## 动态场景

| 要求 | 对应实际证据入口 | 状态说明 |
| --- | --- | --- |
| 两个初始 Published 空间与私有知识联合读取 | `TestPortalM7PublishedDynamicRealPostgres` | 已在真实 PostgreSQL/ParadeDB 通过 |
| 第三个未发布空间排除，发布后同 Key 自动纳入 | 同上 | 已通过，无需重建 Key |
| 已发布空间新增 KB | 同上 | 已通过，包括空 KB 动态发现 |
| 新文档解析/索引就绪后纳入 | 同上 | 已通过；processing 文档即使预先存在索引也不返回 |
| 飞书新增知识 | `TestPortalM7PublishedFeishuIngressPostgres` | 已在真实 PG 通过；模拟远端与解析边界，禁止扩大为真实云端同步验收 |
| Portal 取消发布 | 动态 PG 测试、客户端 withdrawal | PG 已通过；客户端以新候选日志为准 |
| KB 移出空间、删除，历史 Chunk 不可读取 | 动态 PG 测试 | 已通过，旧 target 再校验失败 |
| 取消发布保留独立私有权限 | 动态 PG 测试 | 已通过 |
| 大 KB 与跨空间 Hybrid | `TestPortalM7RealPostgresScale` | 普通 PG 已通过 128/257/512/864/1001，最终 race 与完整套件另列 |
| 同 Key 内网与隔离只读入口范围一致 | `batch7c_network_check.py` | 实际 list 与 Hybrid 结果完全一致 PASS，不止 initialize |
| KB 禁止、旧 Key 无扩权、Key 撤销与伪造主体 | service/router 测试及新客户端日志 | 不删除或放宽授权断言；按新候选完整输出验收 |

## 新证据与候选

`evidence/batch7/published/candidates/` 保存基线、全文件 SHA256、归档 SHA256 和命令阶段。`published/linux/` 保存原始命令、退出码、容量及 PostgreSQL 日志。`published/local/` 保存前端测试、类型检查和构建输出与逐文件输入指纹。`published/clients/`、`published/browser/` 和网络协议日志保存真实客户端与界面证据，不记录密钥明文。

首轮动态 PG 候选 `a59c5bf118c539b3…` 的 integration 全部命令退出 0，包含真实新规则检索及规模测试。后续候选包含新增飞书入口测试、最终页面和客户端工具，必须使用后续实际结果，不能以首轮局部通过代替整体验收。

## 环境与限制

隔离 Linux Docker、Go 1.26.7、PostgreSQL 17.9、ParadeDB pg_search 0.22.2、pgvector 0.8.1、受控 Python 3.11；所有知识、账号与远端响应为合成测试数据。1024 维真实数据库索引和模型服务适配器已测试，模型 HTTP 返回受控向量，不能代表生产模型质量或跨语言准确率。

真实 SID 登录仪式、生产 80/443、实际隔离网络通道、本批生产迁移/启用、真实飞书云端、未知敏感内容识别均尚未验收。现有业务查询确认的是账号有效性，不是实时在职。2048 以上单 KB 快照预算仍 HOLD；非 PostgreSQL 的公共安全预过滤支持未声明完成。

完整套件若仍 FAIL，应与聚焦 PASS 并列保留。Windows symlink 权限 FAIL、旧批次迁移和 SSE 原始 FAIL 不删除、不覆盖。客户端模型完成回复但未调用 MCP 工具，应记客户端验收 FAIL。

最终 service 补齐隔离 Redis 后完整包退出 0；此前缺 Redis 的全套 FAIL 保留。Codex 的五工具、另一员工隔离、Portal 撤销及 Key 撤销通过；本地 WorkMate 连接发现成功，但会话无知识工具 schema、真实 tools/call 为 0，故 FAIL。完整门禁和候选对应见 [57 最终交付](57_SEVENTH_BATCH_DYNAMIC_DELIVERY.md)。

## 生产影响与回滚

没有生产写入、自动批准、成员变更、凭据轮换或部署。生产配置未开启个人 MCP。关闭 `personal_mcp.enabled` 即可关闭新能力；部署前需单独批准候选、正式迁移与网络配置。隔离网络入口使用同 APP 的 `/api/v1/mcp/personal`，不需要新 Go 监听器。旧 Python 和团队 MCP 保留。

发生问题时保留新日志与源快照，按 manifest 回退本批文件，不整体清理工作树。先前 6,677 文件归档完整回读成功，前六批 461 份报告/证据未变化，见 `published/preservation-verification.json`。
