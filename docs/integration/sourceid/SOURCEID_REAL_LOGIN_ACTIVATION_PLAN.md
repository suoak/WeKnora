# KnowHub 真实 OA 业务登录最小启用计划

> 后续更新：2026-10-16已按用户追加授权准备1个普通viewer，未启用业务门禁。下文账号缺失为00:06只读快照；最新账号、合成测试、CI及网络预演见[实现报告](SOURCEID_SINGLE_ACCOUNT_PREPARER_IMPLEMENTATION_REPORT.md)。资源夹具、SID边界、安全审批及实际流量隔离仍未就绪。

更新：2026-10-16（北京时间）。状态：**READ_ONLY_CHECKED / ACTIVATION_BLOCKED**。

本轮只读访问打包机 `172.30.232.42`，未改开关、未创建账号、未执行迁移或真实登录，未操作正式生产服务器。APP `431226c3b`，Frontend `1ba24ef5f`，DB `3027 / dirty=false`，80/8080 正常。不需要重做阶段 B、重建镜像或重复迁移。

## 1. 当前提示的准确代码原因

已对照运行镜像 OCI revision 与 `git show 431226c3b`，不把未发布工作区代码当作现场。相关代码：

| 位置 | 判断与结果 |
| --- | --- |
| `internal/application/service/user.go:549–557`，LoginWithOIDC | exchangeOIDCCode 成功后调用 resolveOIDCUserInfo；随后 `SourceIDEnable && SourceIDProtocolOnly` 返回 `sourceid identity contract pending`，尚未调用 loginSourceID |
| `internal/handler/auth_sourceid.go:151`，oidcPublicError | 将上述内部错误映射为 `identity_contract_pending`；若邮箱契约、自动绑定策略、准入或本地会话策略缺失，也映射为同一公开类别 |
| `frontend/src/App.vue:145` | 将该类别显示为用户看到的“OIDC 协议验证已完成……” |
| `internal/application/service/sourceid.go:61` | 即使跳过上一层，protocol_only 或 contract_confirmed=false 仍拒绝绑定 |

**现场 SOURCEID_ENABLE=true、SOURCEID_PROTOCOL_ONLY=true，因此 protocol_only 是直接原因；只改这一个开关仍不能登录。** 前端提示是共享的安全错误文案，不能单凭它认定本次所有阶段 PASS，亦不能证明发生了身份审核操作。

实际顺序：

1. authorize 生成并保存短期授权事务（会产生协议事务记录，不是业务身份绑定）。
2. callback 验证 state、消费持久化授权事务并关联 nonce。
3. Token 单次交换。
4. resolveOIDCUserInfo：要求签名 ID Token、RS256/JWKS/Issuer/Audience/时间声明校验、nonce 校验；按配置获取 UserInfo 并核对 sub。
5. **protocol_only 门禁：当前在此停止。**
6. 放行后才进入 loginSourceID：总体身份契约 → 提取 RJGH → 本次有效授权准入证明及策略检查 → 已绑定身份查找/首次 RJEMAIL 匹配及绑定。
7. 创建随机本地企业 Session、生成受 Session 约束的原用户 Token，回调结果经前端消费后进入 Portal。

所以当前并未执行阶段 B 的 RJGH/RJEMAIL 业务解析、自动关联或会话签发。SID 授权阶段的准入由 SID 实施；KnowHub 的准入证明与时效策略校验在业务绑定之前执行，不把任意 Token200直接视为在职证明。

## 2. 本次 Trace 与数据证据边界

采集时间：北京时间 **2026-10-16 00:06:47**；[本次只读收据](evidence/real-login-activation-readonly-final-20261016.json)。日志仅在服务端内存中筛选已知阶段/类别，未落盘原始日志、Token、Cookie 或员工字段。

| 项目 | 本次能确认的事实 |
| --- | --- |
| 用户所述事务 | 当前可用日志中未找到能与这次操作唯一关联的完整 Trace；用户确认准确时间未记录、暂不能提供可靠范围。因此不猜测时间、不关联其他事务、不用历史成功替代本次证据 |
| 保留的阶段记录 | 仅找到 10月15日20:17:24 的 state 拒绝记录；不是用户所述“协议完成”的成功事务，不能混用 |
| 本次 Token、ID Token、nonce、UserInfo | **NOT_CAPTURED / 不能逐项标 PASS**。代码路径表明走到 protocol_only 前需要完成相关调用，但这属于代码推导，不是本次 Trace 取证 |
| 诊断限制 | 日志明确出现 `sourceid_diagnostics_limit / window_expired`；现有诊断每进程30分钟、10MiB预算，开关 true 不等于仍持续输出完整诊断 |
| 当前业务持久化状态 | external_identities=0、binding_reviews=0、source_id_sessions=0、source_id_local_sessions=0，无现存绑定或企业会话 |
| 用户及权限 | 用户1、active1、owner成员关系1、普通候选0，与前次只读聚合一致。当前分支不进入用户/绑定/Session写入代码 |

