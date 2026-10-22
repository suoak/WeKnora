# 阶段 A：V1 原需求核对与飞书安全增量风险分析

审计日期：2026-10-21，北京时间。**V1 冻结成果受保护；“我的全部知识”仍有新增私人空间不能自动纳入的产品缺口。旧 Wiki/Drive 还不能安全恢复自动增量。**本报告不批准生产发布或解除停更。

## 管理摘要

1. V1 仍为 RC3 HEAD `e62b17e2d305358467821aeff1cce63ce1d61254` 上的未提交业务源码 `5468da42c518cc83539040506c85b7964bdf2ae20826e641dbd030253316e231`。本轮只读比较冻结清单中的非文档文件，差异及额外文件另见 preservation.json；不把 HEAD 当作 V1 内容版本。
2. V1 的动态 Portal、非成员公共访问、当前 Key 私人范围合并、五只读工具、简化页面、加密 Secret 本人重看、实际 Codex 五工具已有源码及对应合成环境证据。未重新开发这些能力。
3. 当前生产 APP 明确配置 `FEISHU_LEGACY_SYNC_SUSPENDED=true`，Schema3033 clean。DB 里仍有 active 的2个 Wiki、5个 Drive，但 **active 不代表调度已恢复**；部署屏障优先于来源状态。另有 paused/error 旧来源，不能只保护7个 active 来源。
4. 飞书更新不是只缺重试：现有变更重放返回 ErrFeishuStagingRequired，文件替换会先清旧 Chunk/索引，个人导入的 first-create 机制也没有提供旧文档版本切换。缺少贯穿共享索引和旧 Worker 的安全提交闭环。
5. 已独立复现两项 Cursor 故障并开展小范围修复：Checkpoint失败仍继续执行；删除失败被确认。它们可独立基于 RC3 构建且不需3034/3035，但不能冒充完整增量更新修复。
6. 停更截止以本轮用户明确的 **2026-10-27 19:25（北京时间）** 为依据。旧 RC3 文档部分仍记“期限待定”，不覆盖新指令。本轮不更改生产策略，不承诺在未通过原子切换/故障验证前到期自动恢复。

本次生产聚合在2026-10-21约20:11（北京时间）观察到 **17个有效已发布空间、47个有效document KB**，不是历史48。查询同时要求空间active/未删除、KB未删除；差异原因UNKNOWN，不据此推断某KB丢失或擅自修复。该数量是理论Published范围，未减去显式受限和运行时失败，也不是生产MCP实际可检索量。

## 基线、隔离与原始证据

| 对象 | 位置 / 版本 | 本轮操作 |
|---|---|---|
| V1 原工作树 | `E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021`；分支 `audit/knowhub-mcp-b4-rc3-20261021` | 只读；保留1～7批及全部未提交源码 |
| V1 冻结包 | `E:/AIData/Codex/knowhub-v1-handoff-5468da42c518cc83-v4.tar.gz` | 整包SHA重新核对：`ece1af089f6ce684f2fda95f253da10aa2cbcf6097ce818b03f3cca89fa592c0` |
| 飞书独立修复 | `E:/code/WeKnora-worktrees/feishu-safe-incremental-20261021`；`fix/feishu-safe-incremental-20261021` | 直接从确认的RC3 e62建立；不复制MCP大改动 |
| V1 测试实例 | `knowhub-v1-5468da42c518`；服务器loopback60683 | 只读核对5容器运行、APP/前端Image ID与冻结交付一致；不覆盖、不重启 |
| 新测试候选 | `.../WeKnora-release-build/feishu-safe-incremental-20261021/{before,after}-<sha>` | 仅开发检查容器，无APP正式部署、无生产连接；只读源码挂载 |

本轮证据：[preservation.json](evidence/feishu-incremental/preservation.json)、[production-readonly.json](evidence/feishu-incremental/production-readonly.json)、[build-host-readonly.json](evidence/feishu-incremental/build-host-readonly.json)。SSH命令exit0；DB查询使用 `BEGIN READ ONLY; SET LOCAL statement_timeout='15s'; ...; ROLLBACK`，仅聚合状态，不读取正文、标题、来源配置、游标或凭据。

重新比较5164个冻结非文档路径，已有文件无差异；另发现原V1工作树 `website-docs/sample-data/` 有5个不在冻结manifest中的样例文件。逐路径记于preservation.json，不纳入5468版本、不复制到飞书候选、不删除。它们不在APP/前端运行输入内，不能将“运行源码一致”描述为“当前整个目录逐字节等于冻结包”。

