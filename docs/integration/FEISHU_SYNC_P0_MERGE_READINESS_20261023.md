# 飞书 P0 合并候选与发布门禁（2026-10-23）

## 修改前审计与方案

- 基线：`origin/main=b64ab3b6b6dc4dde12adba5dd929be26fb9a16c8`。相对 P0 原基线 e3cc0d39c，只有 MCP 上线文档及证据变化，无运行代码变化。已向 P0 分支无冲突合入；未向 main 合并。
- 原 P0 验收见 [交付报告](FEISHU_SYNC_P0_20261023.md)。本轮必须重新验证最终提交，旧 CI 不作为新候选验收。
- 承载审计：sync 仅由 maintenance 池消费，默认每副本并发 4，shared 池不消费 sync。批量 20 是单次输入上限，不是 QPS 或全局并发限流。模型 governor 在 Redis 故障时 fail-open，不能当作硬网关保护。
- 现有总暂停开关不能表达单源灰度。拟新增默认兼容的 `FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS`：空值保留旧行为；`none` 全部拒绝；其余必须为逗号分隔的规范 UUID 白名单，任意格式错误全部拒绝。仅约束旧飞书/Lark Wiki/Drive 同步，覆盖定时注册、定时准入、手动准入及旧任务执行。禁止通过写 paused、改 cron 或修改权限来实现灰度。
- 白名单不是运行中任务的排空协议；配置变更必须停旧生产者并排空旧执行者，所有副本一致配置。保留总暂停、SSRF、权限、generation/claim/lease、归属和原子发布保护。
- P1 父子分块及版本归属另设分支，不修改本轮未确认路径。无数据库迁移。

后续章节记录最终测试、容量预算、灰度与回滚约束；执行证据以 PR 的最终 HEAD 和同 SHA CI 为准。

## 根因、候选范围与安全复核

|问题|确定的原因及源码|候选行为|
|---|---|---|
|Embedding 超限|generic 未声明网关批量约束，暂存直接整份调用；`internal/models/providers/generic.go`、`internal/models/runtime/resolve.go`、`internal/application/service/feishu_legacy_staging.go`|模型 `extra_config.embedding_max_batch_size` 可收紧模型原上限；`internal/models/embedding/bounded.go` 在旧飞书暂存路径再串行限制 20；数量/顺序校验，任一批失败不发布|
|Redis archived / DB running|生产归档元数据明确 `asynq: task lease expired` 且重试耗尽；Asynq recoverer 可在业务 handler 未执行时归档，defer 无机会收尾。最初导致续租失败的基础设施原因仍未确定|`datasource_execution.go` 补最终写回；`datasource_reconcile.go`（service/repository）、`router/sync_task_inspector.go` 对 Redis 活跃队列快照及陈旧 DB 任务进行 CAS 对账；活跃/证据不全不清理，老 generation 不得覆盖新执行|
|失败重启后无 cron|原 `FindActive` 与 Scheduler 将 status=active 等同启用；error 被排除|`types/datasource.go`、`repository/datasource.go`、`datasource/scheduler.go` 从现有 cron/status/revoked 推导资格，无默认字段回填；5/10/20/40/60 分钟冷却，只在原 cron tick 重试，保留 paused|
|首次恢复范围|总暂停只能全关；队列可能含老任务|`datasource/legacy_feishu_scope.go`、scheduler、service 三处同步准入覆盖注册/入队/执行。被排除任务 canceled，不重试；源健康、cursor、最近成功、cron、权限不改|

原子暂存/发布、资源归属、SSRF、访问权限、claim/generation/lease 均保留。白名单不授权访问，也不恢复 paused/revoked。无迁移、无知识重抓、无旧来源删除。白名单的 unset 与严格的空字符串保持兼容；空格、`*`、无效 UUID、混有坏值或尾逗号均拒绝全部旧飞书源。`none` 为明确的全部拒绝值。只接受数据源自身 UUID，不能填 tenant ID、知识库 ID 或名称。

