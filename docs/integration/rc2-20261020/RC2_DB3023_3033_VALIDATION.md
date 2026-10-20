# RC2：生产 DB3023 → 3033 隔离验证

最终实测结果由 `evidence/clone-result.json`、`clone-app-startup.json`、`clone-rollback-startup.json`、`strict-validation.json`、`serial-reindex.json` 和本报告末尾结果节给出。还原过程不能用现有测试库 DB3033 替代。

## 生产只读审计和授权

生产 `10.51.134.114` / `weknora`，RC24 / main `854f3b956ae3600e12a3ffd2fc40d4fe624ced74`，DB3023 clean。审计时数据库约 23.08 GB，根分区剩余约 18.61 GB。未在生产落盘备份，未执行迁移、DML、服务重启、角色修改或 ForceVersion。

用户在本轮明确批准将一致性备份通过受控 SSH 保存到打包机受限目录，并恢复到无业务网络的独立 PostgreSQL。生产只执行只读事务、导出快照及 pg_dump；保留 snapshot 的同一事务核对数据量。禁止业务正文、凭据、Token 和完整备份进入 Git/普通日志。

备份保存在打包机 `/home/aqyfzx/ai/knowhub-rc2-20261020/private/production-3023-v4.dump`，父目录 0700、文件由 umask 077 创建。保留策略待负责人另行确认，不自动删除。克隆不连接生产或测试业务数据库、Redis、文件卷、飞书、SID、模型端点。

备份：6,239,732,271 bytes；SHA256 `38ab3c9b648037f1a7c4da37e1425882d59d412574615b54b75097399fcfe499`；pg_dump/传输约 238 秒。
一致性快照：44 users、4,060 knowledges、4,961,022 chunks、1,386,916 embeddings、71,652 wiki_pages、32 data_sources。不是先前 pg_class 的估算行数。

## 实际迁移清单和依赖

| 版本 | 内容 | 依赖/作用 |
|---|---|---|
| 3024 | sourceid_oidc | users/auth_tokens；新增身份、事务、Session 表及 token session_id |
| 3025 | feishu_personal | 新增应用/连接/OAuth state/个人导入基础表 |
| 3026 | sourceid_binding_review | users；绑定复核和审计 |
| 3027 | sourceid_local_session | 3024 Session；本地会话元数据 |
| 3028 | sourceid_enterprise_ownership | users；受控员工归属证据及索引 |
| 3029 | sourceid_jit_registration | users；JIT 注册审计 |
| 3030 | sourceid_verified_email_policy | 3026/3027 等；关联决策、阻止列表和策略开关 |
| 3031 | feishu_personal_recovery | 3025；稳定资源映射、Attempt、Grant Epoch、恢复约束及回填 |
| 3033 | feishu_application_grant | 3025/3031；授权模式、应用身份约束及唯一索引 |

3032 在候选中不存在，不补造；golang-migrate 按实际版本序列执行。PG/SQLite 各有真实 up/down 文件，实际 SHA 见 `RC2_CHANGE_MANIFEST.json`。本轮不改历史 SQL、不启用 AutoRecoverDirty、不使用 ForceVersion。既有 task_pending_ops 和 3023 来源 execution fencing 属于生产基线。

## 引擎身份与隔离

从生产实际 PostgreSQL 镜像 `sha256:6396bfd53a97f6c1aad50af05aad4a65b06bb7c07602b42a8b33b5d51cf83ab1` 只读 docker save，直接传给测试 docker load。
打包机导入后 ID 为 `sha256:d6fb8f50e25ea880d20e3acad6862931f97cb23abf4efa9c6256d2e112ed8de4`。没有把二者当成相同 ID：逐项核对了 linux/amd64、全部 RootFS layer digest、Env/Entrypoint/Cmd/卷/工作目录等非空运行配置。仅 inspect 的空/default 历史字段不同。完整比对见 `evidence/pg-image-identity.json`。
实际 PG17、pg_search 0.22.2、pgvector 0.8.1，以及 pg_ivm/PostGIS 等生产扩展随镜像保留。

