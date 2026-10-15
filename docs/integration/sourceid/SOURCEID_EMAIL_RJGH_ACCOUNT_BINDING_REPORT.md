# RJEMAIL 候选匹配与 RJGH 审核绑定报告

2026-10-15。开发分支 feature/knowhub-sourceid-oidc；代码提交 `9a530939c78c4b29e2654d4cfac86b6c7efc32a8`。运行中的打包机仍是 468410e89，本轮没有部署、迁移真实数据库、启用新身份契约或发起登录。

后续需求更新：RJEMAIL 的可信员工归属已由用户正式确认，不再列为外部阻塞。新增批准策略下普通账号自动关联提交 `523d7ab49c42314dddad630ee4295276998baa26`，保留手工审核作为异常路径。以下人工审核实现也是自动绑定的事务基础，不代表仍要求每位员工逐个申请。

## 普通账号自动关联

- 新增默认关闭的 SOURCEID_AUTO_BINDING_ENABLE，以及 SOURCEID_AUTO_BINDING_POLICY_REF（必须非空，代表经人工批准的企业策略，不来自用户输入或 Token）。所有身份/状态/会话/邮箱路径门禁仍先执行。
- 唯一可信邮箱候选写入后，自动策略在数据库事务里重新检查原用户、邮箱唯一性/快照、绑定冲突及权限，原子建立 enrollment、external identity、approved review 和审批审计；之后复用已有 Session/JWT 和前端登录结果通道，正常用户不需要额外页面。
- 系统管理员、跨空间访问用户、空间 owner/admin、组织管理员、未知角色/停用成员、缺失角色表，以及原 home tenant 缺少明确普通成员关系的账号均不自动批准；进入审核路径。不将“查不到权限资料”当作普通账号。
- 同一 RJGH/sub 的并发首次请求可复用同一 approved 绑定；唯一约束、审核行锁确保单一身份/enrollment/审批记录，不增加 SID 请求次数。
- 已撤销墓碑阻止自动重建；管理员 reopen 后必须重新人工批准，不因仍开启自动策略而悄悄放行。
- 当前成功运行的 raw Basic 不变；本轮不部署、不启用上述新开关、不发起真实登录。全部合成/CI 通过后交付，再按外部契约与迁移准备情况决定安全部署。
- 完整验收见 [OA 登录验收方案](SOURCEID_OA_LOGIN_ACCEPTANCE_PLAN.md)。

## 已实现

1. 继续先校验 Token、RS256/Issuer/Audience/有效期、授权事务 nonce、UserInfo sub；protocol_only 在身份写入前停止。员工状态、身份和可信会话契约未确认时拒绝；邮件候选能力也需要独立邮箱契约确认。
2. 新增 SOURCEID_EMAIL_BINDING_ENABLE=false、SOURCEID_EMAIL_CONTRACT_CONFIRMED=false，默认关闭。启用该路径还要求 claims_source=userinfo、employee_claim=attributes.RJGH；读取可信 UserInfo 的 attributes.RJEMAIL，不猜其他字段。
3. 优先检查已审批身份及既有审核 CSV enrollment；approved 后按 RJGH/issuer/subject 取原 user_id，邮箱变化或缺失不会重新匹配。RJGH 相同但 subject 变化安全拒绝，等待 SID 规则和重新审核。
4. 未绑定时仅用规范化 RJEMAIL 唯一匹配 users.email：去首尾空白、ASCII 小写；不去点、不忽略加号别名、不进行模糊匹配，不擅自归一化 Unicode。SQL 对本地字段按相同 lower(trim()) 规则匹配，最多取两条；零匹配、多匹配、停用用户均拒绝。重复邮箱即使另一账号停用也拒绝，策略更保守。
5. 唯一匹配仅创建 pending 审核记录；不创建 User、enrollment、external identity、Session、tenant member 或 JWT。重复候选不能绕过审核。提示映射到现有 enrollment_required 安全错误，前端不接收个人资料。
6. 显式管理员审批需要 reviewer/reference，重新核对原用户启用状态、邮箱快照及唯一性；在单事务内建立 enrollment 与 external identity 并标记 approved。现有 CSV 审批保持兼容；新增普通账号自动策略复用该事务并额外核对权限，账号所有权自助接口尚未提供。
7. 查询、审批、撤销、重新审核通过操作员 CLI sourceid-binding；SQL 访问权限是管理边界，不暴露公共审核 API。查询输出仅 review ID、user_id、status，不打印邮箱/RJGH/sub；管理员在受保护渠道核验身份和账号所有权，reference 记录批准依据，工具不能代替真实所有权核验。
8. 撤销在事务内删除对应 external identity/enrollment、撤销该用户该 issuer 的 SourceID 会话及派生业务令牌，保留 revoked 审核墓碑；不删除用户、租户、权限或本地会话。reopen 只允许原用户重新审核，更新其邮箱快照后回到 pending，不自动批准，不转移到另一个 user_id。不同用户的身份迁移不在本轮范围。
9. 审批/撤销/reopen 产生独立审核历史，操作失败则历史和修改一起回滚。已审批会话创建与撤销共享审核行锁，避免撤销后创建出新的可用会话。

