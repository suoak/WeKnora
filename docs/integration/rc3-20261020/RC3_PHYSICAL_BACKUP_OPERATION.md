# RC3 物理备份操作卡与授权范围

当前状态（2026-10-20 更新）：用户已单独批准本卡所述一次物理备份及隔离恢复；**已执行完成**。完整校验、实际恢复和 18 点 ANN 对比见 `RC3_PHYSICAL_RECOVERY_VALIDATION.md`，授权证据见 `evidence/physical-backup-authorization.json`。下文申请/停止条件保留为本次操作的原始范围，不表示允许再次备份或生产恢复。RC2 pg_dump 授权没有被扩张使用。

## 已确认条件

生产 10.51.134.114，容器 WeKnora-postgres，PG17.9，DB3023 clean。postgres 为 superuser/replication；本机 Unix socket replication trust 已存在，无须新账号、GRANT 或修改 pg_hba。wal_level=replica、full_page_writes=on、max_wal_senders=10，目前无 sender、无 replication slot。仅默认表空间；整个集群包括 weknora（约23.08GB）、postgres、paradedb，不是只备份一个数据库。

archive_mode=off、wal_keep_size=0、max_wal_size=1GB、当前 WAL 约1.06GB、data_checksums=off。pg_basebackup/pg_verifybackup/pg_waldump 17.9 均存在。生产分区空闲约18.61GB，**禁止在生产落地完整副本**。具体只读证据见 evidence/physical-preflight.json。

## 申请执行的最小操作

在现有生产 PG 容器内部调用本机 socket，输出经已固定主机身份的 SSH 链路、Windows 内存转发，直接落到打包机独立0700目录；不在 Windows 或生产落盘业务副本。申请包含整个集群（含账号/配置/正文）的受限数据处理。

```sh
docker exec WeKnora-postgres pg_basebackup \
  --username=postgres --no-password --pgdata=- --format=tar \
  --wal-method=fetch --checkpoint=spread --max-rate=16384 \
  --manifest-checksums=SHA256 --label=knowhub-rc3-physical
```

max-rate 为16MiB/s，约24GB基础数据理论读取约25分钟，另有 spread checkpoint、WAL及传输开销；不承诺总耗时。会增加生产读I/O、连接及备份检查点/WAL开销，不是普通只读 SQL。无生产重启、业务DML/DDL、角色/配置变更；不创建持久slot、不切换归档、不主动pg_switch_wal、不在生产REINDEX。

stdout tar 不支持 -Xstream，故本方案使用 -Xfetch。当前没有WAL保留保证：若备份期间所需WAL已回收，必须判失败、保留未可信产物并停止，不修改wal_keep_size来强行通过。不使用 -Xnone，不把缺失WAL的副本标记可恢复。若fetch在实际流量下不可完成，应另外审批受控的stream通道/保留策略，不能自动扩大生产权限。

审批应覆盖：单次备份、16MiB/s基础数据速率、最长60分钟（超时取消）、允许spread检查点/WAL开销、整集群敏感数据受限落盘、仅隔离恢复。性能异常、空间不足、连接异常则终止备份；不自动重试生产读取。已有生产健康异常时不开始。

## 仅在隔离环境执行

备份流SHA256、bytes、开始/结束时间、退出码与受控stderr摘要；先审查tar路径和表空间，解压到独立空目录，绝不覆盖现有卷。使用相同PG镜像运行pg_verifybackup验证manifest及所需WAL；再建立副本启动，backup原件保持不变。启动容器network=none、不发布端口，不挂公司文件卷，无APP/Worker，不加载生产连接到外部服务。

恢复前核查postgresql.auto.conf及信号文件，隔离实例禁止回连生产/归档命令；任何调整只对恢复副本记录diff。关闭隔离副本pg_cron自动任务，防止DB内定时副作用。记录起止LSN/时间线、恢复终态、表/索引身份，不以端口存活替代一致性完成。

同18个embedding点、相同向量摘要、KB过滤、执行计划、ef_search=100、strict_order、LIMIT100重新核对。保留生产17/18及逻辑恢复14/18证据。验收标准不是强行18/18：要求物理恢复与对应备份基线逐点结果一致；若备份时生产有新写入导致快照不同，先解释LSN、向量及过滤元数据差异，不能降低断言。自召回统计不等于真实RAG可用性，另须通过授权测试身份完成正式HybridSearch、来源引用及常见RAG；不得在含生产数据的隔离DB擅自连接外部模型。

这是单次一致性恢复点，不是已经具备持续PITR。生产archive_mode仍off，不承诺任意时点恢复，也不以本次DB备份替代正式窗口DB/文件/队列协调备份。
