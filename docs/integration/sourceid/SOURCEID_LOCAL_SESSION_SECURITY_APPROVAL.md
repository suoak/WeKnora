# SourceID 本地企业会话安全审批材料

状态：IMPLEMENTED_GATED / APPROVAL_REQUIRED。本文不是批准记录；候选时长未写入正式环境。

## 优先方案 A：短会话，无 Refresh

候选值仅供审批：Access 10 分钟、本地绝对期限 10 分钟、空闲期限不超过 10 分钟、准入结果最大可用期不超过 10 分钟。所有时长必须显式配置，没有默认批准值。Session 实际期限取绝对期限与准入期限的较小值。

授权阶段准入模式强制关闭 Refresh。到期后本地 Access 及会话失效，必须重新发起 OIDC；不能使用原准入结论持续续期。为兼容现有客户端，仍可产生同期限的本地 refresh 记录用于切租户轮换，但 Refresh API 明确拒绝它，不能用于延长会话。切租户必须消费当前前驱，只能在原绝对期限内发新令牌。

前提：SID 明确确认停用生效延迟、已有 SSO 时重新授权确实重新检查准入、未消费授权码失效边界。缺少这些条件时，不能因本地时长短就宣称停用风险已被控制。

风险窗口：若 SID 状态传播上限为 D，停用前 code 还能存活 C，KnowHub 本地会话上限为 L，则保守评估上限为 D+C+L，另计正在执行请求/流的最长持续时间。若已有 SSO 可绕过后续准入，重复授权可能无限续接，本地短 TTL 本身不足；当前该项 UNKNOWN，阻止启用。

## 候选方案 B：可复核的本地 Refresh

候选值：Access 10 分钟、绝对会话 8 小时、空闲 30 分钟、准入新鲜度上限 60 秒。仅供未来审批，不作为首期默认。

需要 SID 正式独立复核或可信撤销机制。已提供统一 resolver/Mock 及有界续期结构，但没有正式 SID 查询适配器。

- 每次 Refresh 必须进行可复核准入校验；DENY、UNKNOWN、超时、不可用均拒绝并撤销该本地会话。
- 新准入期限不能超过审批新鲜度，绝对会话期限不滑动。每次业务认证检查本地 Session、绑定、绝对/空闲/准入期限。
- Refresh 单次消费并轮换。重放撤销同设备整个令牌族；两个并发 Refresh 只允许一个提交，另一笔按重放处理，客户端必须合并并发刷新。
- 此方案必须另行确认切租户、长期请求、WebSocket、后台任务以及停用事件的撤销边界。当前不允许真实启用。

## 保留的控制

- Session ID 由服务器 UUID 随机产生，外部 SessionIndex 字段为空；不使用 RJGH、sub、邮箱或 jti 代替。
- 本地 Session 绑定已批准 ExternalIdentity、issuer、subject、user_id、审批引用及准入决定；访问时重新确认绑定仍有效。
- Access JWT 的 session_id 必须与服务端记录一致。没有 session_id 的原本地登录令牌不能被当作企业会话。
- 退出当前设备只撤销该设备 Session；管理员定向撤销、绑定撤销使相应 Session 及派生 Token 失效。绑定撤销覆盖该身份所有设备，禁止自行重新自动绑定。
- 权限由原账号/合法成员关系决定，不创建或提升角色。
- 本地退出不会注销 SID；SID Back-channel Logout 为第二期，保持关闭。

定向 Session 撤销当前是受控内部仓库能力，已有合成断言；本轮没有新增公网管理接口。现有 `sourceid-binding` 审核工具的 revoke 可由管理员撤销绑定并覆盖该身份会话。上线审批时还需明确具体操作人、权限和操作手册，不能把内部测试调用当作已经验收的管理页面。

## 审批应签署的内容

选择 A/B、Access/绝对/空闲/准入期限、SID 延迟及 SSO 边界证据、Refresh 策略、允许自动关联的普通账号范围、审批/契约引用、日志访问与保留、撤销操作负责人、长连接风险及复审时间。

正式配置继续保留 protocol_only=true、contract_confirmed=false、auto_binding_enable=false、logout_enable=false；session_mode 默认 external。任何配置启用、真实员工数据写入、真实登录和服务器迁移/部署另行批准。

## 审批后的配置核对模板（当前不得照抄启用）

```text
SOURCEID_SESSION_MODE=local_approved
SOURCEID_ADMISSION_MODE=authorization
SOURCEID_LOCAL_POLICY_REF=<已签署的本地会话审批编号>
SOURCEID_ADMISSION_POLICY_REF=<SID准入与SSO复核契约归档编号>
SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED=<上述边界获得正式确认后才能为true>
SOURCEID_LOCAL_ACCESS_TTL=<审批时长>
SOURCEID_LOCAL_ABSOLUTE_TTL=<审批时长>
SOURCEID_LOCAL_IDLE_TTL=<审批时长>
SOURCEID_ADMISSION_MAX_AGE=<审批最大可用期>
SOURCEID_LOCAL_REFRESH_ENABLE=false
```

这组值并不会替代 SOURCEID_CONTRACT_CONFIRMED、邮箱契约和自动绑定政策门禁。SOURCEID_STATUS_CLAIM、SOURCEID_ALLOWED_STATUS、SOURCEID_SESSION_CLAIM 在本地授权准入模式无需填入猜测值。所有真实放行开关仍需在单独批准的变更中统一核对。
