# RC3 物理备份恢复与 ANN 等价性

2026-10-20 已完成用户单独批准的**一次**生产物理备份及隔离恢复，没有执行生产迁移、REINDEX、重启、配置变更或来源暂停。

## 实际结果

| 项目 | 结果 |
|---|---|
| 备份退出 / SSH 目标写入退出 | 0 / 0 |
| 备份大小 | 23,161,352,192 字节 |
| 备份耗时 | 1402.907 秒，约 23 分 23 秒 |
| 限速 / 检查点 / WAL | 16 MiB/s / spread / fetch |
| 备份 SHA256 | `f5d1372830e004f4c9d5dba1700ca63004765204aea2360e5d1f59c3086b9016` |
| 完整 pg_verifybackup | PASS，退出 0；未跳过 checksum 或 WAL 校验 |
| 解包 / 校验 / 独立工作副本复制 | 217.902 / 88.352 / 244.322 秒 |
| 实际恢复启动 | 25.357 秒；recovery=false |
| PostgreSQL / pgvector / pg_search | 17.9 / 0.8.1 / 0.22.2 |
| Schema | 3023 clean；本轮没有重复全量迁移实验 |
| 非法或未就绪索引 | 0 |
| 同组 18 点向量与元数据 | 完全相同 |
| 18 点恢复后结果与生产逐项对比 | **18/18 一致**（含返回数与最小距离） |
| ANN 自召回统计 | 生产 17 PASS / 1 FAIL；物理恢复 17 PASS / 1 FAIL |

保留生产原失败点 1579042（最小距离约 0.271523866），不将它改为 PASS，也不要求近似检索凭空达到原生产没有达到的 18/18。恢复门禁的断言是：同向量、KB、enabled/source_type 过滤、1024 维 halfvec、ef_search=100、strict_order、Top100 下，使用原 HNSW 索引计划，全部 18 个结果与生产一致。RC2 逻辑恢复的 14/18 原证据保留。本轮证明物理恢复消除了该组恢复行为差异，不据此推断所有业务查询均已穷尽验证。

恢复计数：44 users、4060 Knowledge、4,961,022 Chunk、1,386,916 embeddings、71,652 Wiki 页面、32 个来源。没有重新生成 Embedding 或重建索引。

## 隔离与证据

生产通过已批准 pg_basebackup stdout 经受控 SSH 流式到打包机，不在生产落盘，不创建 slot，不修改 WAL 保留策略，超时 60 分钟，无自动重试。生产 APP 身份/健康及锁、磁盘在过程中只读监测，记录于受控证据。

保留原始解包副本，pg_verifybackup 后复制成独立工作目录（无硬链接）。工作实例网络 none、无映射端口、无 APP/Worker/外部模型；禁用工作副本 pg_cron 加载、归档/恢复远端命令及 primary_conninfo。原备份配置不修改。WAL Timeline 1，范围 `88/19000028` → `88/19002BB0`。完成后已停止隔离实例，备份及工作目录保留。

恢复镜像 `sha256:d6fb8f50e25ea880d20e3acad6862931f97cb23abf4efa9c6256d2e112ed8de4`，与生产 PostgreSQL 镜像的 payload 等价性依据 RC2 `pg-image-identity.json`，不是只凭 tag 相近推断。

完整脱敏结果将归档到 `evidence/physical-recovery.json`；实际备份、原始日志和数据只保存在 Git 外受限目录。

**边界：此隔离副本执行的是实际 PostgreSQL 恢复、索引计划与 ANN 查询，不是带模型的 Portal/RAG 用户请求。真实 HybridSearch、RAG、KB 授权和原文引用须在最新 RC3 产品实例独立验收，不能以 SQL ANN 代替。**

校验与恢复步骤参照 [PostgreSQL 17 pg_verifybackup](https://www.postgresql.org/docs/17/app-pgverifybackup.html)；该工具校验不能替代实际恢复启动。备份模式参照 [pg_basebackup](https://www.postgresql.org/docs/17/app-pgbasebackup.html)。
