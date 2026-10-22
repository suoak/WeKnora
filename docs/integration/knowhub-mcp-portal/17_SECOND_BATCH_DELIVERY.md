# 第二批交付与验收建议

实施与测试：2026-10-20；交付记录：2026-10-21（Asia/Shanghai）。分支 `audit/knowhub-mcp-portal-20261020`，worktree `E:/code/WeKnora-worktrees/knowhub-mcp-portal-20261020`。基线 HEAD 仍为 be308d0058db4e647cd5103ae27b39da3a20eace；未提交源码由19个核心变更文件的SHA256和Linux候选overlay绑定，不能只用HEAD代表本批源码。

## 交付状态

| 类别 | 本批结果 | 明确边界 |
| --- | --- | --- |
| 已完成设计 | [13：M1 ADR](13_M1_FINAL_DECISIONS.md)、更新04–08、[15：资源与缓存威胁模型](15_RESOURCE_LINK_CACHE_THREAT_MODEL.md)、[16：M3调用链与顺序](16_M3_CALL_CHAINS_AND_EXECUTION_PLAN.md) | 拒绝默认、独立公共能力、独立审核、收权优先、匿名资源激活门禁已定；实际角色/目录映射仍需人工确认 |
| 已实现源码 | [14：M2实现和测试](14_M2_IMPLEMENTATION_AND_TESTS.md)、access公共正文Grant、严格投影、revision/CAS+事务审计、三表003034双数据库迁移、DENY_ALL/allow-set及衍生源接口 | 默认关闭、没有新HTTP/MCP路由或生产DI；没有重做Go MCP/个人Key体系/检索引擎 |
| 已验证功能 | SQLite与PostgreSQL的批准正文读取/撤销/版本/并发、无成员或管理权、旧Key无扩权、混合/外部/未知整库HOLD、空集拒绝、迁移回滚 | 只用合成夹具；实际Hybrid/多空间检索、旧REST/文件/Wiki全链路仍未验收 |
| 需人工审批 | 企业员工资格来源；内容负责人/发布审核人实际角色映射；历史资源批准范围；未来继承与来源ACL；旧匿名链接处置 | 全部NEEDS_DECISION，未临时授予任何真实用户或Key权限 |
| 尚未测试 | M3旧Python REST全读取面、真实driver预过滤、文件broker/CDN/对象直链与历史引用、真实Rerank/LLM spy、Codex/WorkMate/Claude Code、P50/P95/召回、生产灰度/回滚演练 | NOT_TESTED，不以组件PASS替代生产验收 |

## 实测结果及原始证据

完整命令、退出码、开始/结束UTC时间、镜像digest、逐文件摘要、原始输出在 [evidence/batch2](evidence/batch2)。Linux测试使用独立新目录和network=none的一次性数据库/测试容器；生产主机、数据库、Python服务、密钥、历史知识及成员权限均未写入或变更。测试结束后本任务容器剩余列表为空，见 environment-final.json。

| 检查 | 结果 | 原始记录 |
| --- | --- | --- |
| Windows middleware完整回归 | PASS | windows/knowhub-m2-middleware-windows.log，TMPDIR/TMP/TEMP修复保持原断言 |
| Windows Python transport | PASS，14/14 | windows/knowhub-m2-python-windows-final.log；实际HTTP200和无session-id；原失败输出保留 |
| Windows M2访问/仓库测试 | PASS | windows/knowhub-m2-tests-windows-retry.log、knowhub-m2-repository-final.log；PostgreSQL在Linux单独实测 |
| Windows database完整回归 | PASS | windows/knowhub-m2-database-windows-final.log；真实迁移器fresh/legacy/up/down/up、迁移目录和治理检查 |
| Linux核心完整Go测试 | PASS：access、types、repository、middleware、mcpserver、router | linux/go-full-bookworm.log中逐包结果；同一完整命令另有service/agent失败，未把整条命令改记PASS |
| Linux race | PASS | linux/go-race.log（access/repository/middleware/mcpserver）；runtime修改后的access/repository再次通过go-race-final.log；PostgreSQL新增读取扩展也在-race下执行 |
| PostgreSQL迁移、CAS及JSONB正文 | PASS | linux/postgres-jsonb-read-final.log、postgres-go-cas.log、postgres-results.json；含并发approve/revoke、实际当前Key JSONB读取、非成员批准正文及Portal撤销 |
| 冻结生产3033源码的空库→003034→down→up | PASS | PostgreSQL执行完整冻结源码SQL链，再验证新增迁移前后原表schema指纹一致；不是生产数据库迁移 |
| SQLite实际runner升级及回滚 | PASS | Windows与Linux database完整套件；3027旧库→3034→down→up、旧Key保持、历史策略为空；另有3033形状兼容夹具 |
| Linux Python完整回归 | PASS，53/53 | linux/python-full-offline.log；使用已缓存镜像中的mcp2.2.0，Windowstransport使用mcp2.3.0，版本未混称相同 |
| Linux agent/tools完整回归 | PASS | linux/go-service-agent-full-fixtures.log；可用ensurepip运行时后通过 |
| Linux service完整回归 | **FAIL，仍有1项失败** | TestSkillPythonVerifier/a_pyproject.toml_dependency_the_venv_does_not_carry；同文件含全部原始断言输出 |
| 新企业能力服务测试 | PASS | linux/enterprise-capability-final.log；显式能力、retrieve前置、默认及旧能力不变；不替代service完整FAIL |
| Linux静态检查 | PASS | linux/go-final-vet.log；没有跳过授权断言或关闭SSRF检查 |

