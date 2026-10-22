# 飞书安全增量与联合候选执行清单

本清单承接 Go MCP V1，不修改冻结版本。禁止生产写入、解除停更、commit/push/merge/release；WorkMate 会话 FAIL 暂缓。

## 最新交付状态（2026-10-22，以下优先于历史记录）

最终报告：[阶段 C 联合交付](FEISHU_INCREMENTAL_STAGE_C_FINAL_DELIVERY.md)。历史 FAIL 不覆盖，当前业务源码仍为联合 `1d533edf...`、独立补丁 `2ac380a0...`。

| 任务 | 状态 | 实际证据与剩余问题 |
|---|---|---|
| 阶段 A 原始需求与风险核对 | PASS | 私人空间未来动态授权仍是范围缺口，保留旧 Key scope；不新增审批/HR/WorkMate |
| 两套源码开发质量与冻结构建 | PASS | 各 21 命令 exit0，SKIP 单独记录；联合 b470、独立补丁 02a8 |
| 独立完整部署、默认关闭、显式启用 | PASS | APP/前端同源、独立 PG/Redis/网络/卷，生产不变 |
| Wiki/Drive 实际 Worker 生命周期 | PASS（合成） | a4 的两种 lifecycle-pass + post-a4b 续测；a4 整体 FAIL 保留，不改写 |
| 撤权、旧链接、Python 与恢复 | PASS（合成） | post-a4b exit0，未过期旧链接拒绝，恢复后 schema3036 与拒绝保留 |
| Worker/Redis 故障 | PASS（限定范围） | worker-fault exit0；发布前中断、日志年龄注入，非自然退避/租约等待证明 |
| 页面与真实 Codex | PASS（合成账号） | 当前 b470 的浏览器与实际五工具通过；SID 未验证 |
| 独立 RC3 补丁 APP/Worker | PASS（离线配置、合成） | independent-hotfix-offline exit0，无个人 Go MCP；Excel 扩展未验证 |
| 保留硬化 APP 的功能关闭回滚 | PASS | rollback done=true，不 down schema |
| 直接回退旧 APP | BLOCKED / FAIL | old-image-offline-probe 原撤权 HMAC 返回200，UNSAFE |
| 正式飞书、SID、模型、生产来源覆盖 | BLOCKED | 需要正式测试条件；不把合成 PASS 当生产验收 |
| 生产恢复与正式发布 | BLOCKED | 未获授权，停更保持；HOLD 影响与旧回退风险须评审 |

## ???????????????????????

| ?? | ?? | ???? / ???? |
|---|---|---|
| ???????V1???47/48???? | PASS | ??A???????????????????Scope??TODO |
| ??RC3?????? | PASS | runtime 2ac380a0??21/21??exit0????PG??vet exit0?SKIP?NOT_TESTED |
| ??????????? | PASS | runtime 1d533edf??21/21 exit0???????PG/race/vet??exit0 |
| ???????APP?? | PASS | ????02a8c5eb????0a0117c9????????????Go MCP?? |
| ???????APP/???? | PASS | ????b4702511??APP 90844661????988cb86a??????/??/???? |
| ??????????? | PASS | knowhub-v1-feishu-b47025118adf???59079?Schema3036???403/?Key401???Nginx??FAIL???? |
| ??APP??/???????Key | PASS | acceptance/http-business.json exit0??Key??/????????????? |
| ?????MCP??????? | IN_PROGRESS | Wiki??KID????????????Python?????Drive???/??/???????? |
| Worker/Redis?????????? | TODO | ????????????/?????? |
| ????????Codex??? | TODO | ???????UI/CLI??????d321?? |
| ??????????SID????? | BLOCKED | ??????????/??????????????? |

??????? evidence/feishu-joint/b47025118adfc4cff2d1f75cabc6f98265a0bb9ca1ad816f409099081947f0fc.json?d706????????/??????????5468?1380?d321???????????????????????

## 2026-10-22 当前执行位置

以下为当前状态；下方历史明细及其证据保留，旧候选结论不能自动继承。

