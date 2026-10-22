# 第六批交付：Portal 企业公共知识优先 MVP

**生产默认关闭，混合敏感 KB 完整 M3 继续 HOLD。** 本批按新业务优先级实现个人 Go MCP 与安全整体 KB 检索，没有等待完整文档 ACL 才开始个人入口，也没有把 Portal published 解释为历史资源自动公开。

分报告：[39 业务 ADR](39_PORTAL_MCP_MVP_ADR.md)、[40 个人 Go MCP](40_PERSONAL_GO_MCP_IMPLEMENTATION.md)、[41 真实检索](41_REAL_POSTGRES_SEARCH.md)、[42 模型/引用安全](42_MODEL_REFERENCE_AND_SCOPE_SECURITY.md)、[43 生产只读统计](43_PRODUCTION_COVERAGE_READONLY.md)、[44 测试环境与结果](44_SIXTH_BATCH_TEST_RESULTS.md)。

## 实现和验收边界

| 类别 | 当前交付 |
| --- | --- |
| IMPLEMENTED | Portal Published 动态范围、公私独立并集、真实 owner/Key/操作绑定；PostgreSQL/ParadeDB candidate WHERE；个人 Go MCP + 现有 user_mcp Auth；5 个只读工具；准确来源、RRF 去重、部分失败、模型/输出复验；默认 disabled |
| PASS | 本候选隔离 Linux 的整体安全 KB MVP 场景、真实 PostgreSQL/Hybrid、Go MCP HTTP、模型 spy，以及完整相关回归；详见末尾，不能扩展为生产/完整 M3 |
| FAIL | 本批 Windows 完整套件 55 tests / 1 symlink ERROR / exit 1；全部旧 FAIL 和中间 PG/夹具失败保留 |
| BLOCKED | Windows 当前进程不具真实 symlink 权限；生产缺正式企业 Provider/角色映射与历史分类，当前不能启用 |
| NOT_TESTED | 生产发布/迁移、生产规模性能、全部模型/driver、全部客户端 UI、多 Worker SSE、复杂 shared/agent 与混合 KB 完整 ACL |
| NEEDS_DECISION | Portal 发布、MCP 功能启用、敏感审核三职责正式角色；企业资格来源；用户级 deny 语义；可信新增普通资源分类/继承 Provider |

## 十项需求映射

| 需求 | 证据和结果位置 |
| --- | --- |
| Portal 新业务规则与 PolicyAuthority 兼容 | 39；保留首次确认、CAS、审计，不在每 KB 创建团队 MCP |
| 动态公共 KB 范围 | 40；`PortalMCPResolver` 每次请求及关键边界复验 |
| 实际 PostgreSQL/ParadeDB、Hybrid | 41、44；真驱动和正式 3035 合成库 |
| 员工个人 Go MCP | 40；真实 HTTP initialize/tools/list/tools/call，复用既有 Key/Auth/SDK |
| 公共和私人联合检索 | 44；两个空间公共 KB + 员工独立私人 KB；旧 Key 不获跨空间公共能力 |
| Portal 和私有授权撤销 | 44；Portal draft/archive、成员/Key、查询期间撤销；私人授权独立 |
| 普通与混合 KB 隔离 | 39、40；混合/未知/版本不明整库排除公共；不将后过滤视为预过滤 |
| 工具、模型输入与引用 | 42、44；现有模型客户端真实 HTTP spy；敏感 canary、错 owner、旧正文索引；Wiki 三工具不暴露 |
| 新源码与 SHA256 | `evidence/batch6/change-inventory.json`、`delivery-validation.json`、候选 manifests；原始源码 tar 逐 member 验证 |
| MVP/完整 M3 与上线前置条件 | 本报告、39、43；受控 MVP 与生产/混合敏感 M3 分别结论 |

## 生产真实覆盖

本批两次固定公钥只读聚合确认：**17 个已发布空间、48 个 KB、2718 条知识，3033 clean**；生产没有本方案分类/版本四表，**目前有可核验安全确认依据的公共空间/KB 为 0/0，48 个 KB 均需首次确认或维持受限**。不推断它们都敏感或都普通。

另有 **4 个 KB 超过 256 文档上限，最大 864**，需要安全的大范围过滤适配/性能验证或可靠拆分。不能仅提常量、删除 ACL 或自动补 approved。没有公开生产历史资料、源正文或凭据。详情见 43 原始只读证据。

## 工具目录与剩余 HOLD

暴露 list_knowledge_bases、search_knowledge、grep_chunks、list_documents、read_document。暂不暴露 wiki_search、wiki_read_page、wiki_index；ask/上传/修改/删除不注册。机器 team endpoint 原行为保留，普通 full_access 不可变成员工入口。

文本及 knowhub 引用不携带对象直链、原始附件、图片或匿名签名 URL。旧 URL/CDN 无法即时撤销的问题没有因本批而解决。Wiki/FAQ/graph 持久化来源、历史缓存/引用、来源 ACL、混合 KB 内文档级安全检索继续完整 M3 HOLD。

新个人入口对未知分类或仅部分文档可读的旧私人 KB 也采取 HOLD；原有默认关闭的 Web/Python/团队入口仍保持原业务行为。因此不能宣称已经替代所有原私人知识访问方式，也不能将个人入口验收扩展成整个旧 REST/Agent/文件读取面的完整 ACL 验收。M5 已实现受控核心 REST 组件在本候选回归，生产未切换。

## 保留、回滚和停止点

