# RC3 旧 Wiki/Drive 受控暂停审批与操作卡

状态：**待业务负责人批准；未在生产执行暂停或配置切换。**暂停不等于已修复安全更新。保护代码仅在隔离候选中实现，须通过测试和维护窗口核验后才能用于发布。

## 审批对象与影响

下表依据生产只读审计，不包含两条软删除 Drive。KB 创建者及空间 owner/admin ID 见 evidence/physical-preflight.json，仅是协调线索，**不能视为业务暂停批准人**。业务负责人、暂停起止时间、允许陈旧时长、撤权应急人、未来恢复负责人均待逐来源签认。时间来自数据库原值，没有擅自假设其 timezone。

| Source ID | Type | KB ID | Tenant | Knowledge | Last sync (DB) | Cron | Business owner |
|---|---|---|---:|---:|---|---|---|
|14433cdb-fddc-4d6c-a979-ef11c05b61bf|feishu|54b0a305-1ab4-4fab-9937-778fd71c3a74|10002|16|2026-10-19T18:00:19.127664|0 0 2 * * *|PENDING_APPROVAL|
|3e361ce0-b06a-4e8f-ac92-dd9101097045|feishu_drive|b93b9e1f-e75b-41f5-940a-ad7de5105c6f|10005|0|2026-10-19T22:00:01.70479|0 0 */6 * * *|PENDING_APPROVAL|
|5c7bc873-0ba3-43c7-90a0-32c23e6bd529|feishu_drive|6dcae886-fc03-446d-9523-ee6f4e5009c9|10005|46|2026-10-19T22:00:02.400424|0 0 */6 * * *|PENDING_APPROVAL|
|867e5798-1c2b-4547-921d-d43132e3f275|feishu_drive|8ca41885-69e1-4d43-8bfd-ffc8670900fe|10002|69|2026-10-10T06:17:13.433587|0 0 2 * * *|PENDING_APPROVAL|
|be5a806a-8ff0-4352-9478-0d9063f7ca95|feishu|a01317cc-fcda-4dfd-b9c5-2086d7c0cf7a|10014|108|2026-10-19T22:03:06.915461|0 0 */6 * * *|PENDING_APPROVAL|
|c9ec8746-accd-4e77-9615-c0d2e1cc33fd|feishu_drive|09b0c5d5-0196-4131-a72b-402eee1c50d4|10005|2|2026-10-19T22:00:00.956121|0 0 */6 * * *|PENDING_APPROVAL|
|eea7e329-708a-4c4b-bc77-b34a254db471|feishu_drive|d14eb0c0-1ade-46f9-890e-84fa823b335d|10005|113|2026-10-12T06:04:09.838192|0 0 */6 * * *|PENDING_APPROVAL|

7 个来源、354 条未软删除 Knowledge。暂停期间不同步新增、修改、重命名、删除、子树变化或飞书撤权。已入库副本继续按 KnowHub KB ACL 可见，飞书源端撤权不会自动撤回它；敏感内容需另行经批准收回 KB 权限或处理相关知识。源端文档作者、KB 管理者和空间负责人需明确接受该风险，不能仅由发布人员签收。

暂停方案不删除原 Knowledge、稳定 KID、Chunk、向量、Wiki 页面、共享关系或权限。首发仍允许独立 application_grant 单篇首次导入，不允许借此覆盖旧来源。

## 最小保护及边界

新增 `FEISHU_LEGACY_SYNC_SUSPENDED=true`，默认未配置保持原行为；非空非法值按暂停处理。覆盖全部 feishu/feishu_drive/lark/lark_drive 连接器，包括现有 error/paused 来源和新建入口，防止通过恢复、类型修改或重新建源绕过；该范围比7个active来源更广，业务批准必须明确知晓。不会阻断 feishu_personal/application_grant、RSS 等其他连接器。

- Scheduler 启动/注册不创建旧来源 cron，旧回调再次检查，不创建 SyncLog/队列任务。
- 服务端 Create/Update/Resume/ManualSync 拒绝，保留明确原因；仅隐藏前端按钮不构成保护。
- 已排队、重试、scheduled 或 active 待处理的旧协议任务进入**新版** Worker 后，先取得原执行 fence，再在任何 connector/KB写入前终态拒绝。SyncLog=failed、来源=paused，保存原因，释放认领，原游标、最后成功时间和结果保留；队列返回 SkipRetry，进入 Asynq 归档流程，不冒充同步成功。
- 重复投递不再执行已终态任务。归档有其既有容量/保留策略，不承诺永久保留；恢复以受控保存的源状态/游标重新发起正常任务，不依赖归档永久存在。禁止直接清空队列。
- DB读取或终态确认失败不能虚报暂停完成；等待重试/人工核对，发布保持阻塞。

此屏障不是旧二进制物理写入围栏，不能阻止已经进入 connector 的旧进程。不得热改一个环境变量后认定完成排空，不得新旧 Worker 混写同一库/文件卷。

## 正式维护窗口执行顺序（全部尚未执行）

1. 获得逐来源业务批准、生产发布批准及维护窗口；记录镜像/Compose/全部APP与Worker实例、自动重启/外部拉起任务。仅停止一个容器不等于所有消费者退出。
2. 备份 DB、文件卷、配置、原镜像、Python MCP 原镜像；确认通过物理恢复检索门禁。将各来源完整 cursor/status/schedule/last_sync_at/result、全部相关非终态 SyncLog、执行generation/claim和队列taskID保存到受限文件并校验摘要；完整游标/配置不得进Git或普通日志。
3. 停止新同步接纳，使用现有授权管理接口逐源 Pause；停止全部旧 APP/Worker，阻止自动重启。核对进程退出、远端在途请求终态；未知外部操作不得靠租约到期推定安全。不要让旧版本继续执行先删后建。
4. 在全部候选 APP/Worker 的受控 Compose 配置加入暂停开关，统一部署候选；核对运行配置和实例清单。数据库/文件卷/Redis保持原映射，Python MCP 保留生产镜像。
5. 对 queue=sync 的 pending/active/retry/scheduled/archived 分页扫描，按来源和SyncLog标识对账；不得对每条任务扫描全部队列。只有新版 Worker 可处理既有任务；它们必须终态失败/归档并保留原因，或在已批准的隔离队列方案下保留，不直接清空。
6. 证明没有旧 Worker、相关 running/claimed 无未解释残留、新的旧来源调度/手动/重放都不能抓取。检查来源状态/游标摘要、原KID/Chunk/向量/Wiki/共享/权限前后相同。任何异常停止扩大开放并按备份方案回滚。

当前审计 running_logs=0/claimed_logs=0 只是一个时点，**不是已领取任务/所有队列/外部副作用全部排空的证明**。上线窗口必须重查并记录 Redis 与实际进程证据。

## 恢复与回滚

先通过完整安全增量更新的独立验收，再逐来源审批恢复。不得仅将开关改false、改active或回退危险旧APP以恢复正常更新。使用保存的 cursor 与调度，先做单源小范围更新/失败保旧/重复/KID/索引/权限/引用验证，确认结果后恢复其他来源。不清cursor、不强制全量、不重建原成功Knowledge。

只需回滚应用时仍须保持已批准的暂停保护；RC24没有这个开关，回到RC24时不得重新启用旧自动/手动同步。必须保持旧同步入口停止，并执行单独批准的隔离/停机流程；不能用“配置开关仍为true”证明RC24受到保护。

如果业务负责人不能接受暂停及源端撤权延迟，必须转向安全候选解析/索引隔离/原子切换及共享Wiki保护开发，超出RC3最小首发；没有已测工期，不能承诺当前发布窗口内完成。