| 项目 | 状态 | 当前证据与剩余工作 |
|---|---|---|
| 私有实际解析、PG 向量/BM25 候选和稳定 KID 原子发布 | IN_PROGRESS | 已实现；真实 PG 成功、回滚、配额失败、模型变化、ACK 丢失及旧 Worker 竞态测试通过；最终候选完整重验与 APP 集中验收未完 |
| 持久 HOLD/revoked、候选过滤、正文/文件/模型复验 | IN_PROGRESS | 已实现；注册 local backend/ResourceCatalog、跨空间请求来源、HTTP 写出及历史日志安全收口；真实云端、CDN 和不完整历史来源仍受限 |
| 游标与真实发布回执一致、幂等恢复 | IN_PROGRESS | 原两处防护保留，新增实际回执事务校验和完整来源对账；真实 APP/队列故障验收待执行 |
| 独立 RC3 最终开发候选 | IN_PROGRESS | `b78966cf1556cd581b5db4428f38ba273c284bea5cb2590111e06f22df495746`，原始命令及退出码位于 linux-b2-acceptance-source/results |
| 联合最终开发候选 | IN_PROGRESS | `1d733295720560c773624954b41353889c5081bf0fb596dada7055da98ed2440`，原始命令及退出码位于联合树 linux-joint-acceptance-source/results |
| 联合完整回归历史失败 | BLOCKED | c13e04d 候选的 V1 共用夹具仍断言 3035，实际 3036；失败保留。版本断言和明确回退目标修正后完整重验，未删减权限断言。另新增 DTO 测试类型问题已修正，原候选证据保留 |
| 完整源码冻结与新独立部署 | TODO | 新脚本与合成 Wiki/Drive 服务已准备；不得在最终开发门禁通过前冻结/部署；5468 原实例保留 |
| 真实 APP、Worker、前端、Nginx、PG/ParadeDB、Redis 联合验收 | TODO | 独立项目、镜像、卷、内网和回环入口；实际 MCP、历史链接、撤权恢复、备份及关闭功能待执行 |
| 真实云端、正式模型、真实 SID | BLOCKED | 没有本轮单独授权测试应用/文档和必要正式环境输入，不使用生产来源替代 |

新操作卡：[候选发布与恢复](FEISHU_INCREMENTAL_RELEASE_AND_RECOVERY_RUNBOOK.md)。默认回滚保留 3036 和加固读取，不能用旧 APP 绕开已有拒绝状态。完整实现持续记录见 [阶段 B](FEISHU_INCREMENTAL_STAGE_B_ATOMIC_IMPLEMENTATION.md)。

