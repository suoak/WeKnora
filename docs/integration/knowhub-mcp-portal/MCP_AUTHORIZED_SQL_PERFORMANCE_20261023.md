# 个人 Go MCP 授权检索 SQL 优化记录

## 范围与结论

基线为 main `9ddaf1c9dab4e6cc2bd978d811aa1c98b7b9b495`。本次仅优化维度兼容性检查并增加匿名分段耗时；PR31 缓存、身份、Key scope、授权解析、来源 HOLD/撤权、模型前及输出前复验均保留。不修改 Schema、前端、飞书代码或来源状态。

生产只读单次计划对比：旧维度 COUNT 9474.976ms，新 EXISTS 1360.250ms。前后顺序、缓存冷热及其他线上流量影响结果，不能将这一对样本视为稳定的整体 RPC 加速比例，也没有足够样本报告 P95/P99。

向量原型未取得收益，因此不发布：原向量 SQL 6009.896ms，对照相关子查询围栏原型 6192.565ms；另一组原查询 5669.985ms、延后正文投影原型 5788.252ms。最终保留原 BM25 和向量候选 SQL、完整授权候选集合、精确距离排序、阈值和 ID 消歧顺序。约 6 秒的向量 SQL 仍是后续瓶颈，不能宣称全部检索性能问题已解决。

## 源码与等价语义

- `internal/application/repository/retriever/postgres/repository.go`：`VectorRetrieve` 将 `COUNT(*) / COUNT FILTER` 改为同一 SQL、同一 MVCC 快照内的两个 EXISTS。任一兼容范围中的非匹配维度（含 NULL）拒绝；有有效文档但没有匹配向量仍拒绝。KB、可选文档范围、租户、有效 Chunk、当前正文及启用条件不变。
- `portal_scope.go`：新增 `portalDimensionCanonicalPredicate`，仅维度检查使用 `OFFSET 0` 阻止相关授权关系被展开为基数严重失真的大连接。不增加 LIMIT，不删授权条件，不改候选查询。
- `portal_sql_equivalence_test.go`：真实隔离 PostgreSQL/vector 下对比原 COUNT 和 EXISTS；覆盖 1024、混合 798/3584、NULL、0、负数及空集合。对公共/私人相关谓词比较 ID、距离、排名；覆盖跨 KB、陈旧正文、Portal 取消发布、禁用 Chunk、删除文档、停用租户、显式拒绝、来源撤权、删除/重建 Chunk。
- `internal/perftrace/` 及调用点：认证、范围解析、Snapshot、维度 SQL、BM25、向量 SQL、Embedding、合并、JSON 序列化。仅个人 MCP 请求记录固定字段，随机服务端请求标识；不记录 Key、用户/KB ID、正文、查询或模型输入。不默认开启 SQL 明文和 LLM_DEBUG。

分段时间有嵌套，**不能相加**。`resolve` 包含其内 Snapshot；Snapshot 调用可能重复。`vector_sql` 包含事务、扫描及结果映射尾部；`serialize` 仅工具结果 JSON 编码，不含网络传输。日志总时间包含路由处理，不代表客户端网络耗时。失败请求没有进入认证后 handler 时 `authenticated=false`。

## 证据与候选

证据目录：`evidence/mcp-authorized-sql-20261023/`。`production-plans.json` 和 `production-projection-plans.json` 是低频、只读、20 秒 SQL 上限的生产计划摘要，不执行 DDL/ANALYZE，不调用外部模型；查询向量是合成单位向量，用于 SQL 计划比较，不能作为真实模型检索质量验收。

原始冻结候选保留在 `E:/AIData/Codex/knowhub-mcp-authorized-sql-20261023/`；每个候选独立目录、完整 Manifest 和归档 SHA256，不覆盖早期候选。早期候选测试结果不自动继承为最终候选 PASS。

最终质量检查、PR/CI、镜像与生产 RPC 结果将在实际完成后追加；本记录当前不宣称已合并或部署。

## 飞书独立只读观察

2026-10-23 14:40 北京时间观察为 6 active、2 paused、4 error，2 条 error 来源保持 HOLD。12:00 调度中一条来源进入 error/HOLD，不能人为恢复成 active；另有一条 12:00 起的任务仍显示 running、generation=6。另两条稍后启动的任务已 failed 且 claimed=false。具体脱敏引用、时间、Cursor 指纹和错误类别见独立 JSON；不能仅凭 error 总数推定原因、正常恢复或 Cursor 正确性。

没有修改来源状态、Cursor、任务或飞书开关。真实 Wiki 正文可读性、云端权限和异常任务最终收口并未在本轮性能测试中验收，继续独立跟踪。

## 回退与发布边界

本次仅允许 APP 更新，保留当前已验收 PR31 APP；不重建前端、不迁移数据库、不更改用户权限或飞书设置。只有最终测试、CI 和审查通过后才合并及使用轻量化流程部署。若线上错误、授权或性能退化，回退至 PR31 APP，保留 Schema3037 和所有来源拒绝事实；不得退回不安全旧 RC3。