V1三报告为原工作树 `V1_STAGE1_DEVELOPMENT_DELIVERY.md`、`V1_STAGE2_ISOLATED_ACCEPTANCE.md`、`V1_MCP_ACCESS_UI_DELIVERY.md`。阶段一中仍有“集中测试尚未结束”的早期段落；以阶段二的最终日志、候选指纹及最终验证汇总交叉核对，不能仅凭报告一句PASS认定。旧FAIL与最终同备份template0恢复PASS均保留。

## 原始需求符合性矩阵

下列源码路径相对 **V1 工作树**，而不是缺少MCP改动的RC3修复树。VERIFIED限定原5468合成实例；真实员工/正式模型/生产可见范围不继承此结论。

| 需求 | 源码实现与现有证据 | 判定 / 剩余问题 |
|---|---|---|
| 1 Published空间全部有效document KB默认公共 | `repository/portal_mcp.go::ListPortalMCPCandidates` 动态枚举有效空间和非临时document KB；`access/portal_mcp.go::Resolve`；`portal_published.go::portalPublishedDocuments` 以Published为依据 | IMPLEMENTED / VERIFIED（合成）；真实生产默认关闭，不宣称48KB已开放。明确KB阻断仍生效，未标记的混合敏感资料不能声称受保护 |
| 2 新Portal空间、新KB、新索引文档自动纳入 | 每次重新枚举/读取快照，无48ID白名单；`portalEffectiveDocuments` 只收completed/enabled有效正文；PG候选绑定Tenant/KB/Document | IMPLEMENTED / VERIFIED（动态合成）；真实飞书增量因停更及writer缺口未验收，不能从“索引后发现”推导“同步已成功” |
| 3 员工不用加入目标Portal空间 | `Resolve` 公共分支不授予成员关系；企业能力和真实个人主体仍检查 | IMPLEMENTED / VERIFIED（非成员合成）；未批量修改成员 |
| 4 公共∪个人私人知识 | 公共分支失败可走独立私人授权；`privateContext` / `authorizePrivateSnapshot` 与真实成员及当前Key Scope交集；撤销Published不消灭独立私人授权 | IMPLEMENTED / VERIFIED（两个私人空间）；并非所有未来新私人空间自动纳入 |
| 5 配置一次、无需逐空间MCP | 单一路由 `/api/v1/mcp/personal`、一个Bearer个人Key；UI固定统一地址 | IMPLEMENTED / VERIFIED（实际Codex）；新增私人范围可以沿用同一Key但仍需显式更新Scope，存在第9项差距 |
| 6 有效期、加密保存、本人查看、轮换、撤销 | `handler/user_mcp_api_key.go::{userMCPExpiry,RevealUserMCPAPIKey,RotateUserMCPAPIKey,RevokeUserMCPAPIKey}`；service/repository tenant_api_key AES-GCM与CAS；OwnerUserID/JWT/account/key检查 | IMPLEMENTED / VERIFIED（真实APP合成账号、浏览器刷新重看）；新建默认nil期限，旧Key日期不变；历史hash-only需主动轮换；生产需受保护SYSTEM_AES_KEY，不以缺失配置落明文 |
| 7 同Key/范围、服务端只读 | `mcpserver/personal.go::NewPersonalServer/guard` 只注册并允许5读取工具；Stateless Streamable HTTP；Nginx到APP8080，无独立服务 | IMPLEMENTED / VERIFIED（同入口）；真实隔离网通道NOT_TESTED且本轮不开放。未来增加写工具必须另做网关/路径只读保障 |
| 8 新页面与Codex真实五工具 | `/platform/mcp-access`原侧栏；名称/期限创建、GenericJSON、Codex env引用TOML、历史接入管理；原 `evidence/v1/codex-5468da42c518cc83/result.json` exit0 | IMPLEMENTED / VERIFIED；实际5工具分别completed，另有未授权read失败，非仅模板/发现测试。真实SID尚未验收；WorkMate会话FAIL保留暂缓 |
| 9 后续加入私人空间自动可见 | Key保存tenant_scopes；成员增加不改变已签发Scope；页面有“更新私人空间范围”显式管理 | **PARTIAL / 确认产品缺口**。权限边界是有意保留的，不能把“不漏当前Scope”写成“全部未来私人知识”。本轮不自动扩旧Key |

