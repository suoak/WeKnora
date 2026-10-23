# 飞书同步可靠性 P0：审计、修复与验证

基线：origin/main e3cc0d39c。只在独立工作树开发，不修改生产，不触发生产同步；开发和测试不使用生产数据库。追加的单条归档任务只读元数据核验见下文。

## 修改前审计与方案

- Embedding：generic 未声明批量上限；runtime.resolveEmbeddings 没有处理 extra_config 的批量设置；protocol.BatchEmbed 只按已解析上限拆批；feishu_legacy_staging.go 直接提交整份文档分块。方案：增加可验证的每模型上限设置，并对飞书安全暂存的直接调用增加默认 20 的串行边界；任一批失败整体返回错误，发布仍通过既有原子协议。
- 终态：datasource_execution.go 已有 generation/heartbeat/defer，但进程被杀不会运行 defer；datasource_ingest_error.go 为重试主动保留 running；router/task.go 的死信回调只处理 knowledge，不处理 datasource；知识 housekeeping 没有同步对账。故 Redis 终态不保证数据库终态。生产证据只能证明归档与 running 不一致，不能唯一还原当时是否被杀、数据库写失败或哪次重试终止。方案：保留既有正常收尾，增加读取真实队列的周期性同步对账，CAS 比较 generation、claim 和更新时间，失效旧执行且不更新内容、权限、游标或最近成功时间。
- 调度：FindActive、Scheduler.Start/AddOrUpdate/triggerSync 把执行健康 status=active 同时作为启用条件，error 在重启及 tick 时均被跳过。方案：明确 AutoSyncEnabled 为现有状态的兼容性解释（active/error 允许，paused/deleted 禁止，空 cron 禁止），不增加默认字段或回填历史数据；根据连续失败日志实施有上限退避，不改用户 cron。

边界：不修父子分块、源修订冲突、DNS 和飞书授权；不关闭 SSRF、租约、权限、版本或原子发布检查。

## 验证记录

首轮提交 881299438 的隔离 CI：故障注入/SQLite/PostgreSQL x3、真实解析器/原子发布、race 均通过。普通全量回归发现新增模型字段缺中文 placeholder，已补齐。首轮还将强制 PostgreSQL 变量泄漏到了未配置其专用 Redis 的历史集成用例，已把普通全量步骤与强制 P0 集成步骤的环境隔离；这些历史专用 Redis 集成测试不记为通过。

## 历史事故的进一步只读核验

2026-10-23 16:38 的只读补查解码了“项目信息”归档任务中保存的 Asynq protobuf 元数据（未导出 payload/凭证）：

```json
{"state":"archived","task_type":"datasource:sync","max_retry":5,"retried":5,"timeout_seconds":7200,"last_failed_utc":"2026-10-23T04:14:18+00:00","last_error":"asynq: task lease expired"}
```

确定的直接原因：Asynq 任务租约过期且已耗尽重试；其 recoverer 在 handler 外直接调用 broker.Archive，不执行应用 handler 的 defer 或 mux 死信 middleware。依据本项目锁定的 asynq v0.26.0 源码 internal/recoverer.go 的 recoverLeaseExpiredTasks / archive，可完整解释 Redis archived 与数据库 claimed=true/running 脱节。应用只有知识文档的死信回调，且没有周期性数据源队列对账，故不能保证收尾。此次新增的对账不依赖 handler/dead-letter 回调是否得到执行。

租约为什么没有续期（进程退出、Redis 网络中断、资源阻塞等）仍需当时基础设施日志，不能从归档错误唯一推断。错误与失效 generation 的修复不需要放松或延长租约。

## 最终实现与兼容性

1. `models.ExtraEmbeddingMaxBatchSize` / runtime.resolveEmbeddings 接受 `parameters.extra_config.embedding_max_batch_size="20"`。必须为正整数；只允许收紧供应商已声明的上限。generic 的模型表单显示此字段并提供中文说明。未设置的普通模型调用保持原行为。
2. 飞书安全暂存使用 `embedding.BatchEmbedBounded(...,20)`，历史未配上限的模型也能满足该网关约束。串行累计、按原顺序返回；中间批失败或数量不符时返回 nil/error，不进入文件写入及原子发布。内部 BatchEmbed 仍可按更低模型上限继续拆批。
3. ProcessSync 的 defer 补充终态时对已释放 generation 的 CAS 收尾。正常成功/失败、取消及 panic 继续使用原执行协议。
4. 独立的 DataSourceSyncReconciliation 启动时及每分钟运行，单轮 45 秒超时；只检查超过原 30 分钟陈旧窗口的 running。Redis 通过原子 Lua 获取 pending/active/scheduled/retry 集合，归档/完成/消失任务不算存活。Redis 异常、无法完成查询、超过队列安全上限则不修改数据库。
5. 修正 running 的 CAS 同时检查 tenant/source/log、generation、claim、updated_at，并递增 generation、清除 claim，阻止旧 worker 写回。不触碰知识、Chunk、向量、权限、游标和最近成功时间；同源有更新任务时不覆盖它的数据源状态。
6. `AutoSyncEnabled` 统一解释已有配置：有 cron，status 为 active/error，且未显式 revoked 才允许自动同步；paused/deleted 禁用。FindActive 的接口名保留，但按上述语义筛选。既有安全暂停开关继续生效。
7. 原 cron 不改。连续失败时在原计划 tick 上采用 5/10/20/40/60 分钟冷却上限；成功重置连续失败。手动重试不受冷却限制。执行前再次检查 scheduled delivery 是否仍允许，避免暂停后的旧排队任务继续运行。
8. 领取中的超时执行仍保留原租约失效恢复；没有 claim 的排队/重试记录不再仅按年龄被准入或启动清理误杀，交给队列对账确认。