## 文件与数据结构

- internal/application/repository/sourceid_binding.go：候选发现、审核事务、撤销与受审核会话创建。
- internal/application/service/sourceid.go：受控接入现有企业登录流程，先检查状态/契约再访问绑定仓库。
- internal/config/config.go、.env.example：新开关，默认 false。
- internal/types/sourceid.go：SourceIDBindingReview/SourceIDBindingAudit。
- cmd/sourceid-binding/main.go：操作员 CLI，无 Secret/DSN 参数；数据库连接从 SOURCEID_ENROLL_DSN 受控环境读取，SQL logger 关闭。
- migrations/{versioned,sqlite}/003026_sourceid_binding_review.*.sql：新增独立审核及审计表，不修改 users 或原权限表；issuer+RJGH、issuer+subject、issuer+user_id 唯一约束。原 external identity/enrollment 唯一约束继续强制冲突拒绝。

工作区另有未提交的 003025 飞书迁移，本次未混入提交。真实部署前需要协调迁移顺序，不能在 3024 库中盲目跳过其他功能的 3025 迁移或启用自动迁移。新表会保存受保护的邮箱候选快照和身份关联，应按用户数据控制访问/备份/保留期；它们不会进入诊断日志或 Git。

## 合成验收

| 场景 | 测试与预期 |
| --- | --- |
| 已绑定原账号 | repository/service 测试保持原 user_id/tenant_id/管理员标志，生成与撤销合成本地 JWT/Refresh |
| 首次唯一邮箱 | 大小写/空白规范化后仅 pending，无企业身份或会话 |
| 账号归属确认 | 缺 reviewer/reference 拒绝；管理员确认后原子 approved |
| 不存在/重复邮箱 | 拒绝，不开户、不待办放行 |
| RJGH/sub 冲突 | 拒绝，不覆盖旧绑定 |
| 停用/未知状态 | service 门禁在仓库写入前拒绝，本地停用用户也拒绝 |
| 邮箱变化 | 已绑定仍取原用户；首次候选审批前邮箱变更拒绝 |
| 绑定撤销/重审 | 会话及派生令牌撤销，墓碑阻止再次邮箱发现；显式 reopen 后再次审批 |
| 并发绑定 | SQLite/真实临时 PostgreSQL 并发审批只有一份身份/enrollment，PG 八并发只允许一次成功 |
| 迁移回退 | 临时 PostgreSQL 执行 003026 up/down，原 users 保持；真实部署 NOT_EXECUTED |
| 契约/协议门禁 | 邮箱/身份契约 false、protocol_only、未知状态或缺会话字段均在写入前拒绝 |