## 最终提交与验证证据约定

本报告与代码一起提交，避免在验收后追加文档提交导致 HEAD 与测试 SHA 不同。PR 正文记录最终完整 SHA、基线 SHA、每项检查状态及 Actions 链接；此文件不嵌入自身提交哈希。专用 CI 开头强制 `git rev-parse HEAD == GITHUB_SHA`，末尾上传 `feishu-p0-tested-source/tested-source-sha.txt`。验收必须同时核对 PR headRefOid、origin 分支 SHA、Actions head_sha 与该文件；不能用旧 ba63 验收代替。

|检查|隔离环境、命令和通过要求|
|---|---|
|P0 专项 / 故障注入|`.github/workflows/feishu-sync-p0.yml`：Go 1.26，Redis 7，ParadeDB v0.22.2-pg17；`go test -tags sqlite_fts5 ./internal/models/... ./internal/datasource ./internal/application/repository ./internal/application/service ./internal/router ./internal/container -run 'P0\|TestSync(Execution\|Service\|Scheduler\|Recovery)(SQLite\|Postgres)$' -count=3 -timeout=15m`|
|真实原子发布|`go test -tags sqlite_fts5 ./internal/application/service -run '^TestFeishuLegacyRealParserEmbeddingPostgres$' -count=1 -timeout=10m`；真实解析器、PG 向量/BM25；中间批失败后旧 JSON/Chunk/向量一致，旧检索可用，未发布 canary 不可见|
|race|工作流指定 6 个包及 P0/既有执行用例，`-race -count=1`；必须无数据竞争，包含真实 Redis/PG 归档恢复|
|全量后端|`go test -p 2 ./internal/... -count=1 -timeout=20m`；另由 App PR 检查运行全仓后端 vet/test（排除独立 docreader）及 desktop 容器测试|
|范围门禁|新增 scope P0 测试：四种旧 connector；空值兼容、混合坏值拒绝；SQLite/PG 单源 cron 注册及定时入队；active/error/paused × manual/schedule 的旧投递取消、重复投递幂等及元数据保留|
|既有安全回归|PR Portal 权限/迁移检查及全量 internal，暂存原子发布、CAS 过期任务 fencing、paused-after-enqueue、连续失败/再次成功/并发重复、重启恢复均需通过|

专项 PG 通过 `REQUIRE_SYNC_EXECUTION_POSTGRES=1` 强制，不能跳过。全量 internal 清除专项 DSN，其他依赖专用 fixture 的历史可选集成测试按原规则跳过，不声称它们已经执行。Redis 只使用专用 loopback 变量，不继承生产连接，不 FLUSHDB。原 1/20/21/40/41/100 分块、反序 wire 响应、第二批 HTTP 400、归档 lease 过期、分页/NULL 心跳/并发 CAS 的验证继续保留。

P0 push CI 测试确切 HEAD。通用 PR 工作流默认检出 GitHub 临时 merge ref；在 main 是候选祖先且 base 未变化时，合并树应与候选树一致。两种身份分别记录，不把 PR merge SHA 说成提交 SHA。合并前若 base 或 head 变化，必须重新核验并触发新候选测试。本 PR 显式跳过 App 的 server 发布二进制及镜像 smoke 构建，不发布任何包或镜像；合并后的正式镜像/startup smoke 是后续另行授权发布前的必要门禁。

## 恢复请求量、并发与网关风险

下面为 2026-10-23 原调查快照，不是本轮重新查询的生产现状：12 个旧飞书源，6 active、4 error、2 paused。若 cron、权限及状态无变化，恢复 error 可能使可调度数量从 6 增至 10；原 4 个 active 的六小时计划加 4 个 error 六小时计划，可能同 tick 产生 8 个逻辑同步，另有 2 个凌晨计划。重试、手工请求及已有 backlog 还会改变到达时间。必须在发布前重新只读盘点，不能把这里的数量直接用于生产配额。