原 worktree：`E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021`；branch：`audit/knowhub-mcp-b4-rc3-20261021`；Git 基线 `e62b17e2d305358467821aeff1cce63ce1d61254`。HEAD 不是未提交代码标识。

保留 00～38 报告、全部旧日志和未提交成果。第六批开始完整快照：`E:/AIData/Codex/knowhub-b6-preserved-20261021.tar.gz`，6,209 个文件逐 member SHA256 验证匹配初始 manifest。第五批 157、第四批 194、前三批 1,198 项材料最终再校验；本批新增源码/报告不写入旧 artifact manifest。

没有修改既有迁移、生产 Python、生产数据库或 Key，没有自动增员、自动审批、git commit/push/merge/release/deploy。新卷/新 PG 数据/停止的测试容器和源码候选保留，没有清理历史文件。

回滚以默认 disabled 为运行边界，无本批 DB down。源码必须按本批 changed manifest 与第五批快照逐文件审查，保留此前安全修复；不得 reset/clean。第七批建议先落实角色/员工 Provider、可信首次分类流程与 4 个大 KB 安全适配，再做 Portal UI、个人 Key 显式能力选项和实际客户端联调；不直接进入生产启用。

## 最终冻结候选和命令结果

两份冻结源与执行分别绑定：

- 完整相关套件：`fe6f77e896ec82aacf45a416d760e20b02ed171485ddd4fb6790308c2cc97eac`。
- 补充删除状态断言：`a93d6c107c80ef7428e27c536bf84e4dcceb89620b63935354ca934bd5095cb0`。
- 当前测试输入集合 SHA256：`dd0acc813e162fb478a984038ffa2701295ccaf9a42b6002b64b4640acd05cb0`。

完整套件后仅给带 `knowhub_m6_postgres` 标签的同一测试文件补充 Tenant/KB deleted 两条数据断言，应用源码完全未改；补跑整个该 PG 集成，不重复无关完整套件。交付校验器只允许这一明确测试差异；当前 runtime 与完整套件逐文件一致，当前 PG 断言与补充候选一致。报告 prose 及离线 checker 可追加结果，其当前 SHA 另列入交付清单，不混称为执行过的应用代码。

两个服务器源目录各 **5,932 文件**执行后重新读回验证，无变更；18 个新增/修改 runtime/测试文件、4 个本批工具和 7 个报告共 29 项变化均有旧/新 SHA256。完整 [源码/测试/命令清单](evidence/batch6/delivery-validation.json)、[变化清单](evidence/batch6/change-inventory.json)、[服务器源读回](evidence/batch6/remote-source-readback.json)、[本批产物 manifest](evidence/batch6/artifact-sha256.json)。前三批 1,198、第四批 194、第五批 157 项材料校验通过。

| 实际执行 | Exit | 结论 |
| --- | --- | --- |
| PostgreSQL 正式空库 0→3035 + 真实检索/个人 Go MCP/模型 spy/并发与查询撤权（race） | 0 | PASS；pg_search 0.22.2 / vector 0.8.1 |
| 追加 deleted 状态后的同一完整 PG 集成（普通执行） | 0 | PASS；未将两条新断言扩展称为 race PASS |
| 相关 Go 完整套件（包括 driver/database/handler/container/mcpserver） | 0 | PASS；并非全仓库 `go test ./...` |
| 相关 M2/M3/M5/M6 授权场景 race | 0 | PASS；并非全仓库 race，部分包在 pattern 下无匹配测试 |
| 相关包 vet | 0 | PASS |
| Linux Python 完整 55 tests | 0 | PASS，真实 symlink |
| 实际旧 Python → REST HTTP/SSE SDK 集成，构建/执行 | 0 / 0 | PASS；单 Worker，SDK 2.2.0；不扩展为多 Worker/生产部署 |
| 完整 service 测试，构建/Python 3.11 镜像执行 | 0 / 0 | PASS；含 tomllib 环境依赖 |
| 完整 agent/tools 测试，构建/执行 | 0 / 0 | PASS；不等于所有 Agent ACL 正向接入已完成 |
| Windows Python 完整 55 tests | 1 | FAIL / BLOCKED_OS_PRIVILEGE，1 个 WinError 1314 |
| 容量门禁 / 源码及历史证据校验 | 0 | PASS；卷观测高水位约 14.38 GiB，无 ENOSPC/历史文件删除 |

完整 [命令数组/退出码](evidence/batch6/linux/results-fe6f77e896ec82aa-all/results.json)、[PG 补充结果](evidence/batch6/linux/results-a93d6c107c80ef74-integration/results.json)、[模型/个人 MCP/并发原日志](evidence/batch6/linux/results-fe6f77e896ec82aa-all/postgres-personal.log)。所有 setup/middle-candidate FAIL 原日志保留，Windows 失败不由 Linux PASS 覆盖。

**结论：隔离环境整体安全 KB 的 MVP 已有上述范围内可复现 PASS；生产启用继续 BLOCKED，完整混合敏感 M3 继续 HOLD。** 正式员工/角色 Provider、历史分类、4 个大 KB、链接/衍生 ACL 与规模性能未完成前，不能据此开启生产公共授权。没有 commit、push、merge、release、deploy 或生产迁移。

向量检索是实际 pgvector 的 3/4 维合成验证；生产模型维度下的 HNSW 执行计划、召回/性能及正式 store/model registry 装配尚未认证。当前 Engine/Embed/员工范围使用显式受信注入，生产容器仅装配 disabled server；不将隔离 adapter 配置视为生产已接通。
