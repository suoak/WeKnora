# 第四批：正式 3033→3034→3035 迁移验收

P0-A 的修复是候选源码基线对齐，不是修改生产数据库或修补版本号。旧 FAIL 留在 `evidence/batch3/linux/7098d6/pg-live-cas.log`；新 PASS 使用本批候选及日志，不追溯改写历史。

## 源码和入口

- `internal/database/migration.go::RunMigrationsWithOptions`：沿用实际应用启动入口，PostgreSQL 使用 `file://migrations/versioned`，SQLite 使用 `file://migrations/sqlite`。入口本批未修改。
- `internal/application/repository/document_postgres_migration_test.go`：完整当前 source 执行 `Migrate(3033)` 建立正式旧版本记录，然后调用应用启动迁移器；另设空库直接启动、冲突失败与快照恢复测试。
- `internal/database/migration_enterprise_publication_test.go`：SQLite 使用完整当前 source 建立 3033，不依靠拼接目录；调用真实启动入口，再 down 两步至 3033/up。
- 3034/3035 两种方言 SQL 复用前三批源码，无审批回填、凭据扩权或成员修改。

所有本批正式验收路径均不调用 Force、不手写版本记录、不使用 composed migrations。被替代的第三批 composed 测试源码保存在完整快照，其历史结果继续保留。

## 实际验证范围

| 场景 | 覆盖及断言 |
|---|---|
| PostgreSQL 隔离 3033 旧库 | 完整 RC3 链建立 3033，应用启动升级 3035；版本 clean |
| PostgreSQL 全新库 | `TestDocumentM3PostgresEmptyStartupMigrationRunner` 直接由应用入口执行全部正式历史 |
| SQLite 全新库 | `TestSQLiteMigrationsCreateVersionedSchema`，保留 RC3 飞书断言并检查新增表 |
| SQLite 3033 旧库 | 当前完整 source 建立旧库，启动升级，3035→3033→3035 |
| PostgreSQL rollback | 单步至 3034/up；两步至 3033/up；epoch incarnation 更新避免旧授权复活 |
| 旧数据一致性 | tenants/users/members/keys/Portal/KB/documents 的完整行快照不变；SQLite 哈希比对、PG JSONB 行比对 |
| 历史非公开 | 企业 space/KB/document policy 和 subject grant 表不自动产生批准记录；已发布 Portal sentinel 不变也不成为批准 |
| Key 不扩权 | 原 capabilities/full_access/凭据列保持原值，完整行快照覆盖 |
| CAS 与审计 | PostgreSQL 真实事务、撤销优先并发、审计回滚及文档 epoch 测试重新执行 |
| 故障诊断与恢复 | PG/SQLite 制造 3034 表名冲突，真实启动失败且 dirty=3034；重启继续拒绝；恢复迁移前快照到新隔离库后升级成功 |

故障恢复没有修改 failed DB 的版本行。失败库保留到隔离测试结束，测试容器/临时库随后销毁；恢复使用合成快照。实际生产备份、恢复演练和停机窗口仍未测试、未批准。

## 环境、日志及限制

使用无网络 Docker、ParadeDB PostgreSQL 17 镜像、隔离 Unix socket 和合成 SQLite 文件。测试 DSN 有目录、日期、随机任务名和库名约束，不能传入生产 DSN。

最新命令、退出码、镜像 ID、候选逐文件指纹及最终结果统一见 [30 测试结果](30_FOURTH_BATCH_TEST_RESULTS.md) 和 [交付索引](evidence/batch4/delivery-index.json)。首次完整 PostgreSQL PASS 位于本批 `linux/a14434`；加入故障恢复后的 PASS 位于 `linux/2c1385`。最终候选仍须以最终原始结果为准。

数据库约束通过正式 SQL、Schema 测试与授权组件测试共同验证。go-migrate 的 `schema_migrations` 记录 version/dirty，并不存储每文件 checksum；不能宣称数据库具有逐迁移 checksum 校验。历史 checksum 由 RC3 对照清单和迁移 manifest 测试校验。未修改这一既有机制。

生产影响为零。3034/3035 down 会移除新增授权/epoch 数据，未来上线如已有真实授权不可直接照搬本合成空授权回滚；应备份并重新审批，不能依赖旧 resource_version 恢复批准。
