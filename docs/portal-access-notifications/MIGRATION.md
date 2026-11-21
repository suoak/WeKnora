# 通知数据库迁移与回滚

候选迁移：`003050_user_notifications`；前置版本：`3049`。PostgreSQL 使用 `migrations/versioned`，SQLite 使用 `migrations/sqlite`。应用现有 migration runner 会按顺序执行，不能跳过前置版本或用 force 掩盖 dirty 状态。

## 升级内容

- 新增 `user_notifications`：接收用户、通知类型、标题、正文、业务引用、空间、已读标志、阅读时间和创建时间。
- `(user_id, is_read, created_at DESC)` 收件箱索引、业务引用索引，以及 `(user_id, notification_type, reference_type, reference_id)` 唯一事件约束。
- PostgreSQL 修改 `tenant_portal_configs.allow_access_request` 的列默认值为 true；SQLite 在事务内重建配置表并恢复原有阶段、索引，达到相同效果。
- 所有既有开关值、申请记录及成员授权保持原值。不会为旧申请补发通知，不会统一开启历史空间。

发布前先记录当前 schema 版本和 dirty 标志、备份数据库，并保存已发布空间的开关清单。只读核查 SQL 为 [published-space-audit.sql](published-space-audit.sql)，必须使用已授权的只读连接。关闭申请的空间均列为待人工核查例外，不推测关闭原因。实际生产清单尚未取得。

测试部署先升级测试数据库到 3050，再启用匹配后端与前端。检查通知表、唯一事件约束、默认值、申请数与成员数，并用 Owner/申请人双账号验证通过和拒绝流程。申请本身只产生待审批记录，不能授予成员身份。

## 回滚

停止候选版本写入，保存当前数据库备份和通知数据，协调回退应用版本，再按对应方言将 migration runner 退至 `3049`。down 迁移删除通知表及其索引，恢复未来新记录的开关默认值为 false；**通知内容和已读状态会被删除**。申请记录、审批状态、Viewer 成员关系和审计记录不会被撤销。

迁移失败应保留失败证据，检查版本/dirty 标志和事务状态后处理；不要直接 force。生产升级、回滚和开关批量修改均未执行，需满足需求中的生产批准条件。

## 自动验证

`TestSQLiteNotificationMigrationPreservesOptOutsAndStages` 通过项目真实 migration runner 执行 3049 → 3050 → 3049 → 3050，对比配置、阶段、申请记录摘要，并验证默认值、通知幂等唯一约束和外键完整性。PostgreSQL 的实际升级和回滚需在指定测试数据库上验证；不能将 SQLite 结果当作 PostgreSQL 实测结果。