| 依赖 | 项目 | 状态 | 证据 / 剩余事项 |
|---|---|---|---|
| 无 | A：核对 V1 三报告与源码、原需求 | PASS | 阶段A报告含9项矩阵；明确未来私人空间缺口，未扩大旧Key |
| 无 | A：保护冻结包及运行输入 | PASS | evidence/feishu-incremental/preservation.json；当前比较范围零差异、v4 整包 SHA 一致 |
| 无 | A：RC3 独立工作树 | PASS | fix/feishu-safe-incremental-20261021；HEAD e62b17e2d305358467821aeff1cce63ce1d61254；不移植 MCP 源码 |
| 无 | A：生产停更实际状态、测试主机只读核查 | PASS | production-readonly.json、build-host-readonly.json，SSH exit0；生产 flag=true、Schema3033 clean |
| A | A：稳定 KID / Cursor / 索引 / Worker 调用链与独立补丁评估 | PASS | 阶段A风险分析；staging writer 缺失，不能直接使用 ReplaceKnowledgeFile |
| A | B：补强 checkpoint 和删除失败的 Cursor 拒绝传播 | PASS | a54ccc候选，修复前Linux两项exit1、修复后exit0；原FAIL保留。仅局部保护完成 |
| A | B：私有候选解析/Embedding/索引、同 KID 原子可见切换 | IN_PROGRESS | b2-http1：真实 Wiki/Drive HTTP 连接器→ProcessSync→实际 Markdown parser→既有模型客户端/合成 HTTP 1024维→真实PG/ParadeDB exit0；稳定KID、重复跳过、失败保旧；APP联合部署尚待执行 |
| 上项 | B：新旧 Worker fencing、过期任务清理只限所属版本 | IN_PROGRESS | 3036真实写边界围栏；未提交旧事务接管、过期租约接管、迟到旧写/清理、真实COMMIT后ACK丢失、旧回执不能确认游标已通过；真实Worker/Redis集中验证仍待完成 |
| 上两项 | B：可信删除/移动/撤权与持久拒绝、故障分类、积压重放 | IN_PROGRESS | 不可信缺失和403先持久HOLD/私有快照、不物理删除；429/5xx保旧；源配置变化/删除收紧；真实PG候选和直接ID、文件复验、模型spy；真实云端可信撤回事务入口及全读取面矩阵仍待验收 |
| B | B：Go / race / vet / APP 构建、兼容及故障注入 | IN_PROGRESS | Cursor保护相关测试/race/vet/build exit0；未过滤完整套件首次DNS FAIL保留，受控解析重验exit0但Feishu3/service90项SKIP；完整增量故障验证尚未执行 |
| B 完成 | 冻结独立飞书修复、语义合并联合候选 | IN_PROGRESS | 已建立全新knowhub-v1-feishu-integration-20261021树，V1冻结源码核验后移植；PostgreSQL保留原Grant/tenant/public谓词并AND新增拒绝；3036保留原3034/3035；尚未冻结或部署 |
| 冻结 | C：新独立 Compose、同源镜像、PG/ParadeDB/Worker集中测试 | TODO | 5468项目五容器运行中，保持不变；新项目独立DB/Redis/网/卷 |
| C | C：MCP、Codex五工具、3033→3035、恢复及性能回归 | TODO | 不继承5468 PASS为新联合候选PASS |
| C | 正式环境 SID、正式模型、授权测试飞书、实际数据范围 | BLOCKED | 本轮未提供真实测试应用及文档授权；不访问生产来源 |
| 安全门禁 | 生产恢复停更与正式发布 | BLOCKED | 需另行批准，截止2026-10-27 19:25（北京时间）按用户本轮指令记录；不自动延长或解除 |

“未来私人空间自动进入”本轮仅做差距与显式动态模式分析，不扩大旧 Key，也不扩大 Go MCP 开发。

后续独立V1增强：明确`fixed / all_current_authorized`私人范围模式；新Key可显式选择当前及未来有权空间，实时与真实权限取交集；历史Key保持fixed，机器Key不得使用。TODO，不混入飞书RC3补丁。

47/48集合核对：PASS（只读）。`evidence/feishu-incremental/kb-range-set-difference.json`：历史48项中1个document KB已软删除，对应空间仍active/Published；没有新增有效KB，没有读取正文或更改生产。

迁移兼容实施中：独立修复使用3036，保留V1的3034/3035原SQL SHA，作为连续迁移链携带而不复制MCP功能源码；不跳过3034/3035升到3036后再误称联合候选可补执行旧版本。SQLite仅schema对称及拒绝能力，原子发布只支持共享PG；全部双库升级/恢复门禁仍待执行。

阶段A已交付 [需求与风险](FEISHU_INCREMENTAL_STAGE_A_REQUIREMENTS_AND_RISK.md)。阶段B**部分完成**：[Cursor保护修复](FEISHU_INCREMENTAL_STAGE_B_GUARD_REPAIR.md)。阶段C **TODO**：[集中验收门禁](FEISHU_INCREMENTAL_STAGE_C_ACCEPTANCE_GATES.md)，该文件不代表已部署或验收。


??PG?????????P0?IN_PROGRESS???[?????????](FEISHU_INCREMENTAL_PG_CANDIDATE_SECURITY_FINDING.md)???1380???d321????????????????????????????????????????????


???????????????runtime `2ac380a033af80f022d98a3b45cd5e30645b148d07cde8c76c46cce94787392b`???runtime `1d533edfe33957dc6b74960594ff7d12ab33e4cfe670f631d10074fc9e9bad1a`?????PG????????service?????????Go?????????????????????????????PG/race/PG vet??exit0?????????`linux-b2-materialized`?`linux-joint-release-source`?????1380/d321???


?????PASS???2ac380a0???1d533edf?21/21??exit0????????SHA???????SKIP?`development-final-<runtimeSHA>.json`?SKIP??NOT_TESTED?????/????/??APP?????IN_PROGRESS???????????
