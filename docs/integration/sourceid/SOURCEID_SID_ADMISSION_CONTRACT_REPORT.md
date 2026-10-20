# SID 员工准入契约（2026-10-15）

## 当前正式输入

| 条件 | 结论 | 依据 |
| --- | --- | --- |
| RJGH 稳定、唯一、不回收 | USER_CONFIRMED | 用户本轮正式确认；不再作为开发阻塞 |
| RJEMAIL 可信且属于当前员工 | USER_CONFIRMED | 用户正式确认；用于首次精确匹配 |
| 无效员工在 authorize 阶段被 SID 拒绝 | USER_CONFIRMED | 用户最新确认；首期采用此机制 |
| 新授权在员工停用后的最长拒绝延迟 | UNKNOWN / BLOCKED_ACTIVATION | 需要 SID 提供时间上限 |
| 已有 SID SSO 登录态时 authorize 是否重新检查准入 | UNKNOWN / BLOCKED_ACTIVATION | 决定短会话重新授权是否能限制停用风险 |
| 停用时既有 SID 会话是否失效 | UNKNOWN | 需要 SID 确认 |
| 已签发、未兑换 code 的停用失效规则 | UNKNOWN | 需要 SID 确认 |

不会再要求 SID 解释 STAFFTYPE、HEADCOUNTTYPE、SFLBDM 以实现首期准入，也不规划 HR/飞书主 resolver。Token 200 单独不是准入证明。

## 可执行的首期机制

`SourceIDEmployeeAdmissionResolver.Resolve` 统一返回 ALLOW / DENY / UNKNOWN、检查时点、有效期、契约引用及是否支持复核。

- 正式首期适配器只有 `authorization`。它必须收到 KnowHub 的单次授权事务证明：持久化 state 已消费，Token/RS256/Issuer/Audience/时间/nonce/UserInfo sub 均通过之后，业务服务才建立内存证明。直接调用 Token 接口成功、任意 UserInfo 数据或填入状态字段都不能替代此证明。
- 还需 `SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED=true`，此标志代表停用时效及已有 SSO 的重新准入边界已取得确认并纳入审批。当前不设置。
- `userinfo_claim`、`sid_query` 仅有合成适配器测试；默认业务依赖没有安装这些适配器，没有猜测接口或状态字典。
- DENY、UNKNOWN、过期、未来检查时间、契约/来源不匹配、超时或服务错误一律拒绝；适配器错误原文不向日志/浏览器传播。
- 适配器必须遵守传入 context；统一调用限制最多 5 秒，父 context 可以更早取消。

ALLOW 是一次有界准入决定，不是永久员工状态。无复核能力的授权准入不能 Refresh；本地会话到期只能重新授权，且只有 SID 确认已有 SSO 仍重新检查准入之后才具备放行条件。

## 已读证据

本轮在 Windows 正常证书校验下只读访问正式 Discovery，成功取得：Issuer `https://sid.ruijie.com.cn`，授权 `/oidc/authorize`、Token `/oidc/accessToken`、JWKS `/oidc/jwks`、UserInfo `/oidc/profile`。`claims_supported` 未提供员工准入时效或判断规则。Discovery 不能证明 knowhub 的员工准入策略、SSO 复核行为或停用生效时间。见 [公开元数据证据](evidence/phase-b-sid-admission-public-metadata-20261015.json)。

现有飞书契约调查及员工字段分析也未给出上述边界；不能从字段名称补全。既有真实成功 Trace 是历史证据，本轮未获取或还原真实声明值，未发起真实登录、Token 兑换或员工目录查询。

[OIDC Core 授权码流程](https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth)与本地应用会话是不同层次；跨系统登出另有协议，不能因完成 OIDC 就宣称具备统一登出。

## 给 SID 的最小问题清单

1. 员工停用后，所有服务节点最迟多久拒绝新的 knowhub OIDC authorize？
2. 浏览器已有 SID SSO 会话时，再次 authorize 是否仍执行同样的员工有效性检查？是否需要正式支持的额外参数？
3. 停用是否同时撤销已有 SID 会话？若否，其最长有效期是什么？
4. 停用前已签发但尚未兑换的 code 是否立即失效？否则最长存活与兑换限制是什么？

以上回答应标注适用的 knowhub 应用和生效时间。无需提供员工资料、密钥、Basic Header 或 Token。