据代码分支及空表可确认当前协议门禁不创建企业用户、绑定或企业 Session；但没有完整的本次 Trace、行级前后快照及审计证据，不能声称已经从数据库审计排除过所有瞬时写入或其他操作者的用户修改。协议事务的保存/消费是预期行为，不等于业务账号写入。

下一次获批验收应在受控 APP 配置加载后、30分钟诊断窗口内开始；不扩大日志预算、不自动重启或重复登录。若再次缺少完整关联证据，停止验收并标记未采集，不能补造。

## 3. 当前配置与所有剩余条件

下表是 Docker 运行环境白名单核查；NOT_SET 表示未显式设置，不擅自声称等同审批完成。未读取完整环境变量或秘密。策略引用只核对存在性。

| 项目 | 当前现场 | 启用前条件 |
| --- | --- | --- |
| SOURCEID_PROTOCOL_ONLY | true | SID边界、审批、账号及单次执行批准齐备后才能 false |
| SOURCEID_CONTRACT_CONFIRMED | false | 汇总正式准入及本地会话政策后才可 true |
| SOURCEID_EMAIL_BINDING_ENABLE | false | 邮箱关联范围及冲突保护批准后 true |
| SOURCEID_EMAIL_CONTRACT_CONFIRMED | false | RJEMAIL事实已确认；成套变更中落实，不再要求外部重确认 |
| SOURCEID_AUTO_BINDING_ENABLE | false | 获批普通账号范围、唯一匹配、无冲突后 true |
| SOURCEID_AUTO_BINDING_POLICY_REF | 不存在 | 使用真实签署归档编号，不能用示例值 |
| SOURCEID_SESSION_MODE | NOT_SET，代码默认 external | 获批后 local_approved；否则仍会要求外部Session/状态契约 |
| SOURCEID_ADMISSION_MODE | NOT_SET | authorization，配套 SID 边界证据 |
| SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED | NOT_SET | 取得已有SSO重新authorize确实复核的正式答案后才能 true |
| SOURCEID_CLAIMS_SOURCE | userinfo | 保持 |
| SOURCEID_EMPLOYEE_CLAIM | 空 | 配置 attributes.RJGH；字段事实已确认，不是新增外部阻塞 |
| SOURCEID_LOCAL_ACCESS_TTL | NOT_SET | 填正式批准时长 |
| SOURCEID_LOCAL_ABSOLUTE_TTL | NOT_SET | 填正式批准时长 |
| SOURCEID_LOCAL_IDLE_TTL | NOT_SET | 填正式批准时长 |
| SOURCEID_ADMISSION_MAX_AGE | NOT_SET | 填正式批准最大可用期 |
| SOURCEID_LOCAL_REFRESH_ENABLE | NOT_SET | 显式 false；不能只依赖未设置值判断无长期Refresh已验收 |
| SOURCEID_LOCAL_POLICY_REF | 不存在 | 本地短会话安全批准真实引用 |
| SOURCEID_ADMISSION_POLICY_REF | 不存在 | SID准入/SSO/时效边界归档引用 |
| SOURCEID_LOGOUT_ENABLE | false | 首期保持 false，SID统一登出第二期 |

OIDC_AUTH_ENABLE=true、SOURCEID_DIAGNOSTICS_ENABLE=true、SOURCEID_BASIC_ENCODING=oauth_form、PKCE=false、AUTO_MIGRATE=false 均保持；本轮没有修改。

### 已确认，不再阻塞

- RJGH 稳定、唯一、不回收；RJEMAIL 经 SID 验证且属于当前员工：USER_CONFIRMED。
- SID 在授权阶段拒绝无效员工：USER_CONFIRMED。
- 绑定、本地Session、撤销、合成业务登录和3027迁移已实现并部署。

### 仍需 SID 的三个最小答案