最终克隆容器 `knowhub-rc2-pg-final` 使用 network=none、不发布端口，独立私有 pgdata，CPU4/内存10GiB，maintenance_work_mem 1GiB。需要连接的验证容器只共享此网络命名空间；不使用公司业务网络。没有提供生产加密密钥给 APP 验证实例。

还原发现并保留两个夹具问题：初次 pg_isready 命中初始化临时 PostgreSQL、随后镜像预置 Schema 与 restore 冲突。修正为等待 PID1 为最终 postgres，并从 template0 建空库。第一次引擎 ID 跨 Docker 存储格式变化也单独核实 RootFS/配置，不当作产品或迁移 PASS。失败库及日志保留在私有目录，没有清除业务数据或修改迁移来绕过。

## 验证与恢复方法

完整还原并核对快照行数及 Knowledge/tenant/KB 身份摘要后，保留 `rc2baseline` 不变，复制为 `rc2upgrade`；应用同一个 `database.RunMigrations` 执行所有真实迁移，再重复运行验证幂等。
核对用户全行摘要、tenant_members ACL 摘要、Knowledge/KID/KB 身份、业务行数、业务索引定义和 valid/ready；新增 OIDC 事务读写在事务内执行并回滚。

故障演练使用同一生产 Schema 的独立空数据 `rc2fault`（不是全量业务数据故障库）：对 auth_tokens 持有 ACCESS EXCLUSIVE 锁，以 statement_timeout 750ms 触发迁移失败，检查 dirty 拒绝重试和 3024 不部分提交。恢复演练从保留的**完整已还原 baseline**复制新数据库再正常升级，不手工改版本号。故障库保留，不把失败改成 PASS。

磁盘和索引进度见 `clone-resource-samples.json`。迁移锁采样非连续观测，不能以没有采到等待宣称绝无瞬时锁。正式生产等待时间仍需结合实际流量、事务和发布窗口决策。

APP 启动验证若执行，使用另一个升级后克隆副本：仅在该副本暂停来源、终结待处理意图、取消未完成解析，空 Redis/空文件目录、禁用外部身份，验证真实 RC1 二进制对升级 Schema 的启动和 health。此门禁不替代 OA 登录、旧来源更新或真实 RAG 验收。具体状态以证据为准。

## 最终结果

**DDL、数据/权限对账和启动兼容通过；完整恢复门禁仍 BLOCKED，原因是恢复后 ANN 召回退化。不能将二者合并报告为“迁移与恢复全部通过”。**

| 实测项目 | 结果与范围 |
|---|---|
| 一致性备份 | 6,239,732,271 bytes；导出 238.231 秒，传输 238.469 秒 |
| 完整数据核对 | 92 个 COPY 数据表（83 个应用表、9 个扩展配置表），差异 0；20 个 sequence SET 重放 |
| 核心数据 | users 44、Knowledge 4,060、Chunk 4,961,022、embedding 1,386,916、Wiki 页面 71,652，前后相同 |
| 修复还原后 post-data | 1,415.686 秒；这是该阶段耗时，不能当作一次完整无故障恢复总耗时 |
| 完整 baseline 复制 | 159.360 秒，FILE_COPY |
| 全量数据 3023→3033 | 2.100 秒；重复运行 2.763 秒；3033 clean |
| 数据与权限 | 用户、tenant_members、稳定 KID/KB 映射、业务索引定义摘要相同；invalid indexes=0；11 类配置/ACL/Token 数据摘要相同 |
| 新增表读写 | 合成 OIDC 事务插入/读取后回滚，通过；不是真实员工登录 |
| 锁故障 | 750ms statement_timeout 确实发生；首轮退出 1、3024 dirty；重试退出 1；无部分 3024 提交 |
| 完整恢复演练 | 从保留的完整 3023 baseline 复制新库 334.776 秒，再正常升级 2.347 秒；未使用 ForceVersion |
| RC1 实际 APP 启动 | 独立静默副本 health200，10.654 秒；身份/角色行不变 |
| 实际 RC24 载荷回退启动 | 同一静默副本 health200，5.851 秒；不等于新任务/OA Session 语义回滚已验收 |
| 独立迁移工具 | 9 PASS / 0 FAIL / 0 SKIP；完整业务库只读检查/3033 重放，以及生产 Schema 空数据升级、错误库/dirty/非法模式拒绝 |

