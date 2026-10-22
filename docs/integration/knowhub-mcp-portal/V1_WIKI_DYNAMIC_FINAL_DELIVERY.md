# Go MCP 八工具、动态私人授权与飞书安全增量联合交付

状态：本候选隔离合成联合验收 PASS，可进入获准正式测试环境；生产发布仍未批准，真实环境门禁尚未完成。日期：2026-10-22（北京时间）。

## 管理摘要

本轮保留既有五工具、Portal Published 动态公开和飞书安全写入状态机，新增三项 Wiki 只读工具、新 Key 的 `dynamic_all` 私人空间授权、来源级部分结果隔离及安全回滚门禁。未引入 HR Provider、逐 KB 审批、机器身份替代个人身份、新 MCP 服务或端口。生产、停更和旧 Python MCP 均未修改。

RC3 HEAD 为 `e62b17e2d305358467821aeff1cce63ce1d61254`，所有本轮及继承改动仍未提交。开发工作树 `E:/code/WeKnora-worktrees/knowhub-v1-wiki-dynamic-20261022`；完整冻结候选 `c2f260317fc151fbab98d9e87b2fe5c9acbd903dd4be2e857e0326565d390d75`，业务源码清单指纹（包括测试）`b702755c033328560bbb28802c93a3a1abce31a43352870b43d8f1fee2d74cdb`。两者均为 SHA256，均不是 Git commit。

完整源码包 `E:/AIData/Codex/knowhub-v1-wiki-dynamic-c2f260317fc151fbab98d9e87b2fe5c9acbd903dd4be2e857e0326565d390d75.tar.gz`，包 SHA256 `cb08c31f06d1565196ae0bc93458d10069a73e803b08fa2ffe984a8fe3e67437`。逐文件 Manifest、依赖/迁移清单、Secret 模式扫描及远端回读证据见 `evidence/v1-wiki-dynamic/c2f260317fc151fbab98d9e87b2fe5c9acbd903dd4be2e857e0326565d390d75.json`。扫描没有发现新模式命中，不等于绝对不存在秘密。

开发完整 Go、相关包完整 race、vet、构建、真实 PG/ParadeDB、新增安全测试与 SQLite/PG 正式迁移全部退出0。完整相关包 race 使用25分钟预算，实际约718秒。最终同源镜像构建、集中部署、真实浏览器、实际 Codex 八工具、飞书合成来源真实 Worker 生命周期及九项兼容回滚门禁均通过；集中完整 service/agent、Python与恢复命令全部退出0。证据均绑定本候选，不借用中间候选 PASS。

本轮两个 P0 在隔离合成范围已关闭：来源 HOLD/revoked 对健康来源的拖累，以及无安全回滚目标。前者有实际 PostgreSQL/ParadeDB、APP、REST、MCP、模型 spy 和飞书 Worker 证据；后者仅允许经证明的硬化兼容镜像，明确禁止直接回退旧不安全 APP。

## 需求与实现

| 需求 | 对应实现 | 安全边界 |
|---|---|---|
| 单来源隔离 | `access/portal_mcp.go`、`portal_diagnostics.go`；`GetReadKnowledgeBasesByIDs`；个人 MCP 只读重试 | 仅识别明确来源拒绝，排除该来源；未知错误/数据库/身份失败整体拒绝。无健康范围则拒绝。丢弃整份旧响应后最多重试一次 |
| 部分结果 | MCP 状态/警告；REST `X-KnowHub-Knowledge-Scope: partial` 与 Warning 299 | 不在警告中泄漏受限 KB 标题或 ID，不宣称已检索全部范围 |
| 模型边界 | 原检索候选前精确过滤及输出复验；Wiki 服务端证明绑定输入闭包 | 未证明输入范围的旧 RAG/Agent 保留保守拒绝，并非所有历史 Agent 都已支持部分降级 |
| 八个只读工具 | `internal/mcpserver/personal.go`，复用 `tools_wiki.go` schema；`PersonalWiki` | list/call 一致白名单，未知及写工具拒绝；统一真实个人主体、30秒请求、8并发、1MiB请求上限 |
| Wiki 读取 | `wiki_search`、`wiki_read_page`、`wiki_index` | 跨当前授权 KB；只读，不调用会创建页面的 GetIndex/GetIndexView；重名 slug 消歧，不先返回正文 |
| Wiki 可信来源 | `BeginWikiMCPGeneration`、`sealWikiMCPMutation`、`VerifiedPages`、`RevalidatePages` | 全部输入依赖须有效且授权，缺证明旧页面 HOLD；绑定正文、状态及文档/Chunk内容版本，原始写入与 ABA 不能复活证明 |
| Wiki 模型输入 | `wiki_mcp_generation_context.go`、真实 MAP/Finalize、Chat/Embedding 保护 | 启用时不可信历史队列正文/撤回内容拒绝；不把不可信标题先发送模型。正式模型生成质量未验收 |
| 全部私人知识 | `PersonalScopeMode`、`GetUserMCPTenantScope`、`PersonalMCPAuth`、创建 API/UI | 新 Key 显式 dynamic_all；每请求查询当前有效用户、Owner、成员关系及 KB。未来加入空间自动生效，撤权同步收紧 |
| 历史 Key | 缺省 fixed，模式不可原地修改 | 不扩权、不变更日期或能力；新动态模式不授予其他员工私人知识；机器凭据不能冒用 |
| Portal 动态范围 | 复用原 Published resolver | 无逐 KB approved 前置条件、无白名单；退发布不影响独立私人授权；无生产范围开放操作 |
| 飞书写入 | 保留既有稳定 KID、原子切换、fencing、拒绝持久化、Cursor收据 | `feishu_datasource_replay.go`、`knowledge_replace.go`、`datasource_service.go`、`datasource_execution.go` 与 b470 字节一致 |
| 安全回滚 | 硬化补丁基底兼容镜像、`next_rollback_policy.py` | 构建不等于允许回滚，必须逐项证明同旧链接/REST/历史引用/恢复仍拒绝。已知不安全旧 RC3 镜像明确禁止 |

