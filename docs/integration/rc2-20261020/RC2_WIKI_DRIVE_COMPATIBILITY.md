# RC2：既有飞书来源增量兼容性

状态：BLOCKED。当前安全拒绝保留；未恢复旧 Replace。RC2 尚不能宣称正常变化版本更新已通过。

## 固定源码及实际差异

当前生产 main/RC24：`854f3b956ae3600e12a3ffd2fc40d4fe624ced74`，本轮重新以 `git ls-remote` 核对。
RC1 产品源码 Manifest：`1ffa81392e28d83fd31dd0ab853aa250782e9557c88044e1ec03a653c23cd721`。
RC2 独立工作树：`E:/code/WeKnora-worktrees/knowhub-oa-feishu-rc2-20261020`。

生产版本 `datasource_service.go::ingestItem` 对已存在的 external_id 调用 DeleteKnowledge/HardDeleteKnowledge，再创建随机新 Knowledge。候选将 feishu/feishu_drive/lark/lark_drive/feishu_personal 纳入持久接纳保护，因此旧来源也受影响。不是仅个人导入的限制。

| 输入/状态 | 当前行为 | 兼容性与数据影响 |
|---|---|---|
| 新 external_id | 现有 Create + 持久派发 | 首次接纳；接纳不等于索引完成 |
| 同字节、同文件名，completed/processing/finalizing | Duplicate，保留 KID | 可重放；不执行 Replace |
| 同字节、同文件名，pending/failed | ResumeKnowledgeDispatch 原 KID | Outbox 恢复；不能先清理旧 Chunk |
| 字节变化 | ErrFeishuStagingRequired | 变化版本不发布，旧内容保留；正常增量未兼容 |
| 仅文件名变化 | 同上 | 即使正文相同也拒绝；不能宣称重命名兼容 |
| 已有记录但 URL-only/空 Content | 同上 | 无法证明相同版本，保守拒绝 |
| 已有记录查询失败 | 停止创建/确认 | 防止第二条 Knowledge 与越权混淆 |
| 取消/删除中/未知状态 | DispatchUnconfirmed | 不误报接纳 |

`applyFetchedItem` 将未接纳错误交还 Batch/Streaming；Streaming latch 后不再接纳下一项，Checkpoint 不前进；已有安全接纳的早期分页可已提交。Batch 不提交包含未接纳项的新游标。尚有任务重试额度时，`recordUnacceptedSync` 保留 running、释放执行认领并提示“retrying from accepted checkpoint”；额度耗尽后写 failed，来源转 error（原 paused 保持 paused）。通用条目提示为“ingest_failed”，并非更新成功。自动重试不会解决缺少 staging 的能力问题；一个变化项可阻塞该来源后续条目，不能只理解为该篇新版本延迟。

## 为什么不能只接回 Replace 或复用 FirstCreate

`knowledge_replace.go` 的通用更新先保存新文件、更新 Knowledge，然后 `reparseKnowledge` 清理旧资源并异步入队；恢复列值不能复原已删除的索引。
`knowledge_process.go::reparseKnowledge` 调用 `cleanupKnowledgeResources`，后者调用 Retriever DeleteByKnowledgeIDList、DeleteChunksByKnowledgeID、提取图片删除和图谱删除；后续解析自身亦有同 KID 的清理分支。
Wiki reparse/ingest 有共享页面贡献变更，不能靠一次 Knowledge UPDATE 回滚。PostgreSQL 向量、BM25、Chunk、文件和共享 Wiki 不处于一个现成的发布事务中。

PersonalResource/Attempt/Manifest/Outbox/Fence 解决稳定映射、接纳与派发；当前 `bridgeFirstCreate` 事务直接 INSERT logical KID，依赖“尚无已发布知识”。Receipt 是完成证据，不是旧索引隔离与激活协议。将已存在来源导入此路径不能安全更新，且不能满足旧版本失败保护。

最小完整更新仍需：私有候选解析、与 Active 隔离的索引写入、权限/来源代次复核、稳定逻辑 KID 的原子发布、共享 Wiki 贡献与物理资源寿命管理，以及过期 Worker 不得删除 Active 的保证。现有首发没有这些已验收能力。本轮不借机引入 P1-3/3032，也不提供删旧重建补偿。