1. **D：停用生效到所有节点拒绝新授权的最大延迟是多少？** 明确起算点和缓存范围。目前 UNKNOWN。
2. **已有SSO时，每次authorize是否重新检查准入？停用后旧SSO是否还可取得code？** 目前 UNKNOWN，不能设置 recheck_confirmed=true。
3. **C：停用前签发未消费code的有效期和停用失效规则是什么？兑换时是否复查准入？** 目前 UNKNOWN。

### 内部安全及业务审批

首期选择短本地会话、无Refresh；Access/绝对/空闲/准入四个具体期限、自动绑定范围、审计保留、撤销与值班负责人均须签署。10分钟只是候选，不写成已批准值。已确认SSO再检查的前提下，保守停用风险为 D+C+本地会话上限，另计在途长请求/流和时间边界；D/C未知就不能给出已批准风险窗口。

local_approved 无需 SID SessionIndex，不以 RJGH/sub/邮箱/jti 伪造。其本地退出与 SID 统一登出独立；第二期未完成不阻塞首期本地退出，但也不能宣传同步退出SID。

## 4. 普通员工账号准备：只读 dry-run

现场：用户1（合成账号）、owner1、contributor0、viewer0、普通自动绑定候选0；有效知识库0（历史表总数9均非有效）。没有真实员工邮箱可在本轮合法匹配，具体邮箱匹配结果为 NOT_TESTED。

最小受控准备方案，真实写入另批：

1. 用户批准一个测试企业邮箱、已有测试空间、contributor/viewer角色、知识库范围及数据清理负责人。邮箱通过保护终端/管理界面输入，不能进入命令行、日志、Git、CI或报告。
2. 首先在保护进程只读精确匹配规范化邮箱：唯一active普通账号、无高权限/冲突才可复用。当前聚合无普通候选，不能用owner降权代替。
3. 如无账号，另批一个限定账号的 tenantless 准备操作，复用业务服务创建账号并加入已有空间，不创建个人owner。当前 registration_mode=invite_only，现有邀请注册只允许 self_serve/invite_register，因此不能直接声称邀请链接可注册；如改走邀请注册，注册策略调整也必须另批，不静默全局开放。
4. 原owner准备一个非空合成知识库与一项禁止访问的对照资源，授权最少权限；不得改历史owner/系统权限。
5. 保护清单记录原user_id、home/active tenant、成员角色及知识库ACL；公共报告只输出前后一致性布尔值。
6. 对照普通账号保护条件，包含其他空间/组织管理权限、重复邮箱、撤销记录及绑定冲突；任何一项失败均不放行。

本轮准备结果是 **ACCOUNT_AND_KB_PREPARATION_PENDING_APPROVAL**，不是已创建。首次自动绑定不创建不存在的用户。

## 5. 哪些改配置，哪些需要开发

| 内容 | 结论 |
| --- | --- |
| 从protocol_only进入已实现业务链路 | 满足契约、审批和数据条件后，采用配置变更；无需重写OIDC/邮箱绑定/Session |
| RJGH路径、authorization模式、无Refresh、四项时长及引用 | 配置与正式审批；不是新算法开发 |
| 普通账号及知识库 | 受控数据准备；若现有界面不能tenantless准备，需评审限定账号准备器或另批注册流程，不冒充现成功能 |
| 限定“只一个员工验收” | 自动绑定开关是全局策略，不是单账号白名单。必须确认仅批准账号满足普通候选条件，并控制维护窗口/访问范围；无法保证时另审账号级限制，不能声称已有白名单 |
| 撤销 | 已有内部能力和绑定审核工具；真实操作人、权限、定向会话撤销手册仍需验收，不能把内部Mock调用说成管理页面 |
| 偶发signature失败 | 保留现有细类与严格验签；SID查签名/JWKS节点一致性；本轮不改认证算法，不重试 |

当前未发现需要重写阶段B核心才能进入Portal的缺陷；不以此声明所有真实场景已验收。

## 6. 获批后的完整最小配置模板

**仅供审批，不是可直接执行的配置；含占位符，当前不得应用。** 必须使用真实批准引用和具体期限，不从本文生成假编号。四项期限按代码要求为至少1秒的整秒 duration，Access/Idle/AdmissionMaxAge不能超过绝对期限。