- 每源逻辑任务用 DB 准入去重，执行由 claim/generation 排他；这不等于全局流量控制。默认 maintenance 并发为每副本 4，sync 和 low 共用该池；shared 不消费 sync，集群上界是所有消费副本 maintenance 并发之和。
- 每份候选文档串行 embedding，批数为 `ceil(chunks / min(20, 模型有效上限))`。多个文档/任务、解析或其他后台任务与在线 query 共享网关，不能把“每批 20”当成“每秒 20”或总并发 1。
- 同步请求预算还包括分页、元数据、下载/导出/轮询及 HTTP 层重试。`MaxRetry(5)` 允许初次加至多 5 次基础设施重试；业务确定性错误会终结逻辑 run。冷却只约束原定时 tick，不约束手工点击及 HTTP 内部重试。
- 历史本地文档计数（如 CSBU 266、国际安全概念1 115）不是源端清单，不能推导 381 次请求；hold/full-reconcile 也可能读取未改变资源以核验 hash。估算须测得实际资源数、chunks、导出轮询分布、延迟和重试率。
- 粗略 embedding 吞吐为 `并行请求数 / 平均请求秒数`，但短延迟会显著放大 QPS；容量预算必须覆盖其他客户端。模型 `model.max_concurrency` 默认 32，模型自身 override 可改变限额，且 governor 在 Redis 故障时 fail-open；在线 query 不受后台 governor 同一方式保护。**需要网关/出站代理的硬 QPS、in-flight 和故障熔断门禁，未确认其存在及配额前禁止放量。**

首次恢复范围必须同时满足：

1. 所有副本显式配置 `FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS=none` 起步；记录既有总暂停值，不能擅自解除。未配置不是安全发布配置（为兼容旧行为会全放行）。
2. 经批准只填 **一个** 非关键、权限正常、无父子分块/版本冲突、存储及 DNS 已验证的数据源 UUID。暂无符合条件的源就停止发布，不选“国际安全概念1”等尚有 DNS/权限疑点的源作首次灰度，不改变它们的权限。
3. 把实际 `asynq.maintenance_concurrency` 控制为每消费实例 1，首次仅一个实例消费 sync，其他副本 sync 队列保持暂停/不消费；需要验证进程拓扑，不能假设 API 副本没有内置 worker。暂停是 Redis 队列全局状态，不能用同一队列的暂停来声称只停某个副本。多副本无法隔离消费时仍只允许一个源，并以网关全局硬限流兜底；不把集群并发声称为 1。
4. 系统配置优先级 DB > ENV > default，必须记录实际生效值；`WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY=1` 不能覆盖已有 DB 设置，0 也不是停机开关。maintenance 还承担 low 任务，先确认共享任务不会被饿死。后台模型限额可辅设 1，核对 override，但不是硬容量保障。
5. 网关独立给灰度来源设置 `in-flight <=1`、保守硬限 `<=1 request/s` 且不得超过已确认剩余额度的 10%，无 burst；若该值仍超实际 RPM/TPM 则进一步收紧。若无法区分灰度流量或无剩余额度数据则 no-go；不更换/扩权生产凭据来绕过限制。需测 chunk token 量并施加 TPM 预算，不能只验请求数。

## 发布前检查清单（均尚未执行生产操作）

