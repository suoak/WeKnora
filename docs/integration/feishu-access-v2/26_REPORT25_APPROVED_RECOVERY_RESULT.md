# 第 25 份方案获批后的实际恢复结果

2026-10-26。结论：**已按批准执行受控恢复，但业务验收未通过。新的停止锁已经触发并保留，没有再次清锁；两个来源目前均为 hold，尚未发布用户已修改的新值。**

## 1. 批准及执行范围

本轮明确批准来自 `call_92c64f81683044a8802044a6f9f7d878` 的用户回复“批准仅按第 25 份方案恢复两个来源”。仅操作隔离项目 `knowhub-feishu-v2-d6e99d894d3a`、`test` → `test001` 的两个既有来源，运行时保持 `2607fbec7b7ee63ee66d83fb0e199f02945074fb`。

原第 19 份批准未复用；没有修改生产、来源白名单、速率预算、namespace、原版本批准或知识库可见范围。没有把该批准当作后续新停止事件或另一轮对账的许可。

## 2. 实际执行证据

时间均为北京时间，时间线来自恢复回执和 Redis 审计流，而不是将不同时区的历史 SyncLog 时间字段直接相减。

| 阶段 | 时间 / 结果 | 保护状态 |
| --- | --- | --- |
| 复核与准备 | 22:54–22:55，两个原来源、原镜像、配置 / Cursor / 索引摘要一致 | 原 membership deadline trip、暂停队列和 Bitable hold 保留 |
| 正常停机 | 22:55:48，APP / worker2 已退出，消费者及租约完成退出检查 | 保留两个 canceled 执行的旧投递，不清队列、不改日志 |
| 新备份及恢复验证 | 完成停机后的 pg_dump，并恢复到本项目专用验证数据库；8 个业务表计数一致 | dump SHA256 `489bb83a0adac747077babab26660e71e69c308d4109b38f47f2c7e5d3d2bb41`；未将备份覆盖运行库 |
| 获批策略登记 | 22:56:58，同事务写入两来源原受众持续策略及 Bitable 单次日志 | 持续策略到 10-27 22:56:58；单次授权到 10-26 23:11:58；不改原批准、Cursor 或正式知识 |
| 精确复位旧事件 | 22:57:12.899，Lua CAS 成功 | 只清本次 membership deadline trip；队列仍暂停、hold 仍保留 |
| 正常重启 | 22:57:43，随后两个进程健康、消费者 / incarnation / lease / permit 检查通过 | 沿用原镜像与配置 |
| 单次任务入队 | Bitable 对账日志 `3436921b3690`，固定任务 ID | 队列仍暂停；重复 operator 请求不新建日志 |
| 受控继续 | 22:58:11.941，队列列表、成员和 permit CAS 通过 | 两个旧终态投递由 Worker 正常确认，未重做内容读取 |
| Bitable 实际结果 | 22:58:12.170，generation=1，failed，items_total=0 | 在正文获取前拒绝；未发布，hold 保留 |
| Sheet 自然调度 | 23:00:00，日志 `d82e7cb7cb6d`，generation=1；23:00:00.154 failed，items_total=0 | 确实自然入队及领取，但未读取正文；随后进入 hold |
| 新停止事件 | 23:00:22.347，`audit_unavailable` | 队列重新暂停、permit 关闭；没有再次解除 |

[批准与各阶段回执](evidence/report25-approved-execution-20261026.json) · [限定策略回执](evidence/report25-explicit-policy-enrollment-20261026.json)。

单次 Bitable 日志已经终态，短期授权也已到期；没有重置该日志、换日志重试或延长许可。持续策略按获批的原期限保留作审计，不会覆盖旧身份或 source hold。

## 3. 两篇文档读取前失败的已确认阻塞

只读比对确认：两来源的 KnowHub owner、应用及实际飞书 OpenID 均与当前连接一致，但持久化 `credential_reference.connection_id` 与当前授权计算值不同。

| 来源 | 同 owner / App / 飞书账号 | 持久化连接摘要 | 当前连接摘要 | 是否匹配 |
| --- | --- | --- | --- | --- |
| Sheet `6d1698a6e0d9` | 是 | `978b52db04c1` | `be8a187c88fa` | 否 |
| Bitable `1dcf458346ba` | 是 | `978b52db04c1` | `be8a187c88fa` | 否 |

`ResolveCredentialReference` 用个人身份、用户、App、OpenID 和 GrantEpoch 共同计算连接 ID。先前补充授权更新了个人授权代际，来源仍绑定旧连接；传入旧 ID 时会在提供内容身份之前拒绝。普通 Access Token 刷新不会改变这一绑定，不能将两者混为一谈。

这项事实足以证明当前来源绑定不再有效。它不是源文档的 view/export/copy 判断，也不是导出接口失败；本轮两个执行均未走到正文读取。不能要求用户申请编辑权限、再次重复 OAuth，或借持续发布策略偷偷迁移旧授权。

还发现登记前漏检：原 `ApproveSourceUpdates` / `ApproveSourceReconciliation` 绑定了当前 GrantEpoch，却没有验证来源保存的 connection ID 是否属于该当前授权，因此登记能够成功，Worker 随后才拒绝。候选修复已将该检查前移。

