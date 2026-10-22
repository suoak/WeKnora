# 正式测试部署前检查、影响分析与验收状态

检查日期：2026-10-22（北京时间）。候选 `c2f260317fc151fbab98d9e87b2fe5c9acbd903dd4be2e857e0326565d390d75`，Schema3037。

**当前结论：部署前只读检查已完成；正式测试部署和真实业务验收尚未执行，状态 BLOCKED，等待明确部署目标及共享变更批准。没有新增功能或修改功能源码。** 原隔离合成 PASS 保留，不替代正式环境 PASS。

最关键的发现不是缺少开发功能，而是环境边界：共享80/8080入口实际指向RC3的 `knowhub-mvp-p3`；其SID回调配置为 `http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback`，该域名在打包服务器解析到**生产10.51.134.114**。不能直接从现有配置发起正式测试登录，避免回调到生产并触发身份/JIT写入。需先落实独立获准测试域名/回调及测试账号、应用和模型凭据归属。

生产只读核查已确认17个已发布有效空间、47个有效document KB；历史48中有1个KB软删除，原因已由元数据证实。生产停更仍为true，未迁移、未启用MCP、未调用飞书或模型云端、未读取正文。

## 1. 范围和证据

本轮只新增报告、元数据检查脚本及 `evidence/formal-acceptance/`，不修改APP、前端、迁移或既有候选包。凭据仅使用既有固定主机公钥/DPAPI连接；不记录密钥，不导出数据库或正文。每条SQL均在 `BEGIN READ ONLY` 中，15或20秒超时后ROLLBACK。HTTP只探测未鉴权health/config/MCP GET，不访问callback/start，不发起SID登录、业务工具调用或模型请求。

检查了三套明确不同的环境，不能混用版本结论：

| 环境 | 实际状态 | Schema | 本轮处理 |
|---|---|---|---|
| 打包服务器共享入口80/8080 | entry → p3frontend/p3app；`knowhub-mvp-p3-app-1`运行RC3 | 3033 clean | 只读，不覆盖、不重启、不迁移 |
| 旧 `knowhub-company-test` 备用栈 | APP停止，前端/PG/Redis/SID网络仍存在，18091为旧前端 | 3030 clean | 只读；不是当前80入口实际APP，未擅自启动 |
| 冻结c2隔离实例 | `knowhub-v1-wiki-dynamic-c2f260317fc1`，APP/前端运行 | 3037（上一阶段验收） | 镜像/包/实际二进制复核一致；保留合成范围 |
| 生产10.51.134.114 | 实际APP为RC3加username修补标签，旧Python仍运行 | 3033 clean | 仅元数据只读；停更true |

原始证据：`test-preflight-20261022T064736Z.json`为备用旧栈，`test-active-preflight-20261022T065432Z.json`为**实际P3目标**。不能将前者3030、模型127.0.0.1:9、APP停止等结论误用到实际P3；后者3033且有真实模型配置。入口拓扑补查见 `active-entry-target-metadata.json`、`test-preflight-supplement-20261022T065252Z.json`。

## 2. 当前真实版本和候选身份

| 角色 | 指纹 |
|---|---|
| 冻结候选完整源码清单 | `c2f260317fc151fbab98d9e87b2fe5c9acbd903dd4be2e857e0326565d390d75` |
| 冻结业务源码（含测试） | `b702755c033328560bbb28802c93a3a1abce31a43352870b43d8f1fee2d74cdb` |
| 源码tar两端SHA256 | `cb08c31f06d1565196ae0bc93458d10069a73e803b08fa2ffe984a8fe3e67437` |
| c2 APP Image ID | `sha256:7f7220b7eaa13b8c5d54fe3ec647edc4dbca552f3641bec061392d225a0a13f5` |
| c2 前端 Image ID | `sha256:8c05063a13a85c1a49e0f87eee7fd8bf191365830f38c57d1873aadb112ad6bd` |
| c2 实际APP二进制SHA256 | `ab3c0fadfa008b27a6b19596b6f59270aeffbf0f1c89e2d6b7c36f4aa2ef8124` |
| 当前共享P3 APP | `sha256:0c84cac5af5103b73102c6261f32130d3e51eeed1b8cbf77fda61de20c4bd983` |
| 当前共享P3 前端 | `sha256:6a2def24a8dcf69cecac633cfb4e0c2752e472fe500a34ce464edb6db13ffa22` |
| 当前生产APP | `sha256:f4f0511ac8373038fc743b857117dba28ce1cf2887e036f888a39df67f200c4e`；标签 `e62b17e2d305358467821aeff1cce63ce1d61254+username-97806d7d3200` |

