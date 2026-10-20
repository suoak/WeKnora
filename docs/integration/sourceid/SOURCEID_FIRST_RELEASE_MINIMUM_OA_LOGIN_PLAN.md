# KnowHub首期OA登录最小可用上线方案

2026-10-16。状态：**PLAN_READY / MINIMUM_POLICY_ADAPTATION_REQUIRED / NOT_ENABLED**。

首期采用普通OIDC SSO，prompt=login验收不再作为业务试点前置条件。所有角色统一自动关联原账号、创建有限本地会话并访问原知识库，管理员不另设申请或登录流程。身份归属检查与冲突拒绝对所有账号一致。能力覆盖全角色不等于已批准全员开放；本轮未修改认证代码、运行配置、账号、数据库或网络，未发起SID认证。最新配置及验收矩阵见[SOURCEID_ALL_ROLE_MINIMUM_BUSINESS_RELEASE_PLAN.md](SOURCEID_ALL_ROLE_MINIMUM_BUSINESS_RELEASE_PLAN.md)。

## 1. 结论与现有能力

普通SSO方向可行，但**当前APP479416c90不能仅通过打开开关实现新策略**。静态核对以下文件与部署提交479416c90无差异：本地会话策略、准入resolver、强制认证逻辑及绑定repository。

历史prompt=false两笔事务已通过callback、Token、ID Token及UserInfo，证明该环境普通协议链路曾成功；不证明本地业务会话或权限验收已完成。最近prompt=true测试只有authorize、没有callback，属于INCOMPLETE，不作为普通SSO阻塞。

当前可以复用：严格协议验证、RJGH长期唯一绑定、可信RJEMAIL首次唯一匹配、全角色权限保持、审核/撤销记录、随机本地Session、绝对/空闲期限、请求侧Session校验、退出与撤销。无需重写OIDC或邮箱关联。

