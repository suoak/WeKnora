# 生产历史存储清理回执

用户明确授权“空间不足的话，先清理历史备份和无用的历史docker”。本回执仅描述清理时点；后续发布见[生产验收](production-acceptance-20261127.md)。

删除 141 个无容器引用的历史镜像、两个已停止的历史数据库恢复容器及其专用恢复卷，以及已被更新且经过恢复验证的备份替代的 Schema3033 冷备份。保留全部生产数据卷、原文和 Neo4j 备份、两个真实恢复通过的3042原始归档、最新3049归档及当前/回滚镜像。未使用 force/prune volumes，未清理 Redis 安全锁。

文件系统实测可用空间增加 82,172,006,400 字节，数据盘占用从95%降到79%，镜像传输前余量102.82GiB。Docker负数reclaimable指标未作为回收量依据。清理前后七个运行容器的ID、镜像、启动时间和生产挂载一致，Schema3051 clean，未重启服务或迁移。

首次收尾断言因Docker挂载数组顺序不同而失败；随后对照原始加密快照规范化挂载后独立审计通过。不能将第一次失败写成通过。

[签名摘要与保留备份SHA](production-release-evidence/production-storage-cleanup-summary.json)。根分区空间仍不适合大型备份，后续私有备份位置使用 `/mnt/sata0/knowhub-folders-release-<SHA>`，目录0700、文件0600。回滚脚本新增容量和路径保护，POSIX十四项测试（含镜像映射和SID到期保护）、bash语法及无操作dry-run通过，见[回执](production-release-evidence/production-backup-guard-tests.json)。