候选本地包、远端包、实际运行镜像和二进制均与最终交付一致，见 `candidate-identity-verification.json`。这些SHA不是Git提交；当前工作树HEAD仍是e62 RC3，未提交源码须依据Manifest。**本阶段没有把c2部署为正式测试入口**，所以表中不虚构“已正式部署”的指纹。

共享P3现在运行的旧RC3 APP正是历史撤权HMAC保护不足的版本。此次只观察既有运行状态，没有启动或回退到该镜像。新候选一旦进入正式测试，不能将它重新作为回滚目标。

## 3. APP、代理、数据库、模型、SID和密钥检查

| 项目 | 只读事实 | 判定与部署影响 |
|---|---|---|
| APP/Worker | P3 APP运行，连接本机 `WeKnora-postgres` 的 `WeKnora` 库；Worker内置APP，不是单独Go MCP服务 | 同时替换APP就同时影响HTTP和Worker；迁移前必须安排停写/停任务窗口，不可边运行旧Worker边迁移 |
| 数据隔离 | P3 PG/Redis/Docreader/Python来自 `/home/aqyfzx/ai/WeKnora` 共享栈；不是c2独立卷 | 不能覆盖共享依赖；不是生产主机不等于已确认数据/凭据全为测试用途，来源归属仍需确认 |
| 版本链 | P3为3033 clean；备用旧栈3030，不能假装它是3033副本 | 正式3033→3037必须在获准测试副本/独立库演练；共享库迁移尚未批准和执行 |
| 前端/Nginx | 80/8080入口转发 `p3frontend:80`、`p3app:8080`；HTTP1.1、proxy_buffering off、超时3600s。P3前端原有/api转发 | 尚未证明新MCP在正式网关下的POST/鉴权/stream行为；GET401只证明当前边界拒绝，不证明八工具可用 |
| HTTPS/URL | 主机未观察到443监听；当前SID回调是HTTP生产域名 | 外部TLS终止与正式测试MCP域名UNKNOWN；不能把c2 loopback地址当正式网络入口 |
| SID/OA | P3启用SID、JIT和现有身份契约；client_id/secret有配置，账号总数4，未导出身份；回调域名解析生产 | 配置存在不等于测试账号获批或登录PASS。当前不启动登录，先确认测试回调不写生产及凭据可用于测试 |
| 模型 | P3 Embedding/QA/VLLM/Rerank配置指向 `https://uniapi.ruijie.com.cn` 且有凭据字段 | 有正式模型配置条件，不认定模型已不可用；但测试用途授权和实际1024维返回未验证，未使用这些凭据调用 |
| 索引 | P3现存5682条向量，dimension1024；有798/3584/1024 HNSW定义 | 定义存在≠各维数据均存在，兼容实际调用仍NOT_TESTED；不得跨租户/模型无授权回退 |
| Portal范围 | P3当前无Published有效KB | 真实业务验收须在批准测试库创建测试Portal/私人范围；不能公开生产空间来凑测试 |
| 飞书测试资源 | P3只看到1条已软删除的旧来源；未找到可据此直接使用的获批Wiki/Drive活跃测试源 | 需明确测试应用和文档/目录权限，不能借用生产来源、密钥或游标 |
| SYSTEM_AES_KEY | P3环境变量存在，长度32；未导出，未验证备份保管/恢复 | 升级保留**目标测试环境自己的**密钥；不能生成新值覆盖旧密文，也不复制生产密钥。需验证备份及恢复解密 |
| 备份/恢复 | 本次未创建/导出共享库备份；最新备份完整性和AES材料保管UNKNOWN | 迁移前需获准目标的备份、文件/配置保全和恢复校验。上一轮合成恢复不能替代该项 |
| 回滚 | c2有已验证3037硬化兼容镜像及9项合成证明 | 必须在正式目标独立克隆复验；不down3036/3037，不退旧不安全RC3；保留最新拒绝状态 |
| 磁盘 | 打包服务器根分区约14GiB可用、98%使用率（本次观察） | 并行库/备份预算要先估算，不自动清理或复制生产1.39M向量来占满磁盘 |