剩余service失败在Python3.10.18运行时下复现，该运行时缺少tomllib和tomli，见 python-runtime-diagnostic.log/environment-final.json；该测试期望检测缺失的pyproject依赖却没有返回错误。与该环境限制吻合，但未对无关skill验证逻辑做修复，也不把完整套件标绿。后续需在支持tomllib的受控运行时验证或由对应维护人处理兼容性，完整CI门禁仍保留。

原始失败同样保留：首轮源码包缺docreader/docs依赖、宿主libstdc++过旧、pip TLS；一次性PostgreSQL临时初始化服务器与最终服务器切换、socket目录归属问题。测试工具分别补齐源码、使用现有Bookworm工具镜像/离线MCP镜像、TCP readiness与每次独立socket目录；没有修改生产环境或降低权限断言。网络隔离下已有sandbox测试需要公共域名DNS，复测使用明确的两条DNS fixture，网络仍为none，没有设置SSRF绕过开关。

## M1安全收口与待定事项

设计收口的含义是确定拒绝默认、审批版本、独立主体/能力、正文与文件许可边界和激活阻断条件；**不等于旧生产REST、匿名链接或文档ACL缺口已经修复**。

实际角色映射与企业资格provider没有绑定，缺省拒绝。PolicyAuthority必须逐次核查明确批准的业务角色；不能将当前Portal system_admin/空间owner自动加为企业发布审核人。M2组件构造选项默认为false，没有生产路由或开关自动开启。

M3仍必须解决：所有内容写入路径的可信版本更新；个人显式文档grant；源ACL撤销；真实候选前过滤；Wiki/FAQ/graph/summary/cache来源闭包；历史匿名签名/对象直链/CDN缓存收口。无法及时撤销的资源继续HOLD。M2组件正文中的链接保守阻断不能替代真正的资源broker。

## 已验证回滚与下一批验收

003034在隔离PostgreSQL和SQLite已down/up验证，不改旧用户、Key、Portal状态和成员关系；新表无自动公开回填。M2无运行接线，因此停用无需生产动作。未来一旦真正施行拒绝策略，回滚必须保留安全守卫和策略数据，不能简单删除表或回退到无ACL的旧后端。

下一批优先顺序按16执行：先定角色/企业资格与个人文档grant → 统一knowledge/chunk服务和旧REST → driver候选前过滤及模型输入spy → Wiki/FAQ/图谱/缓存 → 受控文件broker及历史链接撤销 → 飞书/GitLab失权/同步 → 完整C/D/F矩阵。所有混合敏感KB继续关闭，Go个人入口与客户端迁移继续留在后续门禁之后。

建议验收本批“默认关闭组件与合成策略能力”，不批准真实历史知识公开或生产切换。service完整回归剩余FAIL、M3全路径和真实检索/客户端/性能NOT_TESTED须在后续发布评审前单独关闭。当前版本检查逐资源metadata，未提供真实规模性能保证；性能优化不能以跨请求过期授权缓存换取。

本批停止于commit、push、merge、release与生产部署之前；没有暂存或提交当前分支，也没有改动原工作树已有企业定制。