- [ ] 最终 HEAD 的专项、真实 Redis/PG、原子发布、race、全量后端均 success；PR 检查通过，base 未漂移，无 P1 修改，无冲突；人工审阅差异，未开启自动合并。
- [ ] 另行批准合并与发布；当前任务不包含合并、构建发布包或生产部署。正式发布构建必须从批准 SHA 产生不可变 digest，并补齐本轮跳过的镜像/startup smoke。
- [ ] 发布前只读盘点源 ID/tenant/KB、status、cron、revoked/hold、backlog/retry/active、所有 worker 副本及生效配置；选择单源清单并由业务确认，保留原 paused/revoked 和未解决 P1 源不动。
- [ ] 保存旧镜像 digest、完整配置、队列原暂停状态、源配置快照；依据现有策略完成一致性备份及可恢复性核验。保留旧来源和所有备份，无 schema migration，无数据回填。
- [ ] 记录知识/Chunk/向量、版本归属、ACL、cursor、最近成功和检索样本基线（按源可比范围）；保护快照中的 token/凭据，不进 PR 日志。
- [ ] 确认网关 RPM/TPM/并发额度及其他流量基线，部署硬限流/熔断；证实多个消费者共用限额。验证一次熔断演练，不用生产 Embedding 故障注入。
- [ ] 配好下表监测采集和控制器动作，验证告警到停机的链路。此候选提供门禁和策略，**没有操作生产控制器，也没有声称自动停机已部署**；未接线验收则 no-go。
- [ ] 先暂停 sync 队列并停旧调度生产者，等待活动任务完成或经受支持的取消流程退出并确认 generation/claim 失效；仅暂停队列不会中止正在运行的任务。全体新副本必须 scope=none，避免混跑旧副本绕过门禁。

## 灰度与验收

1. **关闭准入启动**：scope=none，全局 sync 暂停，确认健康、原知识与检索样本、权限和配置不变。待既有总暂停策略允许后才解除总暂停；该开关处理旧排队任务会保留其既有 paused 行为，不用它替代本轮范围门禁。对账是只读队列 + CAS 终态修复，观察 orphan 转 failed、旧代写回被拒绝；正常 Redis 证据下最多约 30 分钟陈旧阈值 + 1 分钟轮询 + 单次 45 秒执行预算，DB/Redis 异常则 fail-closed 告警，不承诺该时延。
2. **单源**：完成排空并一致更新白名单为一个真实源 UUID，硬限流/总并发验证后恢复队列。被排除的旧任务将 canceled，不改源状态或旧知识；保存这份取消清单，后续按原计划重建同步，不重放历史 payload。仅执行经批准的一次同步，禁止批量补跑和改所有 cron。生产不做第二批失败注入；该断言由同 SHA 隔离测试承担。
3. **验收**：更新文档超过 20 分块的实际批量均 <=20/更小配置；结果完整且成功发布可检索；未修改文档、历史 ACL 和归属无变化；失败内容未发布且旧内容仍可检索；Redis/DB 对应同 log+generation，paused/revoked 无访问。若灰度源不含 >20 分块变更，批量生产验收未完成，不能伪报。
4. **完整观察**：至少 24 小时、包含一个原计划周期及一次受控重启。error 按原 cron 恢复，连续失败冷却可见，不人工制造源端错误；重启注册数量等于白名单内 active/error 且非 revoked、有 cron 的源。保留跨租户及 paused 样本检查。
5. **扩围**：每次只增加一个确认无 P1/权限/DNS问题的源，在硬预算内至少一个原计划周期无异常再扩；不直接切为空值放开全部。所有 error 源恢复前重新测算请求量。每次配置变化排空并核对所有副本一致；保持失败源原因可见，不静默改 paused。

