# OA/SID 阶段 B 最新实施基线

以 2026-10-15 用户最新确认取代此前“必须先有员工状态 claim/SessionIndex”的首期方案。

## 已确认

SID 授权阶段拒绝无效员工；RJGH 稳定、唯一且不回收；RJEMAIL 可信并属于当前员工。既有邮箱自动绑定代码复用，不重写。

## 已实现，尚未启用

1. `SourceIDEmployeeAdmissionResolver` 规范化准入结果与时效。
2. `authorization` 适配器只接受完整验证后的单次 OIDC 授权事务；SSO 重新检查及停用时效确认仍是独立门禁。
3. `SOURCEID_SESSION_MODE=local_approved`，默认 external，首期无 SID SessionIndex 依赖。
4. 独立持久化本地企业会话元数据、随机 Session ID、绝对/空闲/准入期限及绑定检查。
5. 授权准入模式禁用 Refresh；可复核模式的单次轮换/重放撤销仅用于 Mock 和未来批准场景。
6. 继续复用 issuer+RJGH 查找、sub 一致性、可信邮箱首次匹配及普通账号自动绑定策略；保留原 user_id 和权限。
7. 撤销、退出、切租户及 JWT/session_id 一致性校验。
8. 003027 为新增元数据表，不改现有 users/成员/知识库表；未经批准不在打包机执行。

## 已完成验证

代码 `431226c3b1bd5338ec06c1bd0520e637d5fed398` 的 [CI](https://github.com/suoak/WeKnora/actions/runs/38717758880) 四项全部通过。真实 Vue + 业务认证 Handler + Mock SID 已完成 Portal、auth/me、刷新恢复、原权限、退出和无效员工拒绝断言；PostgreSQL 003024→003025→003026→003027 及回退、并发和完整认证/MCP/飞书回归通过。范围和跳过项见 [验收报告](SOURCEID_OA_LOGIN_E2E_ACCEPTANCE_REPORT.md)。

## 真实放行前剩余工作

- SID：停用传播上限、已有 SSO 再 authorize 的准入检查、旧会话和未消费 code 失效规则。
- 内部：审批方案 A 的明确期限、风险窗口及普通账号自动关联策略；填写可追溯政策引用。
- 部署：提交 3026→3027 检查结果、备份和回滚步骤，单独批准后升级打包机。保持真实门禁关闭。
- 测试账号：另行批准后受保护录入企业邮箱，准备普通 contributor/viewer 和测试知识库，记录权限基线；不自动创建真实员工账号。
- 真实联调：取得上述条件后再单次授权，不自动重复登录。

不再等待员工状态字典，不接入 HR/飞书准入接口，不把统一登出强制捆绑一期登录。二期 SID 会话/Back-channel 契约独立处理，绝不伪造 SessionIndex。
