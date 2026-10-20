# 可信SID企业邮箱自动关联：最小调整方案

2026-10-17。基线为已部署 `f40b1602f69ce5d88e1e87dc3cefae36e9bde356`；相关配置、service与repository源码与当前工作分支一致。本轮是设计、源码核查及隔离合成验证，未实现新的生产准入适配器，未部署、未apply、未真实认证。用户提供的首次测试邮箱只作为既有私有账号句柄的选择依据，不写入本报告、Git或测试数据。

## 决策与边界

可以设计成**公司一次性批准企业邮箱关联政策，员工正常首次OA登录自动关联**，不要求每位员工提交ownership JSON或逐人审批。普通用户与所有管理员均使用同一策略、同一事务及相同冲突保护。

可信RJEMAIL证明当前企业身份所拥有的邮箱，不自动证明历史账号从未转让。OIDC明确不保证email等声明长期唯一；标准稳定主体依据为issuer与sub。本项目额外采用已确认稳定且不回收的RJGH，绑定后同时核验sub，邮箱只用于首次匹配。[OIDC Core 5.7](https://openid.net/specs/openid-connect-core-1_0.html#ClaimStability)、[5.1](https://openid.net/specs/openid-connect-core-1_0.html#StandardClaims)。

对**没有任何可用历史记录的邮箱重新分配**，仅凭当前SID身份与唯一邮箱无法自动区分新旧员工。这是政策选择的残余账号接管风险，尤其可能继承旧管理员权限。不能同时承诺“不用历史信息”且“技术上排除所有历史转让”。可用一次性公司级邮箱分配/回收规则确认、历史异常集中清单和负向风险标记替代逐人证明；已知转让或无法接受风险的异常账号拒绝，其他员工不逐人审批。不得将“数据库未发现冲突”记录成“已证实历史归属”。

## f40现状：为何不能只删一处检查

| 层 | 当前实际行为 | 最小调整点 |
|---|---|---|
| config/sourceid_enterprise.go | enterprise_all要求enterprise政策与EnterpriseMaxAge | 将账号关联依据与公司范围配置解耦，严格校验新增政策模式 |
| service/sourceid.go | 企业分支先查独立ownership；JIT独立处理 | 经全部OIDC及准入验证后，按关联依据分派；不能借其他旧分支绕过 |
| repository/sourceid_enterprise.go | Resolve、CreateSession均要求ownership行 | 新邮箱政策专属入口，不改旧函数含义、不造ownership行 |
| service/sourceid_local_session.go | Access及SwitchTenant每次检查ownership | 同一新政策下校验有效绑定及本地Session；旧模式仍查原证明 |
| repository/sourceid_binding.go | 已有全角色唯一邮箱、review、绑定及撤销事务 | 复用现有核心；补充已知归属冲突、软删除邮箱历史、并发邮箱写入保护 |
| repository/sourceid_jit.go | 历史账号不能在归属失败后降级JIT | 按相同新政策适配“无历史用户”判断；有历史/冲突永不JIT兜底 |

只调用 `ResolveSourceIDAutoEmailIdentity` 不足以构成新方案：它不查询enterprise ownership的disabled状态，现有Session/Access仍要求ownership，因此不能直接接入上线。

## 建议配置（尚未编码或加载）

保留 `SOURCEID_ACCOUNT_SCOPE=enterprise_all`，新增独立 `SOURCEID_ACCOUNT_LINK_MODE`：

- `trusted_ownership`：默认值，保持f40原独立归属数据、Freshness及撤销语义。
- `verified_email_policy`：仅SourceID支持；须显式设置 `SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED=true` 和非空真实 `SOURCEID_VERIFIED_EMAIL_POLICY_REF`。默认不启用；未知值、缺引用、策略不完整一律拒绝，不回退。

新模式仍要求protocol_only关闭的单独授权、contract/email/email_contract/auto/SSO/local等原有批准及严格协议校验。prompt=false、Access-only、无Refresh；不增加任何管理员专属流程。JIT继续独立开关、独立公司级身份与精确邮箱域政策，首个已有账号验收保持JIT=false。

不把policy_ref写成独立source_ref或ownership_ref。BindingReview/BindingAudit记录“依据获批邮箱政策自动关联”，不记录“人工审核通过”。建议小型兼容迁移：review增加明确的link_mode，local_session增加link_mode/link_policy_ref；原记录默认trusted_ownership。ReviewReference继续存真实批准引用。版本及模式必须持久化，避免切模式后旧Session借新配置被错误接受；最终迁移须单独审核后才执行。

新模式的EnterpriseMaxAge和valid_until**不再伪装成逐人历史证明有效期**。本地Session受批准的Access/Absolute/Idle/Admission期限约束，旧ownership模式仍使用原Freshness。已有目录归属disabled/revoked信息不能因切模式失效；已存在但过期/冲突的权威归属记录应阻断自动处理，等待数据修复，不自动重写或忽略。

## 首次关联事务

1. 只能接受服务端已验证的OIDC事务结果：state一次消费、Token/ID Token签名、issuer、audience、时间、nonce、UserInfo sub一致；从可信UserInfo严格提取RJGH/RJEMAIL/sub。不接受客户端提交身份字段替代。
2. 先查issuer+RJGH、sub及已绑定用户。三者一致且账号启用、绑定有效则返回原user_id；邮箱变化不触发新匹配。已有身份换sub/RJGH、撤销或异常均拒绝，不自动迁移。
3. 未绑定时规范化RJEMAIL（trim+lower，保持现有ASCII邮箱规则，不做别名/模糊匹配）；查询包括软删除用户的历史记录。必须唯一命中一个未删除且active账号。
4. 在首次review写入前核对现有review、enrollment、external identity、ownership及已批准异常标记：员工、sub、user_id或历史邮箱冲突、已知重分配、disabled/revoked、来源异常全部拒绝。保留所有角色相同规则。
5. 事务内锁定候选用户，再复查邮箱唯一性与状态，复用review审核/identity/enrollment唯一约束及审计。为防非合作的密码注册/邮箱更新并发插入，在没有全局规范化邮箱唯一约束时，复用现有JIT的PostgreSQL users短表锁策略；与JIT统一锁顺序（users→review/identity），避免锁反转。只将首次绑定串行化，已绑定正常登录不取表锁。
6. identity、enrollment、批准review及审计原子提交；失败不留下pending脏关联。Session创建再次锁定approved review，与撤销串行；用户/tenant/member/角色/ACL全不改，不创建个人空间或提升权限。
7. Session/Access/SwitchTenant按持久化link_mode及真实政策版本复核绑定、账号和本地会话状态。模式/政策版本变更要求重新认证，不能重置绝对期限。退出、管理员撤销、绑定撤销使派生Access失效。

## JIT与异常

无任何历史账号/邮箱痕迹且无RJGH/sub/归属/review冲突，才可进入原JIT创建逻辑：真实公司级准入、精确批准邮箱域、普通tenantless账号、无管理员/成员/KB权限。历史邮箱已存在但停用、删除、歧义或关联失败时，不能通过JIT另建账户绕过。已绑定账号无需因邮箱变化重新开户。现有JIT的ownership表示其真实开户事件，不得拿来补造旧账号历史证据。

## 审计与审批

一次公司级审批需明确：SID issuer/client及可信RJEMAIL契约、适用账号及域规则、邮箱分配/回收制度或未记录转让风险接受、异常处理负责人、政策版本与生效时间、会话期限及撤销责任。无需每人签字，不分普通/管理员，但风险接受要覆盖可能继承高权限旧账号这一实际影响。

受保护审计存内部绑定/用户引用、link_mode、政策版本、时间、固定判定类别及撤销事件；不保存Token、Code、Cookie或完整UserInfo。普通报告只输出计数/固定分类，不输出RJGH、邮箱及真实员工材料。异常可用受保护批次集中维护，不变相恢复全员人工白名单。

## 验证与真实测试准备

本轮新增[五项合成设计探针](../../../internal/application/repository/sourceid_verified_email_design_test.go)，在隔离SQLite、无网络的既有Go构建环境运行f40仓储代码；同时执行已有全角色、重复邮箱、停用、冲突及撤销测试。两个“成功返回旧账号”的风险探针用于证明限制，并非安全控制已通过：未记录邮箱转让不可识别；直接绕过enterprise层会漏掉disabled ownership。

实际结果：[隔离运行收据](evidence/verified-email-design-mock-20261017.json) PASS，Go1.26.7、race检测、12个顶层测试（含子测试共30个PASS），失败0；离线构建归档校验及6个核心文件一致性通过。无网络、无真实数据库访问、无真实SID请求、无运行APP变更。本轮未执行PostgreSQL并发实测或新模式浏览器E2E，不把此前其他模式的CI当作本方案已通过；完整适配后仍需新增验证。

当前未实现配置分派、Session/Access适配、兼容迁移或完整Mock SID浏览器闭环，不能将仓储Mock结果冒称新模式业务PASS。正式实现后还必须验证：默认关闭、旧ownership兼容、新模式首次/再次全角色权限、12路首次并发、非合作邮箱写入竞争、Session撤销竞态、无Refresh及SwitchTenant、JIT不得绕过历史异常、策略切换失效、严格OIDC失败分支。

下一次真实测试前：审核并实现上述最小候选→隔离完整Mock/PG/浏览器回归→单独批准候选测试机部署及兼容迁移→公司级邮箱关联政策与会话批准→只给一笔已有账号测试打开窗口，JIT关闭。届时无需为每位员工导入ownership JSON，也无需给管理员另设流程；仍不自动登录或放开现有门禁。

首笔A正向验收验证原user_id、系统管理员/owner权限、Portal/auth-me、原KB及文档、退出和撤销；B跨空间负向验收单独记录。缺B不伪称B为PASS，A独立运行仍由测试负责人明确批准。结束先存脱敏证据，恢复门禁，定向撤销本次Session，保留账号/绑定/审计。回退旧版本时先关业务门禁并撤销新模式Session；旧f40没有ownership行的绑定将安全拒绝，不能假称回退后仍可继续OA登录。
