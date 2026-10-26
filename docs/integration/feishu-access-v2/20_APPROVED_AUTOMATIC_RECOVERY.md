# 两个既有来源的已批准隔离恢复与自然同步验收

2026-10-26，分支 `feature/knowhub-feishu-access-v2`。本次用户明确批准：**“批准仅按此方案恢复两个来源”**。批准对应报告 19 的原项目、原 namespace、`test001` 的两个既有 Sheet/Bitable 来源；不涉及生产、main、PR #50 或其他来源。

部署产品源码仍为 `2a7f651e6ed078fcf5113905a9358a82084d7d17`，五组已通过门禁不变。恢复工具提交 `4dc476fc401f228acb34161ead281b8e0d09bdda`；只读监控及来源快照增强提交 `479ecbf66a54ab8ca667b00526698f1240d83b7c`。未修改产品的 `internal/`、`frontend/` 或数据库迁移。

## 批准及执行记录

恢复项目：`knowhub-feishu-v2-d6e99d894d3a`；namespace：`legacy-feishu`。

批准关联：`user:call_085aca217ce54805be1d8372feec744b:approved_two_sources`；计划 ID：`8a959aaecf5c49d3813fffe4f037d711`。服务器私有状态文件记录原容器身份、配置摘要、来源配置与索引基线，所有阶段均检查计划和前一阶段状态；公开证据不含凭据、正文或文档链接。

| 北京时间 | 操作与结果 |
| --- | --- |
| 15:53 | 只读复核原 trip=`automatic_permit_consumer_lost`、两个 live controller 与两个 active sync 消费者，个人 access/refresh 有效 |
| 15:59 | 独立 Redis 自测通过；保存批准范围和索引基线，sync 队列 active/pending/scheduled/retry 全为 0 |
| 16:00:02 | APP/worker2 正常停止，确认控制成员、已接纳成员和活跃 lease 清空，旧 Asynq 心跳退出；未手动删除旧成员 |
| 16:00:24 | Lua 条件比较确认原锁值、队列暂停、空成员/lease/任务及成员版本号，写恢复审计后仅解除本次 trip；队列仍暂停，无 permit |
| 16:00:52 | 两进程重启，生成新 incarnation；没有给已封锁的旧 Guard 增加重新打开入口 |
| 16:02:25 | 两 controller 指纹一致、lease 有效，两个真实 Asynq 消费者 active；无新 trip，permit 持续续期 |
| 16:02:44 | 再次检查新进程健康、成员、lease、permit、待处理任务范围及队列状态，写审计后恢复原 sync 队列 |
| 16:06、16:10 | 无新 trip，许可持续有效；两来源 Cursor、Knowledge 和索引基线保留，等待 16:15 自然周期 |

执行证据：[批准快照](evidence/recovery-approved-plan-20261026.json)、[正常退出](evidence/recovery-quiesced-20261026.json)、[条件解除停止](evidence/recovery-reset-20261026.json)、[新实例拓扑](evidence/recovery-topology-20261026.json)、[队列恢复](evidence/recovery-resumed-20261026.json)。恢复事件保存在原 Redis 控制审计流，不伪造外部告警已送达。

恢复脚本只接受固定项目和固定产品 SHA，校验同一两个容器、配置文件摘要、来源 allowlist、个人 OAuth 类型以及来源 active/非 hold/revoked 状态。没有改变来源配置、审批、限流预算、namespace、Cursor、Knowledge ID、索引或原 cron。条件变化时拒绝，不循环强行解除新停止。

## 恢复工具验证与基线

[独立 Redis 自测](evidence/recovery-selftest-20261026.json)使用无网络、无挂载数据、无公开端口的临时容器；结束后仅删除该容器。验证停止原因变化、成员未退出、lease 未退出、permit 缺失、停止重现、lease 失效和 incarnation 变化都拒绝；成功 reset 保留队列暂停，成功 resume 保留预算测试值。未对现有 Redis 运行清库测试。

| 资源 | 来源指纹 | Knowledge 指纹 | Chunk | 向量（1024 维） | 向量 ID 集合摘要 |
| --- | --- | --- | --- | --- | --- |
| Sheet | 6d1698a6e0d9 | d0c4602a75ca | 24 | 22 | 7c38a4c1b9f272abeabc541ecf97154f |
| Bitable | 1dcf458346ba | f48df03f6510 | 30 | 27 | e896eda14f85620f9b2bce70984cfb42 |

[Cursor 与索引基线](evidence/recovery-index-baseline-20261026.json)同时记录 reference revision 摘要，每个来源仅包含用户原选的 1 个对象；没有扩展到文件夹、Wiki 空间或用户其他文档。[启动检查](evidence/recovery-smoke-20261026.json)8 项通过。

## 自然同步结果

原 cron 保持 `0 */15 * * * *`。16:15 自然周期已执行，没有调用手动触发 API、没有修改任务时间或周期。[自然执行回执](evidence/recovery-natural-first-20261026.json)包含真实数据库终态、飞书 HTTP/code、两个进程的入队和执行日志。

| 来源 | Sync log 指纹 | 北京时间开始 → 完成 | 耗时 | 结果 |
| --- | --- | --- | --- | --- |
| Sheet | a5ce4281c7a7 | 16:15:00.026 → 16:15:08.433 | 8.406 秒 | success，新增/更新/删除/失败均 0 |
| Bitable | 16397dbdc3f0 | 16:15:00.025 → 16:15:12.987 | 12.961 秒 | success，新增/更新/删除/失败均 0 |

APP 为 Bitable 入队并执行 Sheet，worker2 为 Sheet 入队并执行 Bitable；另一调度器的同来源重复 tick 被抑制，每个来源只生成一条执行记录。两者 `execution_generation=1`，完成后释放 claim；`execution_claimed=false` 是终态释放，不表示没有执行。元数据 `get_wiki_node` / `resolve_personal_resource` 均 HTTP 200、code 0，两边均有 `worker_completed`。

每个来源仍只检查原选定的 1 个对象，reference revision 摘要与恢复前相同。Cursor 的最后扫描时间正常推进至 16:15，未清空。两个 Knowledge ID、更新时间、Chunk 数、向量数和向量 ID 集合摘要均与上表基线完全一致，证明无变化没有重复入库。`items_total=0` 表示增量扫描没有待导入的变更对象，不能用于声称重新导出或解析了正文。

16:21:58 复核：APP/worker2 健康，trip 不存在、sync 未暂停、permit 持续续期，active/pending/scheduled/retry 均 0。自然无变化同步验收通过；真实改值后的版本发布及新值检索尚未通过，不与本项混淆。

[自然同步后真实 MCP](evidence/recovery-natural-mcp-20261026.json)：Go keyword/semantic/hybrid 21 项通过，Python 实际适配器 hybrid 7 项通过，包含表头和行值；另 1 个库保持凭据范围隔离。Portal 本轮没有使用用户网页会话重验，沿用此前用户命中确认，不冒称本轮新值验证通过。

后续受控改值仍须使用有权源账号，并遵守新版本的发布授权。当前批准是恢复两个既有来源，不能被解释为所有未来版本的隐式共享批准。Portal/Go/Python MCP 新值验证必须建立在真实安全发布完成之后。

新增只读 Docx 的 HTTP 403/code 1069902 及 Scope 核查结论保持报告 19 所述，未以此次控制恢复绕过源端导出拒绝。