注：首份基础元数据脚本只查了通用OIDC_CLIENT_SECRET等环境变量名，其false不能解释为OIDC未配置。补查依据源码读取正确的 `OIDC_AUTH_CLIENT_ID/SECRET` **是否存在**，确认P3存在，见 `test-active-preflight-supplement-20261022T065433Z.json`。

## 4. 生产17空间、47/48KB和索引

实际生产只读观测时间约2026-10-22 14:47（北京时间），Schema3033 clean；17个Published/active/未删除空间，47个未删除document KB，47个均非临时KB。

与2026-10-21第七批48KB脱敏集合按相同 `md5('knowhub-b7-kb:'||id)` 引用比较：**没有新增差集，仅1个原KB被软删除**。

- 空间引用 `577ed42ff1827535`，KB引用 `c2edb6e0b65ad3d9`。
- 该空间仍active、未删除、Portal仍Published；KB仍为document，但deleted_at非空。
- 删除时间2026-10-21 **16:17:55北京时间**（DB `2026-10-21T08:17:55.031141+00:00`）。
- 历史记录3篇 `feishu_personal` 文档；当前元数据为3篇软删除、0篇有效文档。未读取标题或正文。
- 删除操作者及业务动机UNKNOWN，不能由元数据臆测；没有恢复或重建资源。

47是按最终Published规则计算的**理论有效公共KB范围**，不是生产Go MCP已启用、也不是已经通过来源、索引、正文安全过滤的实际检索量。本轮不再使用旧逐KB审批方案的“0个approved”阻断默认公开规则。

生产向量数据实际为1024维 **1,393,111行**；47个公共KB的模型绑定维度均配置1024。HNSW定义还保留798、3584、1024三种。没有据此声称798/3584生产数据正在使用或正式模型返回已验证，未调用生产模型。

## 5. 七个active旧来源与额外风险

当前未软删除来源共12条：active 7（Wiki2、Drive5），paused2、error3。与历史“2Wiki+5Drive active”数量相符；历史保存的是聚合，没有逐源ID清单，因此不能虚构7个ID逐一历史匹配证明。报告附当前7条稳定脱敏引用，供后续受控对账。

来源status=active只表示数据库调度状态，**不表示已恢复同步**：APP当前 `FEISHU_LEGACY_SYNC_SUSPENDED=true`，safe_incremental开关UNSET。Schema3033没有3036的持久来源拒绝状态字段，因此真实云端是否已撤权为UNKNOWN，不能将SCHEMA_UNAVAILABLE解释为active或未撤权。

当前7条来源均有Cursor元数据，保留其摘要用于以后只读比对，未导出内容或推进Cursor。距离last_sync的时间是观测时“距上次成功同步”的估算，不是准确积压数量，也不等于停更时长。没有访问飞书云端，新增/修改/删除/权限事件数量UNKNOWN。两个active来源已有error_message非空，未导出原始错误正文；不能从active掩盖其错误。

具体来源表和集合差异JSON由本报告附表生成。paused/error旧源也须继续保持保护，不能只关注7个active来源。任何云端403、404或未知移动仍不能被本轮只读元数据认定为可信删除或撤权。

## 6. 正式业务验收矩阵（本阶段）