普通SSO不要求每次重新认证；prompt是可选参数，认证新鲜度是另一项策略。依据：[OIDC Core认证请求](https://openid.net/specs/openid-connect-core-1_0.html#AuthRequest)。本方案不将普通SSO结果解释为“本次重新验证员工在职”。

## 2. 必须先处理的最小代码差距

| 代码位置 | 已确认行为 | 首期最小适配，尚未实施 |
| --- | --- | --- |
| `internal/config/sourceid_local_session.go:SourceIDLocalPolicy` | authorization模式强制要求recheck=true、prompt=true | 新增显式、默认关闭的普通SSO批准模式，保留旧authorization严格模式 |
| `internal/application/service/sourceid_local_session.go:sourceIDAuthorizationAdmissionResolver.Resolve` | resolver再次要求prompt及重新检查证明 | 普通SSO独立策略只接收本次完整验证的OIDC事务证明，不复用/伪造新鲜员工准入证明 |
| 同文件`resolveSourceIDAdmission` | 支持authorization/userinfo_claim/sid_query；后两种没有正式适配器 | 不伪造状态字段，不改用未实现的resolver来绕门禁 |
| `internal/application/service/user.go:generateTokensForTenant` | 即使LocalRefreshEnable=false，仍生成、保存并返回Refresh Token | 普通SSO首期只签发Access；对应repository持久化支持无Refresh分支，不影响本地密码登录及其他Provider |
| 同文件`SwitchTenant` | 本地企业Session要求Refresh predecessor | 无Refresh模式改由有效Access及同一Session授权切换；校验原合法成员关系，不延长绝对期限 |
| `frontend/src/App.vue`、`views/auth/Login.vue`、认证Store | 有Refresh时保存；需核查无Refresh登录与旧存储清理 | 新企业登录明确清理遗留Refresh；不自动续期、不因401重复发起OIDC或提前加载Portal |
| `sourceid_binding.go:reviewSourceIDBinding` | 自动策略已覆盖全部角色，没有试点人员白名单 | 不新增角色限制；试点范围校验先于候选写入，所有账号统一归属保护 |

新模式建议命名`SOURCEID_ADMISSION_MODE=oidc_sso_approved`，并新增默认false的`SOURCEID_SSO_POLICY_CONFIRMED`，与现有AUTHORIZATION_RECHECK_CONFIRMED严格区分。**均为拟新增配置，当前479416c90不支持，不是可直接粘贴执行的环境配置。**其“准入”仅表示本地试点接受已验证的SID身份，不宣称进行了实时员工状态查询。

正式策略引用应明确：接受SID原SSO身份复用、不要求auth_time新鲜证明、员工状态不持续复核、限定试点及撤销负责人。保留原prompt强制代码和严格authorization模式供第二阶段使用。预计复用3027会话表；如需新增策略元数据，先单独审查是否需要兼容迁移，不能预先宣称零迁移。

## 3. 风险与审批边界

RJGH稳定唯一不回收、RJEMAIL可信归属均已USER_CONFIRMED；不再作为待SID确认事实。SID原SSO会话不会持续检查员工状态已确认。

**本地60分钟上限只约束一笔KnowHub Session。**员工停用后，只要SID仍接受原SSO，用户可能再次普通authorize获得新本地Session。因此无Refresh也不保证停用后60分钟失效；不能用每次Token200或新nonce推断重新检查过员工状态。

SID SSO“约8小时”尚非已核实的硬上限；是否滑动续期、停用是否使SSO失效、状态传播D仍不明。未确认时，持续访问风险窗口UNKNOWN；不能给出D+90秒+60分钟的保证。90秒Code是已确认规则，停用前未过期Code仍可兑换；Code签发时间不等于state创建时间。

首期试点负责人可明确接受普通SSO的上述风险，无需先完成prompt联调，但必须批准受控人员、有限日历窗口、全体试点会话截止与紧急撤销。建议试点结束撤销本次会话且关闭入口；如需运行期间技术硬截止，应在新模式中增加服务端截止校验，不能只依赖人工定时操作。全员上线另行评审，不能将试点风险接受当正式发布批准。

可信企业邮箱不证明旧KnowHub账号历史归属一直属于当前员工。未绑定账号的历史邮箱回收/重分配，现有唯一邮箱和active检查无法识别全部情况。采用统一归属规则；归属不明、重复邮箱、停用、RJGH/sub冲突或撤销后自动重绑均拒绝，必要时仅异常进入核查。不得仅因管理员角色增加人工审批，也不能把“没有冲突记录”当作历史邮箱从未复用的证明。试点数据基线可统一确认账号归属，不是逐位管理员申请流程。

## 4. 目标配置及依赖关系

以下为**适配完成、合成验收及试点批准之后**的目标，当前保持原门禁。任何政策引用均必须填写真实批准记录，不填写示例审批编号。

| 配置 | 目标 | 前置条件 |
| --- | --- | --- |
| OIDC_AUTH_ENABLE / SOURCEID_ENABLE | true | 正式SID端点、受保护当前凭据、仅测试机路由 |
| SOURCEID_FORCE_REAUTH_PROMPT_LOGIN | false | 首期普通SSO，授权URL不额外追加prompt=login或max_age |
| SOURCEID_BASIC_ENCODING | oauth_form | 保留已收口配置 |
| SOURCEID_PROTOCOL_ONLY | false | 新策略实现、批准、账号及隔离验收齐备后才能改 |
| SOURCEID_CONTRACT_CONFIRMED | true | 只确认本方案实际使用的身份/会话策略，不冒充重新准入确认 |
| SOURCEID_CLAIMS_SOURCE | userinfo | 必须先通过ID Token与UserInfo sub一致性 |
| SOURCEID_EMPLOYEE_CLAIM | attributes.RJGH | 严格类型及非空，绑定同时核对issuer/sub |
| SOURCEID_EMAIL_CONTRACT_CONFIRMED | true | 已确认RJEMAIL可信，不代表自动绑定范围已批准 |
| SOURCEID_EMAIL_BINDING_ENABLE | true | 复用原账号，账号不存在拒绝、不开户 |
| SOURCEID_AUTO_BINDING_ENABLE | true | 全局风险已隔离；服务端限定试点并核准归属 |
| SOURCEID_AUTO_BINDING_POLICY_REF | 实际批准引用 | 一份覆盖所有角色的自动关联策略及统一归属规则 |
| SOURCEID_SESSION_MODE | local_approved | 随机本地ID，DB3027能力及撤销验证 |
| SOURCEID_ADMISSION_MODE | oidc_sso_approved（拟新增） | 完成上述最小适配，当前不可用 |
| SOURCEID_SSO_POLICY_CONFIRMED | true（拟新增） | 普通SSO风险与试点范围明确批准 |
| SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED | false | 没有重新准入证明，不为满足旧代码设true |
| SOURCEID_LOCAL_POLICY_REF / SOURCEID_ADMISSION_POLICY_REF | 各自实际批准引用 | 分别记录会话规则及普通SSO风险接受 |
| SOURCEID_LOCAL_REFRESH_ENABLE | false | 不仅拒绝Refresh接口，也不签发/保存/返回Refresh |
| SOURCEID_LOGOUT_ENABLE | false | SID统一登出第二阶段；本地退出仍必须有效 |
| SOURCEID_DIAGNOSTICS_ENABLE | 默认false，验收窗口受控true | 白名单、体积/访问/保留期受控，结束恢复原值 |
| AUTO_MIGRATE | false | 不在试点启动时自动迁移 |

协议保持Authorization Code、client_secret_basic、scope=openid、PKCE=false、原审批callback；state、nonce、RS256、Issuer、Audience、时间、UserInfo/sub等校验不变。不增加offline_access，不使用SID不支持的max_age=0。不配置猜测的状态字段或外部SessionIndex。

## 5. 会话期限候选与真实执行验收

| 参数 | 候选值，待负责人批准 | 说明 |
| --- | --- | --- |
| SOURCEID_LOCAL_ACCESS_TTL | 60m | 无Refresh时用60m避免10m Access提前使业务会话结束 |
| SOURCEID_LOCAL_ABSOLUTE_TTL | 60m | 服务端硬上限，Access不得超过剩余绝对期限 |
| SOURCEID_LOCAL_IDLE_TTL | 30m | 现代码为已认证API请求活动，不是鼠标/键盘操作；后台轮询可能维持活跃 |
| SOURCEID_ADMISSION_MAX_AGE | 60m | 新模式仅为OIDC身份接受快照的本地上限，不代表员工状态新鲜度 |
| Refresh | 不签发，续期拒绝 | 不影响原密码登录及其他Provider的既有Refresh |

现repository每次企业会话鉴权检查绝对/空闲/决策有效期、绑定/审核及撤销，成功更新LastSeenAt；SwitchTenant不得重置这些期限。Mock使用可控时间验证59/60分钟、29/30分钟边界，再用隔离运行测试核查数据库时间字段及实际401，不能只看配置字符串。

长流式回答当前不能仅凭入口鉴权保证到期瞬间中断。试点可明确批准：到期拒绝新请求，已开始流有明确最大时长；若要求即时终止则需补中途校验。该风险不能被“会话60分钟”掩盖。前端过期时清除本地认证并显示重新OA登录入口，普通SSO可能直接返回，不宣称完成重新准入。

## 6. 试点范围及入口隔离

全局AUTO_BINDING=true没有人员白名单。仅绑定本机hosts、约定“只测一个人”或仅过滤登录页面均不足以限制他人绑定。

建议双重限制，未经部署批准本轮只设计：

1. 服务端在关联写入前限制获批试点既有user_id集合，集合覆盖所有角色，不按角色拒绝；首次邮箱仅匹配集合内账号。受保护配置由录入工具维护，不把邮箱/RJGH写Git、CI或普通报告。统一确认试点账号历史归属；未知人或不匹配返回固定拒绝类别。不能用前端隐藏按钮实现限制。
2. 网络只允许获批测试来源访问Frontend80、APP8080，覆盖反向代理、直连、IPv6及Docker转发；保留内部代理和健康检查。必须从允许与禁止来源做实际验证。不能只凭防火墙规则存在判定隔离成功。
3. 原审批域名在试点浏览器局部路由至172.30.232.42，页面、静态资源、API、callback统一同源；SID HTTPS正常访问。禁止修改生产DNS/网关或访问生产KnowHub。hosts映射只负责选路，不负责准入。
4. 使用短日历窗口、负责人值守及原配置备份；监测单笔事务，发现混入其他流量立即停止关联，不误记PASS。业务验收只接收本次批准的账号，不读取Token或Cookie判断操作者。

## 7. 数据及账号准备

最近只读计数：默认验收库3027、用户2、绑定0、本地企业Session0；历史默认库有效KB为0。计数不能证明批准员工账号仍存在、角色正确或有可检索内容，启用前需受控定向复核，只输出存在/唯一/active/角色类别，不输出员工值。

第一步使用获批普通账号：若已存在，保留其user_id及成员关系；若不存在，单独批准后使用已设计tenantless单账号准备器加入既有测试空间viewer/contributor，不创建个人owner空间、不降低原owner权限、不改全局注册策略。

准备一份非空合成文档并完成解析/检索，以及另一个真实存在但完全未授权空间的对照KB。记录原user_id、tenant_id、成员角色、管理标记、KB/ACL基线于受保护验收记录；普通报告只给一致性结果。真实邮箱通过受保护渠道，不预先写SID身份。

管理员与原知识库验收采用**干净、另行批准的原库副本**。现成候选已迁移3027并通过文件读取及已有向量召回，但尚未正式切换；见[SOURCEID_LEGACY_CLEAN_TARGET_READINESS_REPORT.md](SOURCEID_LEGACY_CLEAN_TARGET_READINESS_REPORT.md)。该报告的035f35e1d是当时验收版本，不是当前运行APP。

复用前重新核对原数据未变化，否则冻结窗口重做数据库/文件同代快照。成套使用候选DB和文件副本，独立JWT/AES、撤销旧Session/Token/API/MCP Key，禁旧同步任务；不使用历史Mock绑定副本、不合并当前viewer与原管理员、不复制原卷可写挂载。保持管理员user_id、租户、全角色及ACL，模型/连接器需另核查加密及调用授权。已有向量召回通过不等于外部Embedding/完整RAG已验证。

在同一受控方案内覆盖普通及全部管理权限，不为管理员另建流程。原库副本的实际数据使用及部署仍按环境授权处理，与账号角色无关。不能为方便验收直接切换当前默认库。新SSO策略候选APP需对干净副本做兼容测试，不能沿用旧候选报告当新版本验收。

## 8. 操作步骤与通过标准

1. 批准新普通SSO会话及全角色自动关联策略、真实政策引用、覆盖全部权限类型的试点集合、候选期限/流式边界、人员和日历窗口；统一准备数据归属基线，不再等待prompt=login真实验收。
2. 开发仅限策略解耦、无Refresh及试点限定的最小改动。Mock/CI验证所有角色权限保持、所有旧严格OIDC用例、无Refresh/租户切换、重复邮箱/停用/冲突/撤销、超时及其他Provider不受影响。当前阶段未批准这一步部署。
3. 经独立批准准备候选环境/数据/网络，备份当前配置/镜像/DB及文件；实际验证允许与禁止来源。保持门禁关闭先做基础回归。
4. 独立批准后仅在试点目标加载第4节配置，实测进程及最终配置，禁止用RECHECK=true伪装新鲜度。启动脱敏业务验收监测，记录原基线。
5. 普通员工本人点击一次OA，允许SID复用SSO；state、Token、签名/nonce/UserInfo均PASS后，唯一邮箱关联原用户及RJGH绑定，生成本地Session，进入Portal，auth/me200。
6. 对比登录前后user_id、租户、成员角色及KB ACL；允许KB可读/检索，真实未授权对照KB拒绝，无重复用户/个人空间/权限提升。协议HTTP200不单独构成通过。
7. 另行批准下一笔登录，验证已绑定RJGH继续命中相同user_id；邮箱变化不改绑；再验证获批管理员权限完全保持。每笔Code仅消费一次，不因失败自动重试。
8. 验证无Refresh返回/存储/续期、页面恢复、合法SwitchTenant及绝对期限不变；非法租户拒绝。真实时间到期、闲置到期、本地退出和受控管理员撤销后，派生Access立即不能发新请求，原密码登录不受影响。
9. 结束关闭试点入口和业务放行，定向撤销试点Session/Token，保留审核与审计，核对权限及数据不变；形成逐项PASS/FAIL/INCOMPLETE记录。

本轮只检查方案及必要源码，没有声称上述新模式、无Refresh改动或业务E2E已经通过测试。

## 9. 快速回滚

先冻结新OA业务登录及自动绑定，保留本地管理员救援登录；定向撤销试点企业Session及派生Access，确认旧Access鉴权失败，再恢复原APP不可变镜像479416c90及受保护配置，Frontend1ba24ef5f不变。原状态为prompt=false、protocol_only=true、contract/email/auto-binding/recheck=false、Session默认external、oauth_form。

repository已有RevokeSourceIDSession及ReviewSourceIDBinding撤销能力；本轮没有证明存在可直接使用的公开管理API。上线前准备受控、定向CLI调用业务方法并dry-run，不能临时对全表SQL失效，也不能假设已有管理页面。需要撤销绑定时保留revoked审核及审计，避免自动重绑；仅撤销Session不会自动禁止下次建立Session，故须先关闭入口/策略。

不删除DB3027或数据卷、不降迁、不恢复raw、不整体回滚飞书。若试点涉及旧库，DB/文件成套切回，写入先冻结并保全新写入差异；切回前不得丢弃新数据。仅关闭protocol_only不能代替定向Token撤销验证。

## 10. 最少待批准项

- 试点负责人：批准普通SSO残余风险、60m绝对/30m空闲/无Refresh、API活动定义及已开始流处理；提供真实政策记录与撤销负责人，不虚构编号。
- 账号负责人：统一确认覆盖全部权限类型的原账号数据基线、批准受保护准备及必要数据写入；管理员不另设申请，身份归属异常按同一规则拒绝。
- 运维负责人：批准候选APP-only发布、试点服务端限定与实际网络隔离；原库副本使用/成套切换另批，当前默认服务保持不变。
- SID负责人：可补充原SSO最大寿命、是否滑动续期、停用是否失效及D，以量化普通SSO风险；不再重复询问RJGH/RJEMAIL，不将prompt测试作为首期业务试点必做项。无这些值时如接受试点，必须明确风险窗口UNKNOWN，不能宣传实时停用。

最短路径：批准新普通SSO策略及有限试点 → 最小适配与Mock/CI → 单独批准发布/数据/隔离 → 普通员工闭环 → 获批管理员及原KB → 退出/过期/撤销。二期再做强制重新认证、可信auth_time、实时准入/撤销和统一登出。