### 第9项的最小后续选择（分析，未实现）

如“配置一次”还必须涵盖 **今后加入的私人空间**，需要一个明确保存的授权意图，例如 `private_scope_mode=fixed | all_current_authorized`。所有历史缺省记录必须解释为fixed；原selected KB/tenant范围不改变。动态模式仅由本人在创建或明确升级时授予，并每次按真实有效成员、KB读取权、个人主体、Key状态及能力取交集；它不是FullAccess，也不授予新空间成员身份。机器/团队/租户/平台凭据不得使用。

失去成员身份、KB授权撤销、账号停用、Key撤销仍立即收紧；未来加入空间可纳入是这个 **新模式的明确授权语义**，不能借用旧scope空集表示。公共Published解析保持原状。需要新增模式字段兼容、服务端范围计算、前端清楚区分、旧Key不变及并发撤权测试，再语义合并候选。按照本轮“不扩大Go MCP”要求，仅列缺口和后续方案，不在飞书热修复中实施。

当前最准确的产品描述是：**全部动态Portal公共知识 + 当前Key授权范围内本人私人知识**。“永远自动覆盖本人全部未来私人空间”尚不成立。

## 飞书调用链与根因

| 环节 | 当前实际行为 | 风险与修复边界 |
|---|---|---|
| 接纳屏障 | `datasource/legacy_feishu_suspend.go`；Create/Update/Resume/ManualSync、scheduler及`processSyncAttempt`拒绝legacy类型；非法非空flag拒绝默认 | 必须保留；旧二进制不认识新flag，部署前必须排空全部旧APP/Worker，不靠租约过期推断副作用停止 |
| 来源执行所有权 | `datasource_execution.go::ProcessSync/checkSyncExecution/writeSyncExecution`；repository `ClaimExecution/HeartbeatExecution/WriteExecution` | DB-issued generation保护来源/SyncLog和Cursor提交；不是共享向量/BM25/Chunk全链路fence |
| 同外部文档识别 | `ingestItem` 使用tenant+KB+datasource+external_id查询；`feishu_datasource_replay.go::reconcileFeishuReplay` | 同内容可跳过/恢复dispatch；内容或文件名变化返回ErrFeishuStagingRequired，故生产未恢复变更更新 |
| 旧文件替换 | `knowledge_replace.go::ReplaceKnowledgeFile` | 先改source/pending/disabled，再reparse清旧Chunk/索引；旧正文可能先失效。Feishu guard刻意拒绝该入口的变更；不能直接删guard |
| 解析与索引 | `knowledge_process.go::processChunks/reparseKnowledge`；先写Chunk再BatchIndex；部分失败按KID删Chunk/索引 | 索引存在部分写入/未知提交；没有通用active-source-generation约束，迟到失败清理可能删当前版 |
| 共享PG | `retriever/postgres/repository.go::BatchIndex/DeleteByKnowledgeIDList` | 向量/BM25共享embeddings；按KID清理不带候选版本，普通DB source事务不能隔离网络BatchIndex及异步Worker |
| 既有个人导入 | `feishu_personal_recovery.go` private manifest/attempt/lease；`feishu_personal_first_create.go` | 可复用manifest、lease、outbox思想；`FirstCreateOnly`及`awaiting_index_isolation`是明确边界。首次创建不等于已完成旧文档更新 |
| Cursor | shared `core/engine.go::runSync`；service `streamSyncHandler`及batch/stream分支 | fetch错误不推进节点edit time已有实现；Checkpoint错误仅log后继续、删除错误nil返回是本轮独立复现缺陷。异步enqueue成功仍不证明最终索引成功，需要单独receipt门禁 |
| 删除/撤权 | 完整列表与旧cursor集合差生成IsDeleted，`SyncDeletions`决定删除 | 列表缺失可能是移动/权限/范围变化，不能冒充可信删除事件。403归permission并不证明永久撤权；需要可信确认和持久读拒绝先行，不能仅删索引后异步收权 |
| HTTP故障 | core/errors.go分类429/5xx/auth/permission/not_found；policy.go有共享限流重试 | 已有成熟机制复用；临时403不当作删除。云端权限语义、事件签名/可信性仍需测试契约和实际授权验证 |

## 最小安全闭环（尚未宣称实现）

优先限定 **同一PostgreSQL/ParadeDB存储** 的旧Wiki/Drive正文更新。资源标识继续用既有tenant/KB/datasource/external_id和稳定Knowledge ID；复用既有pending-op/outbox与DB执行generation，不另建全局版本事实来源。