| 项目 | 状态 | 原因/需要的证据 |
|---|---|---|
| 冻结源码、Manifest、镜像和实际二进制复核 | PASS | 本次两端源码tar、APP/前端Image ID及运行二进制均一致 |
| 当前环境拓扑及版本检查 | PASS | 已辨明共享P3、旧3030备用栈和c2隔离实例，不混用 |
| 正式测试候选部署 | BLOCKED | 目标与共享变更批准待定；未替换共享服务或新建正式测试实例 |
| 3033→3037正式目标迁移/恢复 | BLOCKED | 需获批副本、备份和窗口；上一阶段隔离结果不继承为正式目标PASS |
| SID/OA真实员工及账号状态 | BLOCKED | 当前回调解析生产；测试账号与SID应用/回调范围待确认 |
| Key创建/查看/轮换/撤销、八工具与Codex | BLOCKED | 正式入口未部署；不复用合成PASS当真实员工验收 |
| Portal动态范围、dynamic_all新增/撤权 | BLOCKED | 需获批测试空间/账号和独立数据写入范围；当前P3无PublishedKB |
| 正式Embedding/注册表/实际索引维度兼容 | BLOCKED | 正式配置存在，测试用途凭据和目标待确认；尚未发送模型请求 |
| 飞书Wiki/Drive真实更新/未变化跳过 | BLOCKED | 没有明确获批测试应用及文档范围；生产来源禁止使用 |
| 真实429/临时403/删除/移动/撤权/积压 | NOT_TESTED | 不制造生产故障，不改生产游标；须获批云端测试数据与受控故障方式 |
| 稳定KID/失败保旧/Cursor收据/Worker并发 | NOT_TESTED（正式目标） | 历史合成结果保留，新目标尚未启动验收 |
| Wiki混合来源、撤权页面、HOLD隔离 | NOT_TESTED（正式目标） | 需批准来源夹具与真实业务链；不扩大Wiki工具范围 |
| 原Python/OA/飞书个人导入兼容 | NOT_TESTED（正式目标） | 旧服务保留，本次只读配置/状态不等于完整业务回归 |
| 正式HTTPS/POST/stream代理 | BLOCKED | 独立测试域名/终止点/回调未确认 |
| 生产17/47集合及48差异 | PASS（元数据只读） | 已确认软删除，不宣称正文安全或生产已公开 |
| 生产7源真实积压与撤权事件 | BLOCKED/UNKNOWN | 已得状态、上次同步和Cursor摘要；云端事件与权限未核实 |

本阶段没有观察到可据以判定c2功能失败的新FAIL；当前阻塞为部署授权和真实环境门禁。历史WorkMate tools/call FAIL、e53等测试FAIL继续留存，不重新排查、不改写。

## 7. 可审阅的部署方案与影响

**推荐方案A：新建并行正式测试项目 `knowhub-v1-formal-c2f260317fc1`，不改共享P3和冻结c2。** 精确复用已核验c2镜像，新的PG/Redis/卷/网络，只使用获批测试账号/测试文档、测试专用密钥。建立独立测试域名/HTTPS及SID注册回调；如需变更共享网关、防火墙或回调注册，先批准具体配置。不得自行将生产域名指向新实例或使用生产回调。迁移演练使用新3033测试库或经批准的**测试数据副本**，不从生产复制数据/密钥。

方案A影响：新增实例和资源占用；需要测试域名/证书/回调和独立数据、模型、飞书权限，不影响原服务。当前资源预算、回调与凭据授权未齐；暂不执行部署。

备选方案B：升级共享 `knowhub-mvp-p3`。会影响现有80/8080用户、4个账号的业务数据、共享3033库、内置Worker和旧Python后端REST兼容。迁移无法只靠回退前端恢复；更不能退到当前旧RC3 APP。需要**明确批准停写窗口、共享库备份/恢复、保留AES材料、停Worker、3033→3037迁移、APP/前端替换和网关切换**，并复核其他共享依赖的兼容，不默认允许变更旧Python容器。

无论A/B，顺序：确认目标/获批测试主体及凭据归属 → 备份与克隆恢复验证 → 默认关闭部署同源APP/前端、迁移3037、启动健康/旧服务检查 → 仅测试实例受控启用 → 真实SID/模型/飞书/Codex业务集中验收 → 硬化兼容回滚与拒绝状态恢复 → 单独申请生产灰度/同步恢复。

**需要用户确认的具体信息：**A/B目标、允许的共享变更范围；专用测试域名/HTTPS和SID回调；获批测试员工及测试飞书文档/应用清单；模型和SID/飞书测试凭据的安全存放位置及可用于测试的确认。不要在聊天发送密钥。现有配置“有secret”不等于允许把它当测试凭据使用。

## 8. 本次停止点及生产结论