Wiki 上限：15秒操作，单KB最多200页、最多128 KB，完整文档依赖闭包最多2048篇，分页 offset≤1000，正文有界截断并提示。超过能力保持受限或明确部分结果；不宣称无限目录、未标记混合敏感 KB 或完整 M3 来源治理已经完成。

## 候选关系与历史失败

原 V1 `5468da42`、独立飞书补丁 `02a8c5eb`、b470 联合包/工作树/实例均保留。e53 中间候选也保留：真实业务测试虽有通过，正式 tagged PG 迁移夹具退出1，因此不作为整体 PASS。

e53→本候选的业务源码差异仅为 `document_postgres_migration_test.go`：旧结构摘要排除3037新增表/字段，全部原字段仍逐项比较，另明确断言历史 Key fixed、历史 Wiki 未凭空生成证明。没有删除或放宽授权断言。证据 `post-freeze-migration-fixture-correction.json`。

其他原始失败保留：测试夹具缺非空模型字段、跨 DDL 缓存 SELECT* 结果形状、离线 DNS、10分钟 race 预算、合成 Chunk 复制 seq_id、探针容器名超过 DNS 上限等，分别记录修复和重新运行。不得将故障注入中预期 dirty 拒绝误写成真实迁移整体 FAIL，也不能覆盖真正失败的原命令结果。

## 数据库与回滚

既有3033—3036迁移不重新编号、不修改。3037为正式增量迁移，新增 Key 模式和 Wiki 证明/生成栅栏；历史 Key 默认 fixed，旧页面不自动生成证明。SQLite 与 PostgreSQL 均包含原始页面写入使证明失效的触发器。

迁移运行器在隔离旧库执行3033→3037、新库初始化、允许条件下 down/up、失败 dirty 拒绝与恢复。存在任何 dynamic_all Key 时 down 被拒绝，包括撤销的 Key。部署以停止接收请求、迁移、重启连接池为边界，不支持旧连接池跨 DDL 热读取的假设。

默认回滚不 down3036/3037。仅在最新拒绝状态的数据库或包含该状态的备份上，允许经本轮证明的兼容镜像临时接管；它关闭个人 Go MCP 产品能力，仍须拒绝已撤权的 REST、HMAC 文件链接及历史诊断。恢复撤权前任意旧备份可能丢失新拒绝事实，不能自动认定安全，必须 HOLD 并对账。关闭 MCP 开关不是文件/REST 撤权保护的替代品。

## 验收证据组织

所有路径相对于 `docs/integration/knowhub-mcp-portal/`。开发原命令、退出码和耗时在 `evidence/v1-wiki-dynamic/development-b702755c03332856/results.json`；完整 race 在 `extra-b702755c03332856/done.json`。冻结 Manifest 绑定上述源码，而非只引用 RC3 HEAD。

集中验收以 `*-c2f260317fc151fb/` 为证据前缀：build、deploy、acceptance、browser、codex、feishu-app、wiki-negatives、portal-dynamic、rollback-compatibility。各项以实际 result/proof/done JSON 和原日志为准；未完成项保持 NOT_TESTED，不从 e53 或 b470 自动继承 PASS。

同一候选的精确命令、运行环境和退出码总表见 `V1_WIKI_DYNAMIC_TEST_RESULTS.md`。最终集中命令26项全部退出0；独立 HTTP业务、浏览器、Codex、Wiki负向、动态Portal、飞书和兼容回滚结果另有原始JSON。不是将26项命令数当成全部功能覆盖数。

## 实例、镜像及安全回滚目标

测试地址为打包服务器本机 `http://127.0.0.1:55431`，MCP `http://127.0.0.1:55431/api/v1/mcp/personal`。仅 loopback 暴露，员工网络尚未开放；本轮浏览器与 Codex 经临时 SSH 转发访问。普通部署仍复用 APP:8080 和前端 Nginx `/api/`，没有额外 Go MCP 进程。

Compose 项目 `knowhub-v1-wiki-dynamic-c2f260317fc1`；目录 `/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb`。6个常驻容器为 app、frontend、postgres、redis、model、feishu；后两者为合成HTTP服务。PG、Redis、卷及10.253.46.0/28、10.253.47.0/28网络独立，原冻结实例不覆盖。

