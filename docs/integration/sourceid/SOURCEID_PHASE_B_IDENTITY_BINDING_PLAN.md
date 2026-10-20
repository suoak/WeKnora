# 阶段 B 企业身份绑定计划

2026-10-15。阶段 A 已有两笔真实 ID Token/nonce/UserInfo 验证通过；阶段 B 尚未放行。本文依据现有源码和脱敏字段 schema，不根据字段名称认定业务含义。没有修改打包机运行配置、绑定真实身份或访问生产服务器。

更新：用户后续确定 RJGH 为产品设计标识、可信 RJEMAIL 为首次候选匹配来源。本方案保留旧审核 CSV 通道，并按新指令实现邮箱发现待审候选；这不等于通过邮箱自动开户或自动绑定。最终代码与验收见 [RJEMAIL/RJGH 绑定报告](SOURCEID_EMAIL_RJGH_ACCOUNT_BINDING_REPORT.md)。外部稳定性/可信性契约仍待确认。

最新契约更新：RJEMAIL 由 SID 已验证且属于当前员工，已正式确认，不再作为外部阻塞。普通账号在企业策略批准、员工状态及会话安全条件满足后可自动批准候选，无需逐员工人工申请。异常和高权限账号继续人工处理；RJGH 稳定性、状态和会话契约仍待确认。

## 已有实现及边界

| 能力 | 代码 | 审计结论 |
| --- | --- | --- |
| 审核映射 | cmd/sourceid-enroll/main.go | CSV 表头 employee_no,user_id；默认 dry-run，只有 --apply 写入。要求 issuer/reviewer/reference；校验本地账户启用、批内重复、已绑定身份及现有映射冲突；事务回滚失败批次 |
| 首次外部身份关联 | internal/application/repository/sourceid.go ResolveSourceIDIdentity | 依据审核 enrollment 查找既有 user_id，锁定审核行；subject/employee/user 冲突拒绝；仅新增 external identity，不按邮箱创建用户 |
| 企业登录门禁 | internal/application/service/sourceid.go loginSourceID | protocol_only 或 contract 未确认时拒绝；还要求员工、状态及可信会话字段齐全，允许状态严格匹配 |
| 会话与业务令牌 | sourceid.go、repository/sourceid.go | SourceIDSession 关联原 UserID；JWT/Refresh/SwitchTenant 继承 SessionID；会话撤销与退出墓碑阻止派生令牌重新放行 |
| 权限延续 | user.go LoginWithOIDC | 返回原 User，后续沿用原租户选择及 memberships；绑定不重新分配 tenant/role/知识库权限。真实权限保持尚未验收 |
| 阶段 A | user.go | UserInfo 成功后 protocol_only 立即返回 identity contract pending，在绑定及签发前停止 |

现有合成测试 TestReviewedEnrollmentDryRunAndAtomicConflict、CSV 重复/格式检查、TestSourceIDMockLoginAndSessionLifecycle、UserInfo subject 检查及 protocol-only 测试已有覆盖。本轮复用实现，不新增另一套身份映射系统。真实 SID 员工绑定 NOT_TESTED。

## 必须取得的字段契约

实际 UserInfo 顶层 id/sub 为 string，auth_time 为 number，attributes 为 object。以下 attributes 均实际观察为 string：GH、RJGH、objectId、STAFFTYPE、HEADCOUNTTYPE、SFLBDM、POSITION、POSITIONCLASSIFY、RJEMAIL、RJXM、XM、XB、TEL、SZDW、SZDWCODE。只确认名称和类型，不确认自然人/岗位/工号/状态语义。

| 待确认项 | SID 必须提供的信息 | 未确认处理 |
| --- | --- | --- |
| 稳定员工唯一标识 | 精确字段路径、来自签名 ID Token 或已验 sub 的 UserInfo、自然人/岗位语义、跨应用/跨岗位稳定性、换号及再雇佣规则、是否保留前导零 | 不设置猜测 employee claim，不绑定 |
| 员工状态 | 精确字段路径、类型、完整枚举、唯一允许值或允许值集合、离职/停用/未知/缺失语义、更新延迟 | 缺失/未知拒绝；当前实现单个允许值，如契约需要多值再做必要适配 |
| subject | sub 与自然人/岗位关系、稳定性与变更通知、issuer 范围 | 不将 sub 擅自当工号或会话 |
| 会话 | 可信字段路径，是否等价 SessionIndex，生成及重用规则、跨设备/跨登录唯一性、有效期与注销语义 | 不用工号/邮箱/sub/jti/objectId 替代 |
| 通知认证 | BACK_CHANNEL 请求认证/签名、格式、压缩、ACK、重试、来源策略 | backchannel 继续关闭，不用未验证来源头替代认证 |