无需数据库迁移：不新增或回填列，不改模型既有参数，不改用户 cron/paused 设置，复用已有 generation/claim 字段及日志。extra_config 仅新增可选键；回滚旧代码会忽略它。

## 必测用例与故障注入

| 要求 | 对应测试及断言 |
|---|---|
|1/20/21/40/41/100 条输入|TestGatewayBatchLimitP0：直接模型配置与安全暂存边界两条路径；网关拒绝 >20；逆序 wire 响应验证索引、数量和顺序|
|中间批失败|TestGatewayMiddleBatchFailureReturnsNoCandidateP0：第二批 HTTP 400，整体 nil/error；TestFeishuLegacyRealParserEmbeddingPostgres：真实解析与 PG 向量/BM25，旧知识/Chunk/向量 JSON 不变，未发布 canary 不可检索|
|租约过期归档但 DB running|TestArchivedSyncDatabaseRecoveryP0：给第 5 次重试注入过期 Asynq lease，启动真正 recoverer；断言归档原因为 ErrLeaseExpired、handler 未执行、对账使 DB 终态、generation 递增且重复运行无变化|
|并发与过期结果|TestSyncReconcileSQLiteP0 / PostgresP0：心跳/重领竞争、Redis 不可用、队列等待、暂停、后续成功；旧 generation 心跳及成功写回被拒绝。既有 TestSyncExecution / Service / Scheduler 覆盖 20 路竞争及重复执行|
|最终重试、异常退出|TestSyncService(SQLite/Postgres) 中 panic_final_P0、failure_retry_P0：失败后成功、连续失败耗尽、panic 终态、后续新任务能准入|
|重启与暂停|TestSyncScheduleSQLiteP0 / PostgresP0：两次实际启动，恢复 active/error，排除 paused/deleted；TestSyncRetryCooldownP0；pause_after_enqueue_P0 验证旧计划任务不触发连接器|
|历史与权限|原子发布测试比较既有知识及索引；普通全量 internal 回归包含既有知识库可见性、权限、Portal、迁移与版本保护测试|

专用 P0 集成步骤强制要求 PostgreSQL，不能跳过冒充通过。普通全量步骤清除集成环境变量，其未配置的历史专用环境测试会按原规则跳过；这是与本阶段定向 PostgreSQL 验证分开的覆盖范围。

## 生产部署与验证方案（未执行）

前置条件：最终代码 SHA 的 P0 故障注入、PostgreSQL 原子发布、race、普通全量回归均通过；评审本报告；由用户另行批准部署。

1. 保存旧镜像 digest、部署配置及完整回滚清单，保存数据源 cron/status/legacy_access_state、运行日志与知识/Chunk/向量数量快照；按原备份策略创建一致性备份，保留全部旧来源与备份。
2. 在维护窗口暂停同步队列消费并记录队列原暂停状态；控制计划启动，避免部署后 error 源在尚未验收时批量访问飞书。保留原 suspended/paused/revoked 设置，不通过清空游标或重置源来恢复。
3. 构建经过验收 SHA 的镜像，记录不可变 digest，更新镜像后检查启动/健康状态、调度注册数量、原子发布配置和 SSRF/租约开关。只对确有网关约束的模型设置可选 embedding_max_batch_size=20；历史模型已有 staging 20 的保护，不需要批量改模型。
4. 在批准范围内选一个源，先验证本地旧文档及检索，再恢复该范围的队列消费/计划并执行受控验证。验证实际变更文档超过 20 个分块可以入库，失败仍能检索旧内容，任务成功/失败与 Redis 一致，paused 源始终未启动。
5. 验证错误源在下一原计划 tick 可重试，检查日志中的冷却原因及最近失败详情；逐步恢复维护窗口前允许的调度范围，不改变原暂停选择。
6. 观察至少一个完整原计划周期及一次重启：无新增长期 running；对账失败有日志；知识/Chunk/向量变化都能对应成功发布；旧来源、权限和不可变文件无非预期变化。

风险：本次恢复 error 源的原计划，会增加此前被跳过的任务；须控制放量。对账保留 30 分钟陈旧窗口，因此不是即时归档通知。队列扫描达到安全上限或 Redis 故障时优先不写状态，需运维关注告警。直接 staging 按 20 串行请求可能增加大文档同步时间，但避免一次请求超过网关限制；不改默认并发。

## 回滚方案（未执行）

1. 暂停受影响同步队列/调度，记录仍在运行的任务，不直接清队列或删除数据。
2. 切回保存的旧镜像 digest 与配置；本阶段无数据库迁移，不运行反向迁移，不恢复覆盖整个生产库，不删除当前已成功发布内容。
3. 按 generation/任务状态确认旧 worker 已停止或失效；仅恢复维护窗口前允许的队列/计划设置。旧版会重新出现 error 源不自动注册及缺少对账的行为，回滚后需要明确运维跟踪。
4. 验证本地旧知识读取、Chunk/向量检索和权限，核对回滚前后内容变更清单；保存故障现场与备份供后续修复。

## 未解决的问题

- CSBU质量管理父子分块与安全暂存不兼容：P1，不在本次修改范围。
- CSBU 源修订/内容哈希冲突的具体分支：证据仍不足，本次不绕过版本保护。
- 国际安全概念1 的历史 ::1 DNS 解析原因、源端实时权限与客户调研服务可用性：需后续受控验证，本次不改 SSRF、不扩权、不触发生产同步。
- 导致 Asynq 续租失败的基础设施事件：已确定直接归档错误和重试耗尽，尚不能唯一归因于进程、网络或资源问题。
- 本次修复不会把所有历史文档重新抓取；可见性恢复、同步机制修复和源端内容完整性需分别验收。