1. 实际新部署：**无**；c2冻结实例和历史实例均保留。正式入口仍P3 RC3，不能称已完成c2正式部署。
2. 真实SID/模型/飞书云端结果：**尚未执行**；配置条件、数据和路由已核对，未以合成PASS代替。
3. 47/48原因：**1个KB及其3篇文档软删除**，空间仍Published，已给稳定引用和时间。
4. 七个active来源：2Wiki+5Drive状态与Cursor已只读记录；真实积压事件数量及云端撤权UNKNOWN，额外paused/error来源不能忽略。
5. **现在不建议申请解除生产飞书停更**：可以提交测试资源/窗口申请，但恢复生产同步的真实云端和积压门禁未完成。
6. **Go MCP生产灰度条件未齐**：正式目标部署、SID/模型/飞书及网络、备份和安全回滚目标验证待完成。

按用户“涉及覆盖共享服务、迁移数据库或修改共享网络配置，先报告并等待批准”的明确要求，暂停依赖上述确认的部署和业务测试。继续保持生产 `FEISHU_LEGACY_SYNC_SUSPENDED=true`，不新增MCP功能、不改生产、不解除停更、不提交/推送/合并/正式发布。本报告是本阶段当前交付与授权等待点，不是整个正式验收已经完成的声明。


## 9. Current active-source metadata (snapshot, no cloud calls)

| Source ref | Type | DB status | Last successful sync (DB time) | Hours since sync | Error field present | Cloud revocation / backlog count |
|---|---|---|---|---|---|---|
| 29dfe8cf9c4c5d7b | feishu | active | 2026-10-19T18:00:19.127664 | 60.8 | False | UNKNOWN / UNKNOWN |
| 89926d424dc76872 | feishu | active | 2026-10-20T10:05:08.457077 | 44.7 | True | UNKNOWN / UNKNOWN |
| 1f9afaa178420115 | feishu_drive | active | 2026-10-20T10:00:06.984412 | 44.8 | False | UNKNOWN / UNKNOWN |
| 510373b040da38e8 | feishu_drive | active | 2026-10-20T10:00:07.930811 | 44.8 | False | UNKNOWN / UNKNOWN |
| ba83a2db926f6572 | feishu_drive | active | 2026-10-20T10:00:05.94916 | 44.8 | False | UNKNOWN / UNKNOWN |
| bd1561572ce89f81 | feishu_drive | active | 2026-10-10T06:17:13.433587 | 288.5 | False | UNKNOWN / UNKNOWN |
| ebad6a622c04c7a3 | feishu_drive | active | 2026-10-12T06:04:09.838192 | 240.7 | True | UNKNOWN / UNKNOWN |

Snapshot UTC: 2026-10-22T06:47:38.999232+00:00. Cursor fingerprints remain in production-range-and-source-diff.json; no cursor contents exported or advanced.

## 10. Evidence and commands

- `evidence/formal-acceptance/active-entry-target-metadata.json`
- `evidence/formal-acceptance/candidate-identity-verification.json`
- `evidence/formal-acceptance/production-preflight-20261022T064736Z.json`
- `evidence/formal-acceptance/production-preflight-supplement-20261022T065151Z.json`
- `evidence/formal-acceptance/production-range-and-source-diff.json`
- `evidence/formal-acceptance/test-active-preflight-20261022T065432Z.json`
- `evidence/formal-acceptance/test-active-preflight-supplement-20261022T065433Z.json`
- `evidence/formal-acceptance/test-host-dns-readonly.json`
- `evidence/formal-acceptance/test-preflight-20261022T064736Z.json`
- `evidence/formal-acceptance/test-preflight-supplement-20261022T065151Z.json`
- `evidence/formal-acceptance/test-preflight-supplement-20261022T065252Z.json`

```powershell
python docs/integration/knowhub-mcp-portal/tools/formal_preflight.py test
python docs/integration/knowhub-mcp-portal/tools/formal_preflight.py test supplement
python docs/integration/knowhub-mcp-portal/tools/formal_preflight.py test-active
python docs/integration/knowhub-mcp-portal/tools/formal_preflight.py test-active supplement
python docs/integration/knowhub-mcp-portal/tools/formal_preflight.py production
python docs/integration/knowhub-mcp-portal/tools/formal_preflight.py production supplement
```

All SSH inspection exits: 0. The stopped?? APP binary inspection returned nonzero and is recorded UNKNOWN; it was not started. An SSH inspection exit 0 proves collection only, not formal business acceptance. No migration or deployment command executed.
