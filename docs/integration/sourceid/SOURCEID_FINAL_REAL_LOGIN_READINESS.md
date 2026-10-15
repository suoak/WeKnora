# OA 一键登录最终业务验收准备

日期：2026-10-15。状态：**CODE_DEPLOYED / REAL_BUSINESS_ACCEPTANCE_BLOCKED_BY_CONTRACT_AND_APPROVAL**。

阶段 B 代码已部署，不需要重新构建或迁移。本轮仅只读核查，没有创建账号/知识库、改变配置或发起 SID 登录。可以安排 SID/安全/产品评审会议；尚不能安排放行真实员工账号的业务登录。

## 1. 已满足的条件

| 项目 | 依据与状态 |
| --- | --- |
| RJGH 稳定、唯一、不回收 | **USER_CONFIRMED**，正式设计输入，不再作为待确认阻塞；书面证明如发布制度要求，只归档证据 |
| RJEMAIL 为 SID 验证且属于当前员工的企业邮箱 | **USER_CONFIRMED**，可作为首次可信邮箱匹配依据 |
| 无效员工在 OA authorize 阶段被 SID 拒绝 | **USER_CONFIRMED**；不依赖猜测的 UserInfo 状态字段 |
| SID Token / ID Token / nonce / UserInfo | 有历史真实成功证据；本轮没有新的真实事务 |
| 邮箱自动匹配、RJGH 长期绑定、异常保护 | 已开发并通过合成验证；不重复开发 |
| local_approved 短本地会话 | 已开发、合成验证、部署；未批准启用 |
| 打包机版本/迁移/基础业务 | **DEPLOYED**，431226c3b、3027 clean；见[实际部署报告](SOURCEID_PHASE_B_PACKAGING_DEPLOYMENT_REPORT.md) |
| SessionIndex | 首期 local_approved 不依赖它；第二期统一登出仍未完成，不伪造外部会话标识 |

核查时间：北京时间 **22:21:32**。实际 app/frontend 完整 revision 均为 `431226c3b1bd5338ec06c1bd0520e637d5fed398`，app healthy，80/8080 保持原端口。[只读现场收据](evidence/final-real-login-readiness-readonly-20261015.json)。

## 2. 最少需要 SID 确认的三项

| 问题 | 必须取得的具体答案 | 为什么影响放行 |
| --- | --- | --- |
| 停用传播上限 D | 从员工停用生效时间起，所有 SID 授权节点/缓存最迟多久拒绝新的 knowhub authorize？ | 没有上限就无法量化停用后风险窗口 |
| 已有 SSO 的再次授权 | 浏览器已有 SID SSO 登录态时，每次 knowhub authorize 是否仍重新检查员工准入，失效时是否拒绝发 code？ | 若 SSO 可绕过检查，短 KnowHub 会话也能反复续接，不能放行 |
| 未消费 code 的失效边界 C | 授权码有效期、停用后是否主动失效、若不主动失效还能存活多久；兑换时是否再次检查准入？ | 停用前签发的 code 可能在停用后创建新本地会话 |

请 SID 给出版本/适用范围、时效上限和证据位置，无需员工数据或 Token。不再要求确认已明确的 RJGH/RJEMAIL，也不再索要 STAFFTYPE/HEADCOUNTTYPE/SFLBDM 字典或首期 HR/飞书 resolver。

Token 200 不能单独作为员工有效证明；KnowHub 必须使用本次成功完成 state、签名、nonce、issuer/audience/时间及 UserInfo sub 校验的单次 OIDC 事务，结合获批的 SID authorize 准入契约，生成准入决定。

## 3. 首期方案 A 的待审批内容

候选值只是讨论输入，**不是已批准配置**：[完整审批材料](SOURCEID_LOCAL_SESSION_SECURITY_APPROVAL.md)。