|指标 / 条件|自动动作与恢复门槛（须发布控制器接线验收）|
|---|---|
|非白名单源任何出站同步请求；paused/revoked 被自动启动；跨租户访问；ACL/归属异常；失败候选可见；旧知识不可检索|任一事件立即停止灰度：网关熔断灰度流量 + 全局暂停 sync，新部署 scope=none，取消/排空活动任务并核对 fencing；内容/权限风险禁止自动重启，人工审查后决定镜像回滚|
|网关超配额、429，或灰度 in-flight>1 / 硬 QPS/TPM 越界|首次即熔断灰度并暂停；不把错误交给无限重试。查明预算、其他客户端和限流故障后重新从单源开始|
|网关 5xx/timeout 连续 3 次或 5 分钟 >1%（至少100请求）；在线请求 p95 连续5分钟高于基线20%|停止灰度出站/入队；不能以降低安全检查换吞吐；连续两个5分钟窗口仍异常则回滚应用或继续冻结，由事故负责人判断是否相关|
|Redis 快照失败/超100000安全上限、DB写回失败、lease expired、同源重复执行/旧代写回成功|任一新事件即冻结扩围和暂停 sync；Redis/DB 不可靠时不自动清理队列或强改状态；保留证据并恢复依赖后重新验收|
|已确认 Redis 归档/消失、心跳陈旧>30分钟的 DB running，在健康依赖下再过2分钟仍未收敛|停止灰度、告警；检查对账轮询/分页/CAS，不通过把所有 running 标失败来掩盖|
|本轮单源连续2次逻辑失败，或 backlog oldest>15分钟且连续增长10分钟，或维护 low 队列明显阻塞|暂停灰度和扩围，保留 error/失败原因，确认网关及维护池预算后重试；不全量恢复 error，不清游标|
|监测/自动停止控制器失联或指标缺失>2分钟|故障关闭灰度网关，暂停 sync，不能在不可观测状态放量|

自动停止先保护流量和内容，不能立即杀 worker 再启动旧镜像。镜像回滚适用代码回归/持续影响在线服务；源端拒绝、DNS 或容量不足优先保持冻结调查，不假定换镜像能解决。任何自动化必须限制操作范围并提供审计事件、执行失败告警；没有部署权限的监测器只报警不能算已具备自动停止。

## 回滚步骤与证据要求

1. 记录触发规则、源/log/task/generation、队列/网关指标及已发布版本清单；全局暂停 sync，熔断灰度出站，停所有调度生产者，范围设 none。经支持的取消/排空流程确认旧执行停止和 fencing 后才切换，不能只修改一个副本 env。
2. 切回发布前保存的精确镜像 digest 与配置快照，不从 main 临时构建。**旧版不认识白名单**，必须保持队列暂停且所有旧生产者受既有总暂停门禁限制，不能只留下 scope=none 就启动旧同步。恢复旧总暂停值应单独评审；优先保持飞书同步冻结、线上查询可用。
3. 无数据库迁移，不执行 downgrade、整库覆盖、删来源/Chunk/向量/备份、清队列或回退已正确发布内容。若确有错误内容，依原子版本恢复机制另行审批处理，不能宣称镜像回滚会撤销内容。
4. 对照基线重查知识、检索、ACL、归属和源配置；对已取消/归档任务留存清单，由后续原 cron 产生新任务，不直接重放过期 payload。旧版 error 自动调度及 archived/running 缺陷会重现，保持同步冻结并人工跟踪。
5. 保存回滚 digest、时间、配置、活动执行归零/失效证据和在线服务恢复指标。停止后不得自动反复重启灰度；须查明原因并重新审批。

以上是可审阅的生产操作方案，未执行生产部署、故障注入、扩权或回滚。实际生产上线与回滚演练证据不可能在本阶段生成；交付的是同 SHA 隔离测试证据与明确发布门禁。

## 未解决事项与 P1 隔离

- P1 父子分块及版本归属单独从 main 建分支 `fix/feishu-sync-p1-parent-child-version-20261023`，本轮仅预留分支，不实现 P1，不绕过既有拒绝保护。
- 飞书源端实时 ACL、国际安全概念1 的 DNS/SSRF 原因、具体基础设施续租故障仍需另行受控调查；本轮不触发生产同步、不扩大飞书权限。
- 网关实际额度、集群 worker 数量及硬流控/自动停止控制器的生产接线需发布前确认；它们是 no-go 条件，不能仅凭 P0 测试通过宣布承载风险已消除。
