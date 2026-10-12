# 审核测试员工与历史账号绑定操作

状态：NOT_TESTED。没有取得员工审核材料、测试数据库或真实映射授权；本轮未运行数据库 dry-run/apply。工具行为已由既有 CI 验证，本说明与 `cmd/sourceid-enroll/main.go` 实现逐项核对。

## 资料与权限基线

测试员工优先普通账号，不用生产管理员。本人同意不替代管理员对数据/身份变更的批准。需提供到受限审核渠道：测试环境、HR/SID确认的自然人工号（字符串/前导零）、可信字段来源/path、员工状态/允许值、issuer、subject稳定性/岗位多身份规则、原KnowHub User ID、是否活跃、审核人和审批单。无需员工密码、完整JWT/TGT或邮箱作为自动关联键。

找到原 User ID：优先让员工在**已有本地登录会话**中查看自己的 GET `/api/v1/auth/me` 或管理员检查现有成员信息；须单独授权新增登录，不索取浏览器cookie。也可 DBA 在隔离测试数据库用经过核对的用户ID查看 users 的 id、is_active、deleted_at、is_system_admin、can_access_all_tenants。查找候选账号可用内部资料协助，最终绑定必须审核自然人工号与确切ID，不能因邮箱相同就确认。

保存基线到受限证据库：用户ID脱敏别名、系统管理员标记、跨空间权限、全部tenant_members的角色/状态、共享/知识库ACL、API Key作用域及活跃空间/偏好。仅记录Key标识/作用域，不导出密钥或password_hash。备份由DBA控制；不拷贝生产PII进入开发仓库。

## 工具的实际用法

- `--driver` 默认 postgres，可选 sqlite；`--input`、`--issuer`、`--reviewer`、`--reference` 必须给有效值，issuer须事先按正式契约核验，工具只检查非空，不替代协议验证。
- DSN仅通过 `SOURCEID_ENROLL_DSN`。使用已存在、已完成003024迁移的隔离测试库。SQLite指定已存在文件，否则连接可能新建数据库文件，不能把此误称纯只读验证。
- CSV严格UTF-8无BOM，首行 `employee_no,user_id`；仅两列、最大1MiB/10000行。工号只trim边界空白，保留前导零；UUID需合法。不要经Excel数值化。
- 同CSV重复工号/重复User ID拒绝；数据库中同issuer已有enrollment或SourceID identity占用工号/用户则拒绝；同issuer相同映射幂等，不改原审核人/审批单。
- 默认dry-run**不写映射**，但会开启事务并 `SELECT FOR UPDATE` 锁用户；不要在生产高峰执行，不假定只读角色一定有足够锁定权限。apply整批事务，任一冲突回滚。
- 输出只有 `validated_rows=N applied=false/true`，错误已脱敏；没有逐行差异输出或解绑命令。差异需审核SQL/安全材料独立提供；退出0也不代表已经apply或实际新增N条。

编译已验证版本（使用此前云端编译能力，不重新修改认证代码）：

```sh
go build -o sourceid-enroll ./cmd/sourceid-enroll
# SOURCEID_ENROLL_DSN 由受限进程环境注入；不出现在命令行。
./sourceid-enroll --driver postgres --input /secure/reviewed-test.csv \
  --issuer '<confirmed-exact-issuer>' --reviewer '<reviewer-id>' --reference '<approval-id>'
```

上述为待授权操作示例，本轮未执行。预期 `applied=false`，退出0；不符合则停止，不删冲突行重试。对于SQLite把driver改为sqlite并由安全环境提供对应DSN。

## 冲突核对与差异审批

DBA在测试库使用绑定参数（不拼接SQL）执行只读查询；不输出token/subject/工号明文到公开日志。下面是查询形状，`:...`由数据库工具绑定，不直接复制成未绑定SQL：

```sql
SELECT id, is_active, deleted_at, is_system_admin, can_access_all_tenants
FROM users WHERE id = :user_id;
SELECT id, user_id, reviewed_by, review_reference
FROM source_id_enrollments
WHERE issuer = :issuer AND (employee_no = :employee_no OR user_id = :user_id);
SELECT id, user_id
FROM external_identities
WHERE provider = 'sourceid' AND issuer = :issuer
  AND (employee_no = :employee_no OR user_id = :user_id);
```

对所有CSV行核验工号→ID唯一对应、没有软删除/停用用户、没有重绑另一人。工号冲突只报告脱敏行号/别名，退回HR/SID/管理员人工调查，不按邮箱合并。

提交差异：环境/库的非秘密标识、文件摘要、行数、新增/幂等/冲突数、issuer、审核人/审批单、旧UserID别名、权限基线摘要、备份和撤销方案。**用户确认真实身份关联后**，在批准的隔离测试环境追加 `--apply`；同命令其余参数不变。若涉及生产数据库，须独立生产变更审批，不复用测试批准。

apply后按确切issuer+工号+UserID回读审核登记及原reviewer/reference；首次成功登录才产生 external_identities 和 session。重新比较users/全部成员/知识库ACL/API Key作用域；任何UserID变化、提权或共享变化判失败。

## 错误绑定回滚（操作前再审批）

工具无删除/覆盖功能，不能用新CSV覆盖旧identity。

1. 暂停该测试的SID新登录入口，记录审计/保存受限备份和明确受影响账号；不改变密码/成员/知识库，不以邮箱修复。
2. **尚未发生首次SID绑定/登录**：DBA确认不存在对应 external_identity 及SIDsession后，在事务内锁定确切enrollment ID/issuer/工号/UserID，只删除该审核登记。删除行数必须与审批一致（通常1），否则ROLLBACK。
3. **已发生SID登录**：先按事故审批范围撤销错误账号受影响的SID会话与其session_id派生access/refresh（必要时包括被误关联的两个账号）；保留其他用户/本地密码令牌/API Key。与正常“单会话退出”不同，这属于经审批的事故处置。应确保已发行错误凭据全部失效后，才删除准确ID的错误external_identity和enrollment。
4. PostgreSQL事务先锁受影响用户、会话和映射；更新 source_id_sessions.revoked=true 与 auth_tokens.is_revoked=true（仅经审批的非空session_id集合）；然后精确删已审核的错误identity/enrollment。每个步骤核对row_count和权限快照，异常ROLLBACK；不删source_id_logouts/tombstone或其他会话证据。SQL由DBA按现场schema审阅，不提供“一键全用户解绑”。SQLite在测试库按对应事务锁语义执行。
5. 查证旧access/refresh已拒绝。重新审核正确自然人工号→原UserID，再dry-run→差异确认→apply；不静默更改subject或回填旧令牌。
6. 保存回滚审批、变更ID/脱敏前后差异和原审核凭据到受限审计库。备份恢复也必须保持撤销证据，防止恢复旧token造成复活。