| 项目 | 审批候选/代码约束 | 待签署内容 |
| --- | --- | --- |
| Access TTL A | 例如 10 分钟；必须显式配置、不能大于绝对期限 | 实际批准时长 |
| 本地绝对期限 L | 例如 10 分钟；不滑动续期 | 实际批准时长与可接受风险 |
| 空闲期限 I | 不大于 L；例如不超过 10 分钟 | 实际批准时长 |
| 准入结论最大可用期 M | 不大于 L；例如不超过 10 分钟 | 与 SID 时效契约一致的上限 |
| Refresh | authorization 准入模式强制禁用 Refresh API 的续期能力 | 同意到期重新 OIDC，不采用长期 Refresh |
| 本地退出 | 撤销当前设备本地 Session/派生 Token | 明确不等同退出 SID |
| 绑定/管理员撤销 | 定向会话撤销；绑定撤销覆盖该身份各设备，不能自行自动重绑 | 操作人、权限、响应 SLA、审计与恢复流程 |
| 长连接/正在执行请求 | 新请求可检查会话，不能直接声称已开始的流立即终止 | 流/任务最大持续时间及撤销边界 |
| 自动关联范围 | 唯一邮箱匹配、有效普通账号，冲突及高权限拒绝 | 策略引用、审批人、有效范围及复审时间 |

会话实际到期取绝对期限与有界准入期限的较早值；Access 受服务端会话约束，空闲检查不能延长绝对期限。Refresh API 不可延长会话；现有客户端兼容结构仍可能包含同期限的本地 refresh 记录，供 SwitchTenant 单次轮换使用，**不是可长期续期的 Refresh**。SwitchTenant 必须沿用相同 Session 和绝对截止时间，并验证原账号合法成员关系。

风险估算：停用后最长访问窗口保守按 **D + C + L** 加请求/流处理及允许时钟误差预算评估；这是待参数确认的估算，不是已实测 SLA。如果已有 SSO 可以绕过准入复核，则上述有限上限不能成立，方案 A 也不得启用。

审批必须填入真实的会话策略与 SID 准入证据引用，不能用占位审批号或单纯将 confirmed 设 true 替代审批。SID 跨系统统一登出仍为第二期。

## 4. 测试库账号与权限现状

本轮 SQL 使用 READ ONLY 事务，只查询聚合数量，不查询员工值或 Token。

| 项目 | 当前实际数量 |
| --- | --- |
| 用户 / 有效用户 | 1 / 1，均为现有保留域名合成账号 |
| system admin / 跨租户管理员 | 0 / 0 |
| 租户 / active 成员关系 | 1 / 1 |
| owner / admin / contributor / viewer 成员关系 | 1 / 0 / 0 / 0 |
| 符合普通账号自动绑定权限条件的候选 | **0** |
| 规范化邮箱重复分组 | 0；不能据此证明尚未录入的员工邮箱可以匹配 |
| 有效知识库 | **0**；9 条总记录均已软删除，包含上次验收已清理的合成夹具 |
| 外部身份 / 审核映射 / SID Session / 本地企业 Session | 均为 0 |

因此现有 owner **不能**拿来验证普通员工无感自动绑定；不降低 owner 权限，也不把它当普通用户放行。源码 `ordinarySourceIDAutoAccount` 会排除 system/cross-tenant admin、owner/admin/异常成员关系、相关组织 admin，且需要有效普通 home-tenant 成员关系。

## 5. 待单独批准的普通账号与知识库准备

本节是操作方案，不是已执行的数据写入。

1. 由用户/负责人批准一个测试员工、目标测试空间、contributor 或 viewer 角色、测试数据范围与清理责任人。企业邮箱只在受保护终端/管理界面输入，不放入聊天、URL、命令参数、CI、Git 或报告。
2. 优先选择已有普通邮箱账号；准备器在受控进程内检查规范化邮箱唯一匹配、账号 active、无历史绑定冲突和高权限成员关系，仅输出数量/通过结果。若不存在，再单独批准创建；OA 回调自身不得自动创建用户。
3. 新建时使用现有“加入既有空间”的受控邀请注册/账号准备流程，使 home tenant 和 active contributor/viewer 成员关系正确建立。先审查该流程的日志与默认 provisioning 行为；**不能直接走会创建个人空间 owner 的默认注册，然后仅给另一个空间补 contributor**，否则仍不符合自动绑定策略。当前无 system admin，不能假装已具备管理员创建接口权限或擅自给 owner 提升系统权限。
4. 不修改全局注册策略或原 owner 权限来迁就一个测试账号。若现有界面无法安全完成 tenantless/受邀加入流程，提交限定该新账号的受审准备器方案与 dry-run，再等待数据写入批准；不临时直接改历史用户行。
5. 原 owner 在测试空间创建不含员工资料的知识库及明确的非空合成文档，授予批准的最小权限；由测试普通账号验证读/编辑边界。viewer 与 contributor 的验收范围应分别列清，不把租户角色和具体知识库/共享 ACL 混为一谈。
6. 用受保护的基线清单记录原 user_id、home/active tenant_id、各 active 成员角色、知识库归属及共享 ACL、允许/拒绝的读写请求。真实邮箱/RJGH 不写普通报告；用户/空间标识与对应关系仅保留在获准访问的受保护清单，报告使用匿名测试编号与比较布尔值。
7. 完成合成账号演练与真实账号归属核对后，记录“账号准备通过”，保持 OIDC 身份门禁，等待正式单次真实业务验收授权。账号准备、放行配置和真实登录是三个不同批准动作。