## 4. 新审计停止的确定事实及归因边界

新记录精确为：

| 首次事件 | 停止阶段 | 整轮耗时 | 结果 |
| --- | --- | --- | --- |
| 23:00:22.347 | audit | 712ms | `audit_unavailable`，fenced=1、queue_paused=1 |
| 23:00:22.351 | audit | 664ms | 同上；后续 permit_tripped 是跟随停止 |

这两个数字是整轮 Step 耗时，不是独立的 XADD 耗时。它们超过 500ms 的 Step 预算，且停止阶段明确为 audit；上一轮上下文修复让该事件不再被误归为 membership GET。该分类修复有效，但**没有解决导致整轮超预算的底层延迟，自动更新稳定性仍不合格**。

后续只读采样 GET p50=0.121ms / p95=0.272ms / max=0.452ms；AOF delayed_fsync 累计 85，当前写状态正常，没有保留到可定位这次事件的 LATENCY / slowlog。Redis 日志里的下一次后台保存从 23:00:36 才开始，晚于停止，不能拿它解释 23:00:22 的触发。当前证据不足以断定磁盘 fsync、网络、连接池等待或进程调度哪一项是底层根因。

没有增加 500ms 预算、permit TTL 或重试上限，没有关闭审计 / 限流，也没有通过换 namespace 绕过该停止。后续需要补齐每阶段起止、审计写入和调度等待的证据，再形成可验证修复；当前不适合继续反复清锁。

[新停止审计与依赖采样](evidence/report25-new-audit-stop-20261026.json)。

## 5. 数据与检索最终状态

| 来源 | 当前状态 | 原 Knowledge 摘要 | Chunk / 向量 | 向量 ID 摘要 |
| --- | --- | --- | --- | --- |
| Sheet | error / hold | `d0c4602a75ca` | 24 / 22，1024维 | `7c38a4c1b9f272abeabc541ecf97154f` |
| Bitable | error / hold | `f48df03f6510` | 30 / 27，1024维 | `e896eda14f85620f9b2bce70984cfb42` |

原知识、Chunk、向量 ID、源配置和 Cursor 摘要与恢复前一致，没有重复 Knowledge、移动 KB 或重建旧索引。停止后的 active / pending / scheduled / retry 均为 0。用户此前已改的字段尚未进入正式发布版本。

原正式数据仍持久保留，但 hold 会限制读取范围；不能把“旧数据存在”写成“Portal/MCP 当前可读”。本轮没有新值 Portal / Go / Python MCP 检索成功证据，不要求用户重复提交链接或以重新搜索替代后台修复。

[最终状态及同账号绑定比对](evidence/report25-final-protected-state-20261026.json)。

## 6. 已提交但尚未部署的候选修复

候选 SHA：`020a2fb068e371783c402a2c104039d6c3c837a0`，包含基础修复 `13abcebc6ff46ba4aa520bfac70f44a20b48ad7b`。现场应用仍是 `2607fbec7b7ee63ee66d83fb0e199f02945074fb`，没有将候选版当作已部署版本。

- 在来源绑定、持续策略和单次对账登记前验证真实当前连接；旧连接与当前 GrantEpoch 不匹配时提前拒绝，不创建可用策略或对账日志。
- 新内部原因 `source_identity_rebind_required`，与 Token 过期、未连接和文档权限分开。失败提示为“飞书授权已更新，请联系管理员更新此来源的授权绑定。”不新增常驻说明，不诱导重复 OAuth。
- 普通 Token 刷新仍可沿用同一来源绑定；重新授权不自动迁移旧来源，也不解除 paused / hold / revoked。
- 回归覆盖旧绑定不能登记恢复授权且策略 / 日志均不产生；原持续策略的终态、身份代际和受众保护继续保留。

定向 Linux 编译及四项服务测试通过且无 skip：完整首授范围、错误分类、诊断归属、刷新与重新授权的区别；15 项离线 operator 用例通过。完整 CI 已启动，结果以单独回执为准，尚未宣布五组全部通过。[候选测试回执](evidence/report25-binding-candidate-tests-20261026.json)。

本轮操作工具还补齐了受控停机后的备份 / 恢复核验，以及停止容器的 operator 检查；只读诊断明确显示 `STOPPED_AFTER_RESUME`，避免把历史 RESUMED 阶段当作当前同步正常。均不改变运行时安全门禁。

## 7. 后续门禁

1. 查明审计阶段超预算的底层延迟，保持当前审计和停止阈值，完成针对性复现及回归。
2. 为这两个来源设计并验证一次明确管理员授权的同账号绑定迁移。该操作会涉及来源身份绑定和策略摘要，**本轮未执行**；不能把用户本轮的“保留原配置、单次对账”批准扩展成自动迁移或另一轮恢复。
3. 候选完整回归通过后再更新隔离站；涉及新 `audit_unavailable` 停止锁、两个 hold 及重新发行对账执行时，先提交新的具体方案并获得明确批准。
4. 真实改值发布、原 Knowledge ID、版本 / Chunk / 向量 / BM25、自然无变化同步及 Portal / Go / Python MCP 最新值全部通过前，不进入最终发布。

本轮恢复没有通过。新锁保持关闭，未合并 main、未部署生产、未改 PR #50 或受保护来源。