SessionIndex 未确认仍阻塞当前正式 Session 登录结构；它不阻塞阶段 A Token 定位。协议验证成功不等于企业账号可放行。

## 审核绑定与权限保持流程

1. SID 正式确认上述契约；记录审批引用和契约版本。先在隔离 Mock 中用精确类型/缺失/冲突/停用/状态变更测试。
2. 管理员使用受保护渠道核实员工与原 KnowHub User ID，禁止通过邮箱自动认领。只读记录原 user_id、tenant_id、成员关系、角色、知识库授权基线；不把个人资料放入 Git。
3. 生成受保护 CSV，表头 employee_no,user_id；审核人和审批单独留痕。确认目标账户启用，工号与本地用户一对一，无冲突。
4. 在测试库运行审核工具，不带 --apply 即 dry-run：`sourceid-enroll --input <protected.csv> --issuer https://sid.ruijie.com.cn --reviewer <reviewer> --reference <approval>`。DSN 仅由部署进程安全注入 SOURCEID_ENROLL_DSN，不打印。
5. 提交脱敏差异：新增/保持/冲突条数、目标环境、审核引用、权限基线是否变化。经另行授权才加 --apply；本轮不执行。
6. 外部契约、映射及会话安全条件全部满足后，经单独授权才关闭 protocol_only 并启用 contract_confirmed。真实认证后绑定 issuer/sub 到审核的原用户；不得自动创建用户或租户成员。
7. 验收 user_id/tenant_id、空间、知识库、管理员/成员角色与基线一致；本地登录继续可用。验证 Refresh/SwitchTenant 与设备隔离，未验证统一登出前不得宣称阶段 B 完成。
8. 错绑回滚先阻止企业登录并撤销受影响的 SourceID 会话/派生令牌，再按审批解除指定 external identity/审核映射，恢复正确映射；不删除原 User、租户、知识库或权限。现有 enroll 工具没有撤销命令，需要另行审批的精确事务/运维工具，不能把 --apply 重跑当撤销。

## 仍需解决的代码/验收风险

- ResolveSourceIDIdentity 与 CreateSourceIDSession 为分开的事务；后续 Session/JWT 失败可能留下已审核 external identity 或未使用会话。应做故障注入并评估是否需要统一事务；不应未经证据重构认证模块。
- Session 当前固定七天，与正式 SID 生命周期尚未对齐。待契约确认后评估必要适配。
- 003024 源码迁移已声明 external identity 的 provider/issuer/subject、employee_no、user_id 唯一约束及 enrollment 的 issuer/employee_no、issuer/user_id 唯一约束；正式绑定前仍须只读核对目标库实际约束，Go 模型字段本身不等于数据库已生效约束。
- 字符串字段采用 trim 与长度限制；稳定标识格式/大小写规则应明确，不能无意将不同身份归一化。
- at_hash 当前未实现校验，ID Token 还有一笔间歇性失败；需在正式阶段 B 放行前处置或明确标准适用边界。

## Raw Basic

保持当前已成功的 raw 运行配置。本次成功发生在更正凭据之后，不将 raw 认定为 401 修复。恢复 oauth_form 的条件：SID 明确支持 RFC 6749 的凭据解码规则，或在受控进程中只得到两算法等价布尔值；随后单独授权一次同凭据、同其他参数的新 code 验证。未授权前不修改打包机认证模式，不混入阶段 B 工作。

完成：现有实现审计、实际 schema 清单、审核/回滚计划、安全门禁保持。待 SID：身份/状态/会话/通知契约。尚未完成：真实绑定、权限实测、生产放行。当前不是 READY_FOR_RELEASE。