最终自动关联提交的 [CI 全部通过](https://github.com/suoak/WeKnora/actions/runs/38679739604)：Go 竞态专项 232 个通过条目（含子测试）、完整认证/MCP 回归 4757 个通过条目，失败 0；PostgreSQL 迁移/撤销/并发自动绑定、前端类型/23 项认证回调测试/构建以及 server/enroll/binding 工具构建通过。机器证据见 [CI 汇总](evidence/phase-b-auto-binding-ci-20261015.json)。首轮 PostgreSQL 夹具漏建 Tenant 触发外键，已补齐并通过复验，未绕过约束。完整 Go 验证使用云端 Go 1.26.8，未使用真实企业凭据。

## 管理工具操作

仅在授权的隔离环境，并由受控进程注入 SOURCEID_ENROLL_DSN：

```text
sourceid-binding -action list
sourceid-binding -action approve -id <review-id> -reviewer <admin> -reference <ownership-approval> -apply
sourceid-binding -action revoke -id <review-id> -reviewer <admin> -reference <revocation-approval> -apply
sourceid-binding -action reopen -id <review-id> -reviewer <admin> -reference <new-review-approval> -apply
```

list 是只读；变更命令没有自动写入模式，必须 -apply。不要将员工资料、审批证据原文、DSN 放到命令行或报告。不提供公共 UI/自助邮箱认领，避免未经审核的所有权转移。

## 仍需 SID 确认及真实放行条件

- RJGH 全局/issuer 范围唯一性、稳定性、换岗/重入职/号码回收规则；目前仅按产品指定字段开发，不宣称供应商正式确认。
- RJEMAIL 可信且属于当前员工已正式确认，不再阻塞；仍应补充唯一性、大小写/别名/Unicode 及转让/更新时间规则。无需擅自要求出现 email_verified 标准声明来否定已确认的企业契约；新配置尚未开启不等于外部邮箱契约未知。
- 员工状态精确字段/类型/允许值、停用与离职处理；缺失/未知仍拒绝。
- sub 与 RJGH 的稳定对应，sub 变化如何审批。
- SID 会话与 SessionIndex 的可信对应、有效期及 BACK_CHANNEL 认证要求；不得用 RJGH、邮箱/sub/jti 替代。

后续真实绑定须另行授权，先做权限基线和审核，再启用契约。现有 SOURCEID_PROTOCOL_ONLY=true、SOURCEID_CONTRACT_CONFIRMED=false 不变；新代码即使部署也保持邮箱开关关闭。没有把真实协议验证成功当作企业业务登录可放行。

## ID Token 与 Raw Basic

安全分类与无 kid/JWKS 节点不一致合成测试已开发，前一提交 d0b5ad79f 的 [CI 全部通过](https://github.com/suoak/WeKnora/actions/runs/38677283016)。首次正式 JWKS 条目指纹不同，但该指纹包含 kid 元数据，不能确定 RSA 材料变化；后续只计算 kty/n/e 的两次复核一致。节点/轮换一致性仍需 SID 查证，不能追溯证明历史失败原因。详见 SOURCEID_ID_TOKEN_VERIFICATION_FAILURE_ANALYSIS.md。

raw 运行模式保持，本轮不切换 oauth_form。正确凭据后的 200 不证明 raw 必要；恢复标准模式需后续单独授权的单变量验证。

## 部署与回滚状态

当前交付为代码、测试和报告；没有部署阶段 B 或执行真实库迁移，打包机运行版本保持 468410e89。部署前需协调 003025/003026 迁移顺序并备份；新开关和身份门禁保持关闭/阻断状态，不需要外部契约全部完成才能部署安全代码，但真实身份放行仍需确认。

部署前备份审核表和配置，保持新开关关闭/协议门禁；只升级 app/frontend 不改 PostgreSQL/Redis/DocReader。失败先关闭企业登录并回滚旧镜像，保留新增审核数据；不能用 down 迁移直接删除审批审计历史。确需回退表结构必须先审批归档，且保持企业登录关闭，原用户和权限表不删除。
