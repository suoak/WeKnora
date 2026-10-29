# 飞书发布后派生回写设计与验证

生产基线：APP `962c7c888c99e8f13d64e5d2568fb4ff3ca0b171`，前端 `c969e95e95c0972e549eff5850cb1219c0546a55`。开发分支从 main `88e46547331acf28cf256617cdfd25ddd2c848e3` 创建；相关后端与迁移源码一致。本分支仅处理任务 A。

## 问题与字段边界

生产确认：通用 `UpdateKnowledgeColumns` 在已发布飞书资源上写摘要/profile，原触发器要求仍有效的 staging/ready attempt，故抛出 40001。过期 published attempt 是历史发布凭据，不应延长或复用。

| 字段/操作 | 处理方式 |
|---|---|
| description、profile、summary_status | 当前已发布版本上的受控派生回写 |
| metadata.derived | 同样受控；metadata 其余键必须完全不变 |
| datasource_id、external_id、来源修订、文件身份、正文、parse_status | 保持原子发布/原操作授权，不加入派生白名单 |
| Chunk、Embedding、摘要索引 Chunk | 保持原子发布保护；派生回写不允许更新 |
| updated_at | 随本次合法派生写更新，不作为授权凭据 |

受保护文档的摘要存入 description/profile。新增或刷新摘要索引内容不能借本次派生权限执行，须随新的原子发布实现；普通非飞书文档保持现有摘要 Chunk 行为。

## 调用链审计

- `knowledge_process.go:saveSummaryState`：异步摘要、刷新、失败/重试状态统一走版本绑定 context。
- `knowledge_summary_refresh.go:enqueueSummaryRefresh`、`knowledge_post_process.go:enqueueSummaryGenerationTask`：任务保存 SourceVersion；旧载荷和已被新版本替代的任务在模型调用前终止。
- `datasource_service.go:ingestItem`：来源身份 metadata 属于原子发布信息。飞书安全分支已有 StageFeishuLegacyItem；其他连接器的 URL 创建后 metadata 附加保留。通用附加若碰到已纳管资源，明确拒绝，不转为派生授权。
- `knowledge_create.go` 的创建/处理状态、`knowledge_process.go` 的删除失败收尾、`knowledge_replace.go` 的来源列替换及回退：均不属于派生白名单，不能通过通用字段更新获得已纳管资源写权。
- `knowledgeRepository.UpdateKnowledgeColumn` 合流到 `UpdateKnowledgeColumns`；纳管资源没有受控 context 时返回终止错误。
- Redis 与 Lite 任务执行器均将明确的来源写保护/派生版本错误视为不可重试；普通数据库 serialization failure 和模型临时错误保留原有有界重试。

## 受控协议

1. 任务入队记录资源/generation/current attempt/epoch incarnation/generation。它是版本标识，不是凭据。
2. 开始摘要前核对入队版本；通过现有 `bindFeishuSource`、`batchContext` 验证持久来源 owner 的当前成员、连接、配置、批准和来源权限。缺少可验证来源引用时拒绝。
3. 捕获文档行摘要和访问 epoch。网络读保护在事务外执行；本地授权提交保护在持锁事务内重查。
4. 按 source → 来源 identity advisory lock → KID advisory lock → resource → knowledge 顺序锁定，要求来源 active、未删除、可运行，资源 active，current/active attempt 为当前 generation 的 published 记录。无需且禁止续租旧 attempt。
5. 对比文档行摘要及 epoch；事务中插入一次性 `feishu_derived_write_grants`，随后更新白名单字段。
6. 数据库触发器独立检查来源/发布状态、OLD 版本、允许字段、完整未改的非派生字段及 metadata 身份键，消费同事务凭证；随后原有 epoch 触发器仍执行。
7. 返回新版本给此任务后续状态写入。其他并发任务的旧快照失败，不能覆盖较新结果。

拒绝日志只含知识 ID、结果与原因码；派生更新的 GORM SQL 日志关闭，避免将 profile/metadata 值写入错误日志。没有打印 nonce、token 或配置密文。

## 迁移与回滚

- PostgreSQL 3043 新增事务内凭证表并给原写保护函数增加严格的 UPDATE 派生分支。无凭证的旧程序继续执行原保护。
- 不修改任何来源状态、历史 generation/lease/nonce、用户权限或 Key。
- 新程序须在迁移后启用。可先回滚应用，保留兼容新表；若回滚迁移，先停止/排空派生写任务，再恢复原触发器函数并移除凭证表。之后摘要回写再次被原保护拒绝，内容保护保留。
- SQLite 不模拟 PostgreSQL 事务能力，保留对已纳管飞书资源的原拒绝策略；普通摘要不受影响。

## 验证与发布门槛

真实隔离 PostgreSQL 使用正式 migrator、epoch 与来源触发器验证：已 published/过期旧租约的合法摘要/profile/派生 metadata；身份字段及正文/Chunk 拒绝；同版本并发只允许一方成功；来源撤销、generation 变化、旧任务拒绝；事务凭证不残留；迁移 down/up 后原保护和合法派生路径。

同时运行既有 FeishuLegacyAtomicPostgres 原子发布与旧 worker 变更拒绝回归、摘要状态与取消回归、Redis/Lite 任务终止判定。测试使用独立新数据库，未连接生产库运行测试。

2026-10-29 第一轮候选快照 `8bd5c524d68dd5a8fb3e2e2c44e6226e5bcd885df5e7bc81fb68f34ab42dd6e9`：选取的仓储、Service、container、router、dead-letter 回归全部通过，测试进程 exit=0，61 个通过事件（包括子测试和 package 结果）。该计数不等于完整项目测试数。包含 profile、并发快照、撤销、旧 generation、任务终止及迁移 down/up 测试；该快照的选定并发/任务测试另通过 Go race 检测。

复查修订将队列版本限定为已发布内容身份，避免任务自身的摘要状态更新使合法重试失效；行/epoch 乐观快照仍保护每次回写。迁移治理映射和全链路夹具的 latest 断言同步至 3043，历史升级及内容/权限断言不删减。缺失 epoch、资源与来源 KB 不一致继续拒绝。

外部测试账号的真实 HTTP 操作、真实飞书授权生命周期验收以及发布评审仍应作为独立门槛记录，不以仓储测试代替。此分支不包含 MCP 授权范围重构，不自动合并或上线。

## 新发现：d14eb0c0… 文档停留 pending

2026-10-29 只读核查：该库 30 份未删除文档中 28 份 pending、2 份 completed；pending 均为 feishu_personal，Chunk=0。已有来源处于 hold。document:process 的死信明确为 `Feishu source access requires verification`，部分失败 4 次。失败回调重新读取文档也被同一读保护拒绝，因此没有收尾为 failed。另有 24 条 pending 操作记录。

它发生在内容处理前，不能靠本次发布后派生回写修复解除。应独立设计来源受限时的任务准入和状态解释；不能直接把 hold 改为 active、重投生产任务或跳过读保护。本轮仅调查该现象，没有改动这些生产记录。