## 受影响范围与短期方案

本轮生产只读计数的 active 原始记录为 Wiki 2、Drive 7；过滤 `deleted_at IS NULL` 后是 Wiki 2、Drive 5。Repository `datasource_repo.go::FindActive` 明确过滤软删除和空调度。完整 ID、KB、调度、模式见 `evidence/production-audit.json`，克隆来源核对另见证据。软删除记录不能恢复为活跃来源，也不能与可运行来源合计。

生产一致性快照进一步确认：这 7 个实际来源涉及 3 个空间、354 条未软删除 Knowledge，全部开启向量+关键词，其中 6 个目标 KB 开启 Wiki。共享 Wiki 的发布问题是实际边界，不是仅假设。下面数量按来源元数据归属计算，不代表已通过新版本更新：

| 来源 ID | 类型 | 空间 | 已有 Knowledge | KB Wiki |
|---|---|---:|---:|---|
| 14433cdb-fddc-4d6c-a979-ef11c05b61bf | Wiki | 10002 | 16 | 开启 |
| be5a806a-8ff0-4352-9478-0d9063f7ca95 | Wiki | 10014 | 108 | 开启 |
| 3e361ce0-b06a-4e8f-ac92-dd9101097045 | Drive | 10005 | 0 | 开启 |
| 5c7bc873-0ba3-43c7-90a0-32c23e6bd529 | Drive | 10005 | 46 | 开启 |
| 867e5798-1c2b-4547-921d-d43132e3f275 | Drive | 10002 | 69 | 开启 |
| c9ec8746-accd-4e77-9615-c0d2e1cc33fd | Drive | 10005 | 2 | 开启 |
| eea7e329-708a-4c4b-bc77-b34a254db471 | Drive | 10005 | 113 | 关闭 |

另有已暂停、错误、软删除记录，完整 30 个飞书来源记录的状态、KB 映射和策略见 `evidence/cloned-source-inventory.json`；不会自动恢复它们。

若必须先上线，只能申请**明确接受暂停旧飞书来源增量**，不是宣称兼容已修复：

1. 业务负责人逐一确认尚可运行的来源/KB、最后同步时间、允许陈旧时长、监控与恢复负责人；同时覆盖 error/paused 来源日后恢复的限制。
2. 正式维护窗口批准后，停止相关新接纳，按产品 Pause 操作暂停调度，核对已领取/排队/重试中的来源任务并等待终态。**暂停调度不等于取消已领取任务**，不得清空队列。
3. `ManualSync` 允许 paused 来源，故必须另行控制/禁止这些来源的手动触发与恢复；Pause 不是完整接纳屏障。不得仅改数据库 status 后声称所有消费者已停止。
4. 上线后旧知识、KID、Chunk、索引、ACL、来源引用保留；暂停期间新增文档、修改、删除、撤权及子树变化均不会同步。源端撤权不会自动撤回 KnowHub 已入库副本，需独立权限应急流程。
5. 当前应用授权的单篇首次导入独立运行，不能用于覆盖旧来源或替代旧来源增量。
6. 恢复前验收安全更新；按保存的来源调度和游标恢复（不清空 cursor、不强制全量、不删除旧知识）。逐来源试点、核对新版本索引/引用后扩大。版本回滚需同时满足数据库兼容和权限条件，不能用重新打开危险 Replace 代替修复。

不得保留一个旧版 APP/Worker 去绕过新保护继续更新同一数据库和文件卷；这会重新引入旧协议物理删除及并发写入风险。

本轮未在生产暂停、改调度、取消任务或改数据。未取得暂停业务批准前，本门禁仍 BLOCKED。

## 验证含义

原恢复/游标 Race 专项实际 16 PASS / 0 FAIL / 0 SKIP，不能替代变化版本更新验收。
新增 `TestRC2LegacyCloneChangedRevisionPreservesPublishedKnowledge` 使用隔离生产快照的来源与 Repository，注入合成变化内容，检查拒绝、重复执行、KID/ACL 范围与 Chunk/索引保留、游标不前进。它验证“拒绝后保护”，明确不验证正常更新、真实新版本解析索引或物理文件存在性。
