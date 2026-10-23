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

最终质量检查、PR/CI、镜像与生产 RPC 结果见下文及同目录证据，早期原型与最终发布明确分开。

## 飞书独立只读观察

2026-10-23 14:40 北京时间观察为 6 active、2 paused、4 error，2 条 error 来源保持 HOLD。12:00 调度中一条来源进入 error/HOLD，不能人为恢复成 active；另有一条 12:00 起的任务仍显示 running、generation=6。另两条稍后启动的任务已 failed 且 claimed=false。具体脱敏引用、时间、Cursor 指纹和错误类别见独立 JSON；不能仅凭 error 总数推定原因、正常恢复或 Cursor 正确性。

没有修改来源状态、Cursor、任务或飞书开关。真实 Wiki 正文可读性、云端权限和异常任务最终收口并未在本轮性能测试中验收，继续独立跟踪。

## 回退与发布边界

本次仅允许 APP 更新，不重建前端、不迁移数据库、不更改用户权限或飞书设置。部署前发现 PR32 已更新生产，因此实际回退目标必须保留 PR32，不能退回缺少该修复的 PR31 镜像。保留 Schema3037 和所有来源拒绝事实；不得退回不安全旧 RC3。

## 最终源码与质量检查

PR #33 已合入 main `e3cc0d39c85f2ba564c3f2d402a2185eddc00361`，保留并行 PR32。最终代码与构建提交 `3479b97d461d0b3d5a0ac572c25c1f72941864e6` 的业务源码一致，已通过 Git diff 核对。

最终冻结源码指纹：`04c2b02b8a0ab35825f6b943288e970bec470ed8f585512882da68aaa0ff16c9`。归档 SHA256：`dc6b75802b56d1dec5081d1f7d45ad84b4a21ad8301477ed68bc2f761b3ace32`。这是文件 Manifest 指纹，不是 Git commit。

完整命令及 exit code 见 `quality-summary.json`。Repository、access、认证中间件、PostgreSQL、MCP race、Portal/模型/动态授权服务定向 race、路由、vet 和 APP 构建均 exit 0。最终计时关联标识的三个 Go 文件变更已补跑对应 race、vet、构建。真实 PG 测试配置了独立 disposable DSN，不能将未配置 DSN 的跳过运行算成通过。

GitHub lint、格式/测试/构建、APP 启动烟测、Portal RBAC/race、PostgreSQL 升级和 Linux MCP 兼容均成功。条件未启用的 resilience 检查标为 skipped。没有通过跳过授权断言使测试变绿。

`X-KnowHub-Perf-ID` 为服务端随机生成的响应关联标识；已有测试证明不采用客户端自报 ID，不包含主体或资源信息。

## 冷缓存的剩余问题

PR32 生产 APP 上首次 initialize 约 30 秒返回 403。日志显示 `enterprise_snapshot_batch.go:112` 读取 Chunk 版本元数据耗时 29862.504ms 后超时。第二次 initialize 23.118 秒成功，tools/list 0.554 秒；关键词 8.858 秒、向量 13.581 秒、Hybrid 16.887 秒，各 3 条合法命中。

本次维度检查优化不解决这条冷启动路径。保留首次 FAIL，不能改写为 PASS，也不能据此认定 PR31 缓存失效。累计 SQL 统计不是独占该请求的追踪。部署后真实耗时仍需单独记录，不以提高 30 秒上限或削减实时校验解决。

## 实际生产发布

2026-10-23 北京时间 15:54:08 开始 APP 替换，15:54:17 健康就绪，替换至就绪 9.29 秒。未部署前端、未执行迁移、未修改 Key scope 或飞书状态；未执行回退。该时间是发布步骤区间，不是逐请求测量的精确业务中断时间。

- 构建镜像 config ID：`sha256:c3514813e4049beca4ace718a8b1f9f5a96cae5667f2d1f32128489e27ffe56d`。
- 生产运行 image ID：`sha256:ad905a87fbfd4b9fe3621b04f60251af0c51061b4351b0e083ef657c361f50c6`。两端镜像存储类型不同，已核对 RootFS、运行配置及实际二进制，不能只比较不同类型的 ID。
- 实际二进制 SHA256：`a563115976e498bc61a26ef34912fe6b823f1d3780b8d345f7d24cf7ca283021`。
- 离线包 SHA256：`d3ce05afe37ccc2702e7a9c2c307f5c3b040998590f799c0b6125a71d0167f26`，2679485510 字节，打包机→Windows→生产全链校验一致。
- 回退目标：部署前实际 PR32 APP `sha256:bf72412285664df2cd30e04001e382da5658192de30df1af2e160d516bc9aed1`。脚本保存在生产本轮受控目录 `private/rollback-app-only.sh`，不回退 Schema。
- 生产证据目录：`/mnt/sata0/knowhub-mcp-authorized-sql-04c2b02b-20261023/`。个人 MCP 保持启用，飞书开关保持部署前状态，Schema3037/dirty=false，其他四个容器 ID 不变。