1. 导出到私有manifest，保留源revision、内容hash、来源授权证据及KB模型绑定；解析/Embedding在候选区域完成，不能写入公开Chunk或当前KID可检索的embeddings。部分、超时、未知commit保留待核对候选，旧内容保持有效。
2. 产物收据必须精确列出候选Chunk、向量维度、BM25、总数/hash；同名文件、modified_at或一个任务成功状态不能替代内容及索引校验。
3. 在一个受DB锁与generation/CAS保护的事务中，复验来源权限、KB状态/模型配置、旧版指针和执行所有权，原子发布source元数据、Chunk及PGembeddings。需要明确PG各索引与同一事务的实际可见性；其他外部驱动缺乏原子发布能力时保持HOLD。
4. 历史/迟到任务必须在每个持久副作用前携带可验证的源版本/attempt，清理只按其拥有的候选Chunk ID/版本，不再按稳定KID任意清理新版。未知旧任务拒绝；旧二进制物理排空是额外部署前置。
5. Cursor只在上述发布或可验证相同已发布版本后确认；首次新建的dispatch也必须等待真实索引receipt，失败保留原Cursor后安全重放，稳定唯一身份防重复Knowledge。多页面部分成功可保存每项已验证进度，不能把整页失败抹成成功。
6. 确认删除/撤权先持久标记不可读，覆盖REST、MCP候选及直接ID、历史引用和模型输入，再做所属版本清理；临时网络故障只重试/标待核查，不误删、不把旧授权永久视为可信。

该闭环涉及共享索引提交和实际读取路径，不是只增加一个service接口或mock状态机。是否可完全不新增迁移目前UNKNOWN；不得为赶期限随意占用3034/3035，也不能用AutoMigrate偷偷写生产。完成SQL设计后需明确RC3独立补丁与V1联合链的迁移兼容方案。

对 `feishu-enhancement-20261019`、`feishu-stability-20261018`、`feishu-mvp-20261018` 三个相关并行工作树也做了限定只读检查：均有未提交成果，已检索到的恢复实现仍保留awaiting_index_isolation边界，没有发现可直接复用并宣称已验证的通用旧文档原子更新实现。不是全盘合并或全部源码安全审计；没有覆盖其未提交文件。

## 独立RC3热修复评估与期限控制

**小范围Cursor拒绝传播可独立构建：**只改两处既有Go实现及相应测试，无MCP、前端、模型、身份、迁移或依赖变更。可用于改善故障可诊断性，但在缺少staging/共享索引/Worker/撤权闭环时，**不能以此批准恢复7个旧源自动增量**。

**完整安全增量候选当前未形成。**技术上可继续基于RC3独立实现，之后以逐文件语义审查合入新的联合候选；不需等待Go MCP正式业务验收才能做独立飞书测试，但它自己的全部安全门禁必须通过。

若在2026-10-27 19:25前仍未通过，应向业务负责人申请明确的新停更截止/复核频率，继续保持当前屏障；本轮不自行延长或解除。停更期间：新增资料只能使用既有已批准的独立导入途径且避免覆盖旧source身份；疑似撤权/敏感事件应由具备现有权限的负责人按精确KB/知识采取授权收紧并留审计，不能依赖暂停同步自动收权，也不批量删除历史资料。确需生产操作时另行批准。

人工决策的对象是延长停更期限及应急处置授权，**不是重新审批Portal公共KB、HR资格或额外MCP服务**。

## 阶段C前置和正式环境缺口

打包服务器已只读核对：现有V1五容器运行；root约41G可用，Docker数据盘约1.5T可用；端口和网络详见host证据，不修改系统配置。新候选必须另建Compose/DB/Redis/网/卷/镜像标签并由完整源码构建。当前只开展开发检查，不因两个局部修复就部署一轮正式APP验收。

阶段C应真实验证PG/ParadeDB原子可见性、稳定KID、失败保旧、撤权/删除、重复任务、旧Worker并发、Cursor恢复与MCP一致性，随后集中回归Codex五工具、Portal/两个私人空间、3033→3035及备份恢复。没有完成的项目保持NOT_TESTED。真实SID、正式模型、真实飞书授权测试应用/文档、生产数据覆盖仍需获准正式环境，合成成功不代表生产验收。

WorkMate：初始化/发现通过、会话tools/call FAIL原状保留，不继续调查、不作本轮前置。
