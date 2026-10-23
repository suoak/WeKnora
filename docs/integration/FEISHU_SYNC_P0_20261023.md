# 飞书同步可靠性 P0：审计、修复与验证

基线：origin/main e3cc0d39c。只在独立工作树开发，不访问或修改生产，不触发生产同步。

## 修改前审计与方案

- Embedding：generic 未声明批量上限；runtime.resolveEmbeddings 没有处理 extra_config 的批量设置；protocol.BatchEmbed 只按已解析上限拆批；feishu_legacy_staging.go 直接提交整份文档分块。方案：增加可验证的每模型上限设置，并对飞书安全暂存的直接调用增加默认 20 的串行边界；任一批失败整体返回错误，发布仍通过既有原子协议。
- 终态：datasource_execution.go 已有 generation/heartbeat/defer，但进程被杀不会运行 defer；datasource_ingest_error.go 为重试主动保留 running；router/task.go 的死信回调只处理 knowledge，不处理 datasource；知识 housekeeping 没有同步对账。故 Redis 终态不保证数据库终态。生产证据只能证明归档与 running 不一致，不能唯一还原当时是否被杀、数据库写失败或哪次重试终止。方案：保留既有正常收尾，增加读取真实队列的周期性同步对账，CAS 比较 generation、claim 和更新时间，失效旧执行且不更新内容、权限、游标或最近成功时间。
- 调度：FindActive、Scheduler.Start/AddOrUpdate/triggerSync 把执行健康 status=active 同时作为启用条件，error 在重启及 tick 时均被跳过。方案：明确 AutoSyncEnabled 为现有状态的兼容性解释（active/error 允许，paused/deleted 禁止，空 cron 禁止），不增加默认字段或回填历史数据；根据连续失败日志实施有上限退避，不改用户 cron。

边界：不修父子分块、源修订冲突、DNS 和飞书授权；不关闭 SSRF、租约、权限、版本或原子发布检查。

## 验证记录

待实现后填写实际执行结果，不将跳过的 PostgreSQL 测试计为通过。