首次绑定应在事务与唯一约束下保留原 user_id、tenant_id 和成员/知识库权限。后续按 issuer + RJGH 找到同一 user_id，同时核对已验证 sub；邮箱变化不重新匹配其他用户，sub/RJGH 冲突不得自动迁移。

## 6. 配置放行清单：当前全部保持原样

| 配置 | 当前 | 允许变更的前提 |
| --- | --- | --- |
| SOURCEID_PROTOCOL_ONLY | true | 下述外部契约、内部批准、测试账号及单次真实验收授权完成后才可 false |
| SOURCEID_CONTRACT_CONFIRMED | false | 汇总已确认身份及获批准入/会话契约，不能泛指“Token 已成功” |
| SOURCEID_EMAIL_BINDING_ENABLE | false | 邮箱匹配与冲突保护验收、批准配置变更 |
| SOURCEID_EMAIL_CONTRACT_CONFIRMED | false | RJEMAIL 契约已经满足；可在获批的成套变更中确认，不是外部阻塞 |
| SOURCEID_AUTO_BINDING_ENABLE | false | 普通账号自动绑定策略签署、目标账号符合条件 |
| SOURCEID_AUTO_BINDING_POLICY_REF | 本轮未读取值，也未修改 | 正式策略归档引用，不能虚构 |
| SOURCEID_SESSION_MODE | 未设置，默认 external | 会话方案 A 安全批准后才可 local_approved |
| SOURCEID_LOGOUT_ENABLE | false | 首期保持 false，第二期另行审批 |

审批后还须核对 `SOURCEID_CLAIMS_SOURCE=userinfo`、`SOURCEID_EMPLOYEE_CLAIM=attributes.RJGH`、`SOURCEID_ADMISSION_MODE=authorization`，以及真实 LOCAL_POLICY_REF、ADMISSION_POLICY_REF、显式批准的四项时长、AUTHORIZATION_RECHECK_CONFIRMED 和 LOCAL_REFRESH_ENABLE=false。当前不填写或启用它们。无需为 local_approved 猜填员工状态或外部 SessionIndex 字段。

现场 `OIDC_AUTH_ENABLE=true`、`SOURCEID_DIAGNOSTICS_ENABLE=true`、oauth_form、PKCE=false、AUTO_MIGRATE=false 均沿用，严格的 state/nonce/RS256/Issuer/Audience/时间声明与 UserInfo sub 校验不变。登录入口可见不代表自动绑定或业务会话已经放行。

## 7. 获批后的一次真实业务验收

每一笔 authorize/code 事务需在授权范围内执行；不自动重试、复用 code 或为了验收连续登录。

