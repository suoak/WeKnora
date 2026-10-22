# 正式测试发现：3037 多页 Wiki 迁移 P0

2026-10-22。**原c2候选隔离合成验收结论保留，但不能原样升级当前真实P3测试库。** 独立恢复库在项目正式3033→3037迁移时失败，真实测试库与P3服务未动。没有为使测试变绿而删除旧Wiki、强制版本或修改Schema记录。

原始证据：`evidence/formal-shared-upgrade/deploy-a3-20261022t072133z/clone-formal-3033-3037.log`。恢复库停留3037 dirty，完整保留；后续从已经验证的3033备份恢复到新的临时库重试，不修饰失败库。

原因：PG SQL原为 `SELECT DISTINCT knowledge_base_id,1,md5(random()... ) FROM wiki_pages`；SQLite同样包含 `randomblob(16)`。DISTINCT作用于完整输出行，随机值不同使同一KB的多页无法去重，写入以KB为主键的epoch表时冲突。早期空库/单页夹具无法暴露该问题。

修复：先在子查询中仅对 `knowledge_base_id` 去重，再对每个KB生成随机incarnation。不更改版本号、不新增权限记录、不生成历史Wiki证明、不改变拒绝策略、Key能力、APP或MCP工具。3037尚未进入main或生产；原c2已冻结包、隔离实例及其旧SQL仍保留，不原地改写。

必要源码差异只有3文件：

- `migrations/versioned/003037_personal_dynamic_wiki.up.sql`
- `migrations/sqlite/003037_personal_dynamic_wiki.up.sql`
- `internal/application/repository/feishu_legacy_migration_test.go`

新回归在真正3033形状中创建两个KB、共三页历史Wiki（同一KB两页），执行项目完整正式迁移，断言只有两个epoch、每个generation=1且incarnation32字符、三页正文和published状态不变、历史证明仍0；原有Key不扩权与down拒绝断言保留。

旧c2 SQL加新回归已在独立PG/SQLite**均复现FAIL**：`evidence/formal-shared-upgrade/migration-red-20261022t072133z/`。这不是业务PASS，作用是证明新增断言确实捕获原缺陷。

修复后两种数据库的完整正式链已经退出0，见 `evidence/v1-wiki-dynamic/development-090216bc1a7d0c54/formal-migrations.log`。修订业务源码指纹 `090216bc1a7d0c547235df30eef6f425050b9260fadba9791329b6c267a24783`。逐文件新旧SHA见 `evidence/formal-shared-upgrade/required-migration-fix-source-delta.json`。

修复后完整Go、六个关键包完整race、vet、PG/race、正式PG/SQLite迁移及构建均退出0。新冻结源码SHA为 `0a7374bbbdbd8c4a802733811ad3ecbeb70293ba33b89eefa9bd05c36ecbd371`，源码包SHA为 `d7e5a9b3d2c1609014939bb831600e8c5518acdac9cdbaf225ac819b7fb8d26a`。

新恢复库3033→3037及获准真实测试库迁移均通过；现有80/8080已升级为同源APP/前端。APP镜像ID `sha256:da19dbb14680ea450624dfca0cb3f9ed1d15f64fac6d73d88181a50808969988`，前端 `sha256:c8025af250c0e4c15b35af78f505ba9cd3a1bfe8b10bf373bde4877543cd7e3e`，实际二进制SHA `1214be4c7751a9a610cb36dd6a09b9fc3fabd087a17a5746d7ee7b6b732a8f92`。证据 `evidence/formal-shared-upgrade/deploy-a4-20261022t072133z/`。原有数据和旧Key不变、Schema3037 clean、原Python容器运行。集中业务及回滚门禁完成前仍不提交、推送或合入main；生产不变。
