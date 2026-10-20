# RC3 数据库备份、恢复及回滚 Runbook

本卡是正式发布准备材料，不授权新的生产操作。2026-10-20 单次物理备份授权已经执行完成；正式维护窗口若需新备份、迁移或切换，必须按该窗口另外批准。

## 备份

1. 只读核对服务器、PG 容器镜像、DB/schema/dirty、角色 replication 权限、默认表空间、磁盘、WAL、已运行备份和复制槽。配置或表空间与本次验证不一致则重新评估，不直接套用路径。
2. 在受限目标目录保存 APP/Frontend/PG/生产 Python MCP 精确镜像、Compose、运行配置及文件卷备份。不能只备数据库：附件/原文文件和配置必须与数据库状态匹配。维护窗口应停止所有业务写入/消费者，记录 RPO 与时间点后生成最终一致集合。
3. 本次批准且执行过的命令形式（不得未经批准再次执行）：

```text
docker exec -e PGAPPNAME=knowhub_rc3_physical WeKnora-postgres \
  timeout --signal=TERM --kill-after=10s 3600s pg_basebackup \
  --username=postgres --no-password --pgdata=- --format=tar \
  --wal-method=fetch --checkpoint=spread --max-rate=16384 \
  --manifest-checksums=SHA256 --label=knowhub-rc3-physical
```

4. stdout 通过受控 SSH 流式送达受限目标文件，分别核对生产命令与写入端退出码、字节数及 SHA256。失败或结果未知不接受；不自动重试、不改 WAL 配置、不建持久 slot。监控生产健康、磁盘、等待锁及备份进度，异常立即按窗口停止规则处理。
5. 当前 archive_mode=off、wal_keep_size=0，fetch 依赖备份期间所需 WAL 尚在；这次成功不等于未来每次成功，更不代表具备持续 PITR。WAL 缺失时必须失败，不跳过校验。

## 独立恢复与验收

1. 新建受限空目录；先校验归档 SHA、tar 路径与链接安全。不得覆盖原生产目录或故障目录，不允许未确认的外部表空间映射。
2. 解包到原始备份目录，用匹配 PostgreSQL 17 的 pg_verifybackup（及 pg_waldump）完整校验，无 skip-checksums / no-parse-wal。保存 Manifest、校验摘要和所需 WAL 范围。保留原备份目录不启动。
3. 复制到新的独立工作目录，不使用硬链接。仅修改工作副本隔离配置：network none、无端口、无 APP/Worker、无 cron、无外部归档/恢复/主库连接。核查 include/外部文件路径和 standby/recovery signal；异常先人工评估。
4. 使用已确认的 PostgreSQL/pg_search/pgvector 镜像启动，核实 WAL 恢复完成、pg_is_in_recovery=false、schema/dirty、数据量、ACL/KID 摘要及索引 valid/ready。
5. 对固定 18 点按同过滤、维度、TopK、ef_search、iterative_scan 和执行计划比较生产基线。标准是与生产逐项相等，保留既有失败，不以强制精确扫描或 REINDEX 把指标“修绿”。
6. 在批准的隔离应用环境核查真实授权 KB 检索、BM25/Hybrid、来源、RAG 和权限拒绝；网络 none SQL 验证不冒充产品请求。恢复结果未完成应用验收的范围明确列出。

## 发布与回滚衔接

DB3023→3033 的迁移沿用已通过 RC2 的克隆结果及原 SQL：3024–3031、3033，无 3032；不重复无意义实验、不修改历史 SQL、不 ForceVersion。正式迁移须独立发布批准。

回滚优先恢复旧应用且保留新数据，但旧 RC24 不识别停更屏障，不能让其 Worker 恢复危险同步。业务库受损时停止全部写入、保留原故障卷，在新空卷恢复已验证备份和匹配文件集合，独立核对后再按批准流程切换。明确备份后写入丢失的 RPO，禁止直接覆盖现场或承诺零数据损失。

生产仍通过打包机出镜像、Windows 上传与服务器受控部署；不改变交付方式。回滚时保留生产现有 Python MCP 镜像及配置，不拿测试 MCP 镜像覆盖。