迁移前后数据库大小分别 19,933,755,059 / 19,934,713,523 bytes，净增 958,464 bytes；逻辑恢复已去除原库部分膨胀，不能直接用该数估计生产总占用。隔离实验多份完整库使打包机空闲空间从约 204 GB 降到约 116.6 GB，未触及生产磁盘。迁移锁只采样 3 次，均无等待；不证明发布时没有瞬时锁或并发流量影响。

首次恢复默认 64MiB shm 导致 HNSW 并行创建失败；中断的并行恢复还留下 25 张空表。完成逐表修复、全量 COPY 行数对账和完整 post-data 重建后才进行迁移。原失败日志保留，没有将该次失败改记为 PASS。最初迁移 harness 因 gojieba 字典缺失在执行 SQL 前失败；补充已批准字典挂载后重跑通过。正式迁移工具不初始化应用服务，规避此无关依赖。

## ANN 恢复差异：仍需处理

BM25 查询可返回结果，向量数据自距离为 0，索引均 valid/ready；这些不足以证明检索恢复正确。

固定 embedding 3945：生产原索引相同查询最小距离 0，逻辑恢复后的 DB3023 和 DB3033 均为 0.31139911438416057；在隔离库强制精确扫描为 0。原向量摘要相同，问题在升级前的恢复库已存在，**没有证据归因为 3024–3033 DDL**。

仅在 `rc2recovered` 对同一 1024 维索引进行一次串行 REINDEX：937.579 秒，m=16/ef_construction=64 定义不变、parallel maintenance=0、maintenance_work_mem=4GB。固定点仍失败，KB 查询最小距离变为 0.3199191520890434；原 baseline/upgrade 和红色证据保留。

对六个有已入库内容的 active 来源各取首/中/末 embedding，共 18 个点，按相同 KB 过滤、ef_search=100、strict_order、LIMIT100 对照：

- 生产原索引：17 个精确自召回通过、1 个失败。
- 串行恢复索引：14 个通过、4 个失败；这 4 个在生产均通过，其中一个恢复查询返回 0 行。
- 18 个点的向量摘要、KB、dimension、enabled 和 source_type 元数据相同。
- 另有 1 个生产失败点在恢复库通过，说明 ANN 图重建存在双向差异，不能把所有差异称为数据丢失或损坏。

以上是特定自召回诊断，不是正式语义检索准确率；ANN 不保证精确搜索，但不能据此忽略已测得的相对生产退化。没有调低断言、改变查询阈值或在生产重建索引。当前恢复检索等价性不通过。证据：`ann-source-comparison.json`、`ann-exact-reference.json`、`ann-scoped-comparison.json`、`serial-reindex.json`、`ann-production-sample-comparison.json`。

下一步最小处理是单独验证**保留现有物理索引的 PostgreSQL 物理备份/恢复路径**，或在隔离库形成可证明的逻辑恢复索引质量修复；先确认所需备份权限、空间和数据处理范围。无需为此重写飞书导入、重新 Embedding 或扩大全站索引架构。原地 3023→3033 升级本身不重建原 HNSW；本问题影响的是当前逻辑备份恢复和回滚保障。

`clone-result.json` 中早期 `app_startup=NOT_EXECUTED` 保留阶段原始记录；后续两份独立 startup 证据补充了实际执行结果，不覆盖历史。原始敏感日志、备份和数据仅在受限目录保存；Git 只含脱敏指标、摘要和测试事件。
