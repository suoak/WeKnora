# 单笔真实 OA 业务登录准备状态

更新日期：2026-10-17。本次不执行真实认证、真实绑定或全员开放。普通 SSO、全角色统一关联与 Access-only 路径继续复用。正式发布一次性向公司全员开放，不限制部门/组织/角色或人数；取消事业部范围及组织字段校验要求。新员工JIT必须在正式发布前完成真实验收，不作为上线后的分批补充。独立环境保持f40b1602f/DB3029、Frontend832b6652a及关闭的门禁，详见[技术验收](SOURCEID_JIT_PILOT_FINAL_TECHNICAL_ACCEPTANCE_REPORT.md)与[最新READY报告](KNOWHUB_REAL_OA_BUSINESS_ACCEPTANCE_READY_REPORT.md)。

## 正式目标配置与依赖

| 项目 | 当前保持 | 未来目标及条件 |
|---|---|---|
| OIDC_AUTH_ENABLE | false | 单独真实验收批准后才开启 |
| SOURCEID_FORCE_REAUTH_PROMPT_LOGIN | false | 继续 false，普通 SSO；不要求强制 auth_time 证明 |
| SOURCEID_BASIC_ENCODING | oauth_form | 保持，不修改凭据或认证构造 |
| SOURCEID_PROTOCOL_ONLY | true | 获批业务验收窗口才关闭 |
| SOURCEID_CONTRACT_CONFIRMED | false | 有真实身份与业务策略批准后启用 |
| SOURCEID_EMAIL_BINDING_ENABLE / EMAIL_CONTRACT_CONFIRMED | false / false | 使用已确认可信 RJEMAIL，按正式批准加载，无需重新确认邮箱可信事实 |
| SOURCEID_AUTO_BINDING_ENABLE | false | 按统一全角色策略批准开启，不按管理员分流 |
| SOURCEID_AUTO_BINDING_POLICY_REF | 未加载正式业务批准 | 必须是真实审批引用 |
| SOURCEID_SESSION_MODE | external | 获批后 local_approved |
| SOURCEID_ADMISSION_MODE | oidc_sso_approved，但门禁关闭 | 另需 SSO_POLICY_CONFIRMED 与真实 LOCAL/ADMISSION 政策引用 |
| SOURCEID_ACCOUNT_SCOPE | pilot 默认 | enterprise_all；不需要逐人试点 JSON |
| SOURCEID_ENTERPRISE_POLICY_CONFIRMED / POLICY_REF | 未开启 / 未加载 | 批准权威历史归属及来源后加载 |
| SOURCEID_ENTERPRISE_MAX_AGE | 未加载 | 结合来源周期、传播与恢复预算正式确定 |
| SOURCEID_LOCAL_REFRESH_ENABLE | false | 继续 false，不签发/保存/返回 Refresh |
| SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED | 不开启 | 普通 SSO 不借用未经验证的授权复核声明 |
| SOURCEID_LOGOUT_ENABLE / JIT_ENABLE | false / 默认 false | SID统一登出继续关闭；JIT须在正式一次性开放前完成候选部署、真实验收及独立政策批准 |

Access/Absolute/Idle/AdmissionMaxAge 的 60m/60m/30m/60m 仅为候选。企业负责人须明确同意普通 SSO 的停用残余风险、这些期限、统一自动关联范围、撤销负责人和真实政策引用。普通 SSO 复用 SID 会话不代表每次检查在职，本地退出不等于 SID 统一退出。

## 剩余最少条件

1. 数据负责人提供可独立核验的原账号历史归属批次及增量契约；批准受保护 dry-run/apply。没有证明的账号统一拒绝，不按角色区别处理。
2. 批准上述会话与归属新鲜度参数，确定真实政策引用与故障值班。不能用 Mock 的 synthetic 引用替代。
3. 确认被测真实账号在干净试点库中的原 user_id、角色、成员和原知识库权限；不合并默认验收库 viewer 与原管理员，不创建重复账号。
4. 当前试点入口仅服务器 loopback 18090；日常 Chrome hosts 指向服务器 80 时进入默认项目，**不能用于本次独立试点验收**。需单独批准可保留 SSO 且不改全局网络的浏览器/隧道路由方式，并在该实际上下文确认页面、静态资源、API、原获批 callback 均进入独立试点。无凭据临时浏览器预检不能替代日常 Chrome 上下文证明。
5. 明确一次真实业务验收授权、受控来源与时间窗口、SID/业务负责人协作，以及单笔事务的服务端脱敏监测和恢复计划。

6. JIT候选f40与003029已完成独立技术验收；仍须核实SID应用实际允许的企业账号类型、批准公司级开户资格及完整精确邮箱域集合。现有配置支持多域，不需要组织字段门禁开发。单独授权真实新员工开户、重复登录及退出/撤销的具体事务数量；技术部署不等于批准真实JIT开户。新账号无默认管理/空间/知识权限，公共知识按共享ACL访问。

7. 正式生产容量需另取全公司用户数、集中登录峰值、资源与SID限流输入；原300–500人只是旧规划，历史4300成功/100预期拒绝保留，不能作为全公司容量PASS。

## 一笔业务验收操作卡（获独立批准后）

1. 核对独立项目f40b1602f、DB3029、Frontend832b6652a、同代文件及全部备份；记录原账号/权限私有基线，确认没有并行登录。
2. 权威批次 dry-run 无冲突，经批准 apply 后，核验记录有效截止与政策一致。导入只写归属证据，不提前伪造 OIDC sub 或身份绑定。
3. 在网络限制已实际验证的独立入口加载批准配置。业务开关为全局策略，不能仅说“一个人测试”就假定其他人无法触发；保留受控来源和服务器端归属准入。
4. 用户只点击一次 OA，完成 SID 认证并返回 KnowHub。Codex 监测 authorize/callback/Token/ID Token/nonce/UserInfo/关联/Session，失败不重试。
5. 核对 Portal、auth/me 原 user_id、原管理标记、所有空间成员、原 KB 文档读取、真实存在且无授权的对照资源拒绝；SwitchTenant 不扩大权限或延长期限，无 Refresh。
6. 本地退出后旧 Access 拒绝，定向撤销及会话到期行为按批准用例验证；再次真实登录须包含在授权范围内，不自动追加。
7. 恢复 protocol_only 和绑定门禁，撤销本轮企业 Session/派生 Token，保留绑定/审计证据；不靠恢复旧 dump 抹除真实业务写入。异常时仅 APP 回滚至09e6d86db、保留 DB3029/Frontend/文件。

**当前结论：尚不满足立即开展真实业务登录的条件。** 技术候选部署与合成验收不替代真实归属数据、正式策略和实际浏览器路由批准。