| 验收项 | 操作与通过标准 | 仅保存的证据 |
| --- | --- | --- |
| 路由与配置 | 浏览器整个前端/API/callback 均到打包机，绕过干扰代理；正式 callback 不变；实际 local_approved 和审批配置正确 | 环境、版本、门禁与路由通过结果，不保存 Cookie/HAR |
| 首次普通员工 OA 登录 | 用户手动 SID 认证；state、单次 Token、签名/nonce、UserInfo sub 均通过 | Trace、阶段、耗时、HTTP 状态及安全失败类别 |
| 自动关联 | 首次可信 RJEMAIL 唯一命中准备好的普通原账号，事务建立 issuer+RJGH 长期绑定，sub 一致 | approved/冲突分类、原 user_id 是否相同，不记录邮箱/工号 |
| 本地会话及 Portal | 随机独立 Session ID；auth/me 200、Portal 正常；不是仅检查页面 200 | 会话模式、期限满足批准值、前端登录态结果 |
| 原权限 | 对照基线，原 tenant/角色/ACL 不变；允许请求成功，禁止请求保持 403/404 | 基线一致布尔值与各请求结论 |
| 到期 / Refresh | 实际等待批准的短期限，过期后访问 401；Refresh 被拒绝，不能延长 Session；不篡改系统时间 | 实测截止、拒绝类别；重新授权需单独计入许可事务数 |
| SwitchTenant（如该账号具备多个合法空间） | 同一有界 Session，只切入已有成员空间；非法空间拒绝 | Session/绝对期限一致、授权结果；单空间账号记 NOT_APPLICABLE |
| 本地退出 | 当前设备会话及派生 Token 失效，auth/me 拒绝；不宣称 SID 同步退出 | 撤销范围、退出及后续拒绝结果 |
| 管理员/绑定撤销 | 操作人按已批准流程定向撤销；绑定撤销使该身份所有设备失效、阻止自动重新绑定 | 受控审计引用及失效断言 |
| 第二次 RJGH 识别 / 邮箱变化 | 同一 user_id，不依赖邮箱重新匹配；真实邮箱不为测试而修改 | 优先用既有 Mock；真实第二笔登录必须另行授权 |
| 异常 | 无匹配/重复邮箱/高权限/冲突/停用/无效授权/不可用/nonce或签名失败均安全停止，不发 JWT | 以 Mock/专用合成夹具覆盖，不破坏真实员工账号来造故障 |

退出、到期、定向撤销和绑定撤销会互相影响同一会话，不能声称一笔真实事务可以独立覆盖全部分支。先完整复用已有合成证据，单次真实首验优先覆盖正常登录→原权限→本地退出；到期及撤销另用独立合成会话，或申请明确的后续真实事务授权。多设备隔离也不能靠一台设备的一次登录证明。

若首次 Token、ID Token、nonce、UserInfo 或绑定失败，立即终止该事务并保留脱敏阶段证据，不自动重试或放宽验证。SID 授权准入边界与真实停用测试只能在 SID 联合批准的专用账号上安排，不擅自停用普通员工。

收尾恢复验收前批准的安全门禁；不因验收通过永久开启真实自动绑定。保留身份/会话审计，需要清理新夹具时单独按明确 ID 操作，不删除历史账号或原知识库。

## 8. 当前登录页为何没有视觉变化

用户截图中的旧按钮文案与当前部署一致。`431226c3b` 仍使用 provider_display_name 组合“使用 企业 SID 登录”；此前“使用 OA 登录”、浅绿色次级按钮及分隔线优化仍是**本地未提交修改**，没有包含在本次获批版本。

Windows 绕过代理直连打包机，实际返回 `/assets/main-Dqj-SBfJ.js` 和 `Login-DwQVCxKj.js`；登录 chunk 包含旧 `oidcLoginWithProvider` / `redirectingToOIDC`，不包含新 `oaLogin`。[实际静态资源检查](evidence/frontend-oa-ui-release-status-20261015.json)。因此不能靠刷新把未发布的 UI 变出来，也不能据此否定业务版本升级成功。

本轮不混入未提交 UI/MCP 等工作区改动，不重新构建或部署。UI 优化需先整理为独立可验收提交，再按单独批准的前端发布流程处理，不影响本报告的身份安全门禁。

## 9. 何时可以安排真实登录

以下四组条件同时满足后，才提交具体时间安排并请求用户授权该次真实业务事务：

1. **SID**：三项准入时效/SSO/code 边界已答复并归档；确认再次授权不会绕过准入。
2. **内部安全/产品**：方案 A 具体期限、无 Refresh、风险窗口、自动绑定范围、撤销责任与审计要求正式批准。
3. **账号/数据负责人**：批准并完成普通邮箱账号、有效成员关系和非空测试知识库基线准备，未复用或降权原 owner。
4. **执行负责人/用户**：批准一次受控放行配置与真实登录，验证路由、当前有效凭据来源及回退门禁，现场有人处理撤销与脱敏证据。

目前已满足代码、部署、协议历史验证和 RJGH/RJEMAIL 身份事实；尚缺的是上述外部边界、内部批准及真实测试账号准备。**本轮没有新的真实 OA 登录、绑定或业务会话证据。**