首次镜像导出误用了打包机空间紧张的系统盘，产生不完整归档，已保留失败日志并删除仅本轮无效归档。本轮构建文件逐文件 SHA256 核对后保留到 `/mnt/sata0/knowhub-mcp-authorized-sql-20261023-build/work`，原路径留兼容软链接。没有清理其他项目、有效备份或镜像；没有执行全局 prune。

## 部署后真实检索与耗时

普通用户 `test`、已有固定范围 Key `test01`，3 个原授权 KB，未扩权。大 KB 的同一查询、limit=3，前后各一次样本：

| 模式 | 更新前 RPC | 更新后 RPC | 更新后主要 SQL/模型阶段 | 结果 |
|---|---:|---:|---|---|
| 关键词 | 8.858s | 5.242s | BM25 3.329s | 3 条，ID/顺序一致 |
| 向量 | 13.581s | 9.417s | 维度 1.236s，向量 5.933s，Embedding 0.625s | 3 条，ID/顺序一致 |
| Hybrid | 16.887s | 12.061s | BM25 2.864s，维度 1.176s，向量 5.695s，Embedding 0.143s | 3 条，ID/顺序一致 |

每个阶段使用响应中的随机标识与生产日志精确关联。认证 3.8～7.4ms，范围解析 0.916～1.201s，Snapshot 累计 1.075～1.334s，合并和 JSON 编码低于 1ms；阶段嵌套，不能相加。Snapshot 每次请求仍有 124～129 次调用，作为后续只读分析线索，不能通过缓存最终授权消除。

关键词 SQL 本轮未优化，其耗时改善受缓存和环境影响，不能归因于维度检查改动。线上基线脚本误读了不存在的 `score` 字段，因此旧摘要仅能证明资源 ID 与顺序一致，不能作为数值分数相等证明。最终脚本已另采集真实 `rank_score` 摘要；排名语义保持的证据还包括候选/排序 SQL 未变与隔离 PG 等价测试。未编造未采集的旧分数。

小 KB 实测：关键词 1.574s（无命中，不算质量 PASS）、向量 1.160s、Hybrid 1.841s（各 3 条）。对应维度 SQL 7.9～8.6ms、向量 SQL 8.0～8.8ms。不是生产全量 KB 覆盖或性能压力验收。

本次重启后首个 initialize 13.398s 成功，暖 initialize 0.499s；tools/list 0.507s；列表 1.358s、文档列表 0.984s、正文读取 1.458s。冷启动仍偏慢，之前 30 秒失败保留为剩余风险。样本不足，不报告 P50/P95/P99，也不宣称稳定改善比例。

## 安全、兼容与观察结论

- 生产 PASS：原固定范围恰好 3 个 KB；合法文档读取；跨租户 KB/直接文档 ID 拒绝；已删除文档拒绝；写工具拒绝；80/8080 的匿名及非法 Key 均 401。
- 生产 PASS：Wiki 搜索/索引/缺失页面拒绝不写入；但没有可信可读页面，真实 Wiki 正文检索保持 NOT_TESTED。
- 生产 PASS：原 Python MCP initialize、31 工具发现。没有把它扩展为 Python 所有工具业务验收。
- 已撤销 Key：当前测试用户没有保留的可用历史撤销凭据，生产该项 NOT_TESTED；没有为凑 PASS 创建或撤销真实 Key。相关权限变化、并发与 PR31 缓存安全依赖本轮已通过的隔离 Repository/access/MCP/middleware 回归。
- 部署后约 7 分钟观察：APP/PG 健康、全部容器运行、restart=0；APP 约 233MiB、CPU 1.63%，PG 约 5.16GiB、CPU 0.08%，Redis 约 7.66MiB、CPU 0.85%，只是单次资源快照。APP 日志窗口未见 panic 或 deadline。保留新版本，没有声称多日稳定性。
- 飞书保持 6 active、2 paused、4 error；没有改状态。原 7 active 中脱敏来源 `2a692e...` 转为 error/HOLD，Cursor 自启用基线以来未推进。来源 `d0e1eb...` 的 running 对应固定错误模板 `Item admission failed; retrying from accepted checkpoint`；另两条 failed 对应 `Item admission failed; checkpoint retained`。这不证明底层云端错误已定位或最终任务已收口。失败来源的 Cursor 对账见独立证据；不能凭数量或某个字段变化推定错误推进。

## 剩余最小优化方向

1. 冷启动 Chunk 版本元数据加载：先对该 SQL 做独立计划与结果等价分析，保留 PR31 实时 epoch 和精确版本哈希；本轮不改缓存实现。
2. 大 KB 仍约 6 秒的精确授权向量 SQL：继续研究授权关系扫描/统计估算及安全查询形态，不使用全库未授权 TopK 后过滤，不发布没有稳定收益的原型。
3. BM25 约 3 秒与重复 Snapshot 查询：先积累低开销真实样本，再决定最小优化，不能削减撤权/输出复验。
4. Wiki 真实正文和飞书失败/重试终态继续独立跟踪，不混入本性能修复或清除 HOLD。