```text
SOURCEID_ENABLE=true
SOURCEID_PROTOCOL_ONLY=false
SOURCEID_CONTRACT_CONFIRMED=true
SOURCEID_CLAIMS_SOURCE=userinfo
SOURCEID_EMPLOYEE_CLAIM=attributes.RJGH
SOURCEID_EMAIL_BINDING_ENABLE=true
SOURCEID_EMAIL_CONTRACT_CONFIRMED=true
SOURCEID_AUTO_BINDING_ENABLE=true
SOURCEID_AUTO_BINDING_POLICY_REF=<真实批准的普通账号自动绑定策略引用>
SOURCEID_SESSION_MODE=local_approved
SOURCEID_ADMISSION_MODE=authorization
SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED=<SID正式确认后才允许true>
SOURCEID_ADMISSION_POLICY_REF=<准入时效及SSO边界真实归档引用>
SOURCEID_LOCAL_POLICY_REF=<短本地会话安全批准真实引用>
SOURCEID_LOCAL_ACCESS_TTL=<批准值>
SOURCEID_LOCAL_ABSOLUTE_TTL=<批准值>
SOURCEID_LOCAL_IDLE_TTL=<批准值>
SOURCEID_ADMISSION_MAX_AGE=<批准值>
SOURCEID_LOCAL_REFRESH_ENABLE=false
SOURCEID_LOGOUT_ENABLE=false
```

保留 oauth_form、scope=openid、PKCE=false、Bearer GET、原批准callback及state/nonce/RS256/Issuer/Audience/有效期校验。不要填虚构状态字段或SessionIndex。配置通过不代表有合格账号或真实事务成功。

## 7. 单次启用、验收与回滚步骤（本轮未执行）

1. 收齐SID三项答案、内部签署、账号数据写入批准和本次单笔真实登录/配置批准。指定现场操作者和撤销负责人；不足就停。
2. 保护备份当前配置与容器元数据，记录所有实际开关、DB及权限基线，保留现有镜像。受控运行进程仅核对凭据来源状态，不向Agent读出Secret。
3. 验收仅限批准测试员工；其他用户不得并行测试。应用获批的成套配置，只加载APP，保持APP/Frontend现有镜像、DB及其他组件不变。Compose依次为 compose.yml、candidate-image-pin.yml、frontend-ui-pin.yml，避免旧pin恢复前端。
4. 核对新策略实际加载及健康状态，在30分钟诊断窗口内验证浏览器登录页/API/callback都进入打包机同一真实业务实例，绕过干扰代理；不访问生产，不录HAR。
5. 用户点击一次OA并手动认证。只使用新code且兑换一次。Token、签名、nonce、UserInfo任一失败立即停止；401也不改参重试。保存统一Trace、时间、阶段、HTTP元数据及安全失败类别。
6. 通过标准：SID授权准入通过 → OIDC逐阶段PASS → 可信RJEMAIL唯一命中基线普通账号 → issuer+RJGH绑定并核对sub → local_approved会话 → Portal → auth/me200且为原user_id；原tenant、角色、ACL不变，允许知识库可读、越权知识库仍拒绝；不创建重复用户、不提升权限。
7. 本次真实会话优先验证本地退出后原Token/auth/me拒绝。到期拒绝、Refresh禁止、绑定撤销、管理员撤销及第二次RJGH识别复用已有独立Mock或另批真实事务；不能声称一次登录同时独立覆盖互相破坏的全部分支。
8. 成功或失败均先停止入口放行，恢复保护备份中的企业门禁：protocol_only=true、contract_confirmed=false、email/auto_binding=false、session_mode恢复未设置/default external、logout=false；新增策略项恢复原状态。OIDC/诊断按批准的结束基线恢复，不误称原来关闭。
9. **只恢复开关不足以证明已发会话失效。** 按本次受保护会话/身份清单定向撤销本地企业Session及派生Token，检查auth/me拒绝；若撤销了身份需确认禁止自动重绑。保留审计/绑定记录，清理账号或知识库须另批；不删除数据库/卷。
10. 核对APP健康、Frontend、DB3027及其他组件/权限不变，关闭专用浏览器和隧道。配置验收失败恢复备份及原APP镜像；不回滚数据库或前端，不恢复raw。

## 8. 用户与SID管理员最少需要确认什么

**SID管理员**：给出D、已有SSO再授权复核规则、code停用/到期边界C；同时按 [历史验签材料](SOURCEID_ID_TOKEN_SID_HANDOFF.md) 核查偶发signature，不需提供真实Token或员工数据。

**用户/内部负责人**：签署短会话四项值、无Refresh、普通账号自动绑定范围及真实政策引用；指定撤销和审计责任人；批准一个普通测试账号/知识库的数据准备路径；最后另批一次配置放行与真实登录。

当前主要剩余工作是契约归档、审批、受控数据准备及配置启用，不是重复基础开发。满足以上条件后可以安排一次真实业务验收；本报告本身不构成放行批准。