| 产物 | 镜像或指纹 |
|---|---|
| APP | `knowhub-v1-wiki-dynamic/app:c2f260317fc151fb` |
| APP Image ID | `sha256:7f7220b7eaa13b8c5d54fe3ec647edc4dbca552f3641bec061392d225a0a13f5` |
| 前端 | `knowhub-v1-wiki-dynamic/frontend:c2f260317fc151fb` |
| 前端 Image ID | `sha256:8c05063a13a85c1a49e0f87eee7fd8bf191365830f38c57d1873aadb112ad6bd` |
| APP 二进制 SHA256 | `ab3c0fadfa008b27a6b19596b6f59270aeffbf0f1c89e2d6b7c36f4aa2ef8124` |
| 安全兼容回滚镜像 | `knowhub-safe-rollback/app:662485ffc851ac83` |
| 安全兼容回滚 Image ID | `sha256:fbac4d811054356cd0aa1d8a99fdc7d58466d590ab156efce007492a868a3c0c` |
| 本候选回滚证明 SHA256 | `08c03ead73dc8f65ae316aa3195eb1083ff704bad24fe1db3b4b2f33f58d4e6d` |

本地构建镜像未推送，RepoDigests为空，不虚构 registry digest。APP运行层继承旧基础镜像的部分 OCI 标签，旧 revision不是当前代码证据；当前依据是 `knowhub.source.sha256`、冻结Manifest、构建记录和已从镜像实际核对的二进制SHA。二进制与本候选构建产物一致。

兼容回滚在新合成克隆库执行，未启动已知不安全旧 APP。九项门禁为前进升级、恢复后拒绝状态保留、兼容冷启动、返回新候选、旧HMAC拒绝、REST拒绝、历史引用拒绝、功能关闭仍拒绝、Schema保留；全部PASS。相同未过期HMAC返回403，REST返回403 `source_access_restricted`。Key/Scope/来源拒绝摘要前后相同。许可JSON绑定本候选、精确镜像和证明文件SHA，见 `rollback-approval-c2f260317fc151fb.json`；不构成生产操作授权。

停止新候选只需在其独立目录使用其Compose项目执行 `stop`；不默认删除数据库卷。精确数组见 `deploy-c2f260317fc151fb/instance.json`。探针APP和Redis已自行停止，卷与日志保留；原RC3/Python/5468/b470/e53未被替换。

## 原始业务目标及正式测试顺序

“我的全部知识”对**新建显式 dynamic_all Key**已实现：Portal公共范围加当前用户最新有效私人授权；未来空间加入及撤权由同一Key动态发现。历史fixed Key继续固定范围，不静默扩权，这是兼容约束而非待自动回填项。生产旧凭据没有变更。

飞书安全自动增量可在本轮合成环境恢复并保持稳定KID、原子索引切换、失败保旧和可信撤权拒绝。真实云端事件可信度、7个旧来源积压尚需独立授权与正式测试，**不能据此解除生产停更**。独立RC3飞书补丁继续保留，不强制绑定MCP上线；但其真实云端/数据演练和上线批准仍必需。

建议下一步按以下顺序，不增加功能批次：

1. 审查本候选、3037迁移、历史Wiki HOLD和兼容回滚方案，确定正式测试的账号、模型和飞书测试应用授权；不执行生产操作。
2. 在获准正式测试环境验证SID账号/禁用、真实模型1024维和现有798/3584索引兼容，核对多租户模型绑定及权限失败拒绝。
3. 使用专门飞书测试文档验证真实429/403/删除/移动/撤权语义、增量Cursor及积压重放，验证生产形状数据副本的迁移和拒绝状态恢复；不得直接用生产游标试验。
4. 只读核对生产Portal默认规则下的实际范围、显式受限KB和模型可检索内容，确认敏感混合KB例外；合成17/48设计数不能替代实际范围验收。
5. 完成实际网关/HTTPS、备份、密钥加密材料、监控、停用与**硬化兼容镜像**回滚评审后，再由人工决定灰度和飞书恢复策略。

结论：具备进入正式测试环境的候选条件；不具备直接宣布生产验收完成或解除停更的证据。本轮开发、隔离部署和集中验收完成后停止，不 commit/push/merge/release/deploy 生产，不继续 WorkMate 专项。

## 不属于本轮合成 PASS 的事项

真实 SID 登录及正式人员账号、正式1024维模型服务、历史798/3584生产索引、真实飞书应用云端授权/权限变更、7个旧来源生产积压、生产17空间48KB实际可见性、CDN/对象缓存治理、完整 M3/Wiki历史重生成质量及持续高并发负载均需相应正式环境证据。WorkMate历史会话 tools/call FAIL保留并暂缓，不作为本轮专项。

生产 `FEISHU_LEGACY_SYNC_SUSPENDED=true` 依用户最后确认继续保持，本轮不连接生产验证或修改。测试脚本使用合成身份、来源、模型及数据；官方 SDK、真实 APP/Worker/PG 和客户端成功不等于飞书云端或生产模型验收成功。
