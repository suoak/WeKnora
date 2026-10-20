# SourceID prompt=login 强制重新认证实现

2026-10-16。状态：实现及隔离候选构建完成，**NOT_DEPLOYED / NO_REAL_SID_LOGIN**。最终 CI 和镜像收据见本文验收节。

## 1. 最新契约与证据边界

以下作为 USER_CONFIRMED 产品输入：SID支持prompt=login，忽略既有SSO会话重新认证；无效员工重新认证时拒绝授权；不支持max_age=0。员工停用状态存储于数据库，SID说明不存在数据不一致。**该说明没有提供传播时延SLA，D仍为UNKNOWN，不能写D=0。**

Code有效期C=90秒；停用前已签发的未过期Code仍可兑换。这是已确认的残余风险，不能把Token200解释为兑换时员工仍有效。SID普通SSO约8小时的配置尚未最终核实；本方案不使用该时长作为KnowHub本地会话期限。

RJGH稳定、唯一、不回收，RJEMAIL为当前员工的可信企业邮箱均已确认，不再列为待确认身份事实。全角色首次自动关联继续复用既有实现，不重新开发、不按角色拒绝。

[OIDC Core 3.1.2及3.1.2.3](https://openid.net/specs/openid-connect-core-1_0.html#AuthRequest)规定prompt=login触发重新认证。本文采用SID已确认的prompt能力，不采用其不支持的max_age。是否提供可信auth_time与是否接受KnowHub员工准入是两个不同问题。

## 2. 配置与最小实现

新增 `SOURCEID_FORCE_REAUTH_PROMPT_LOGIN`，YAML字段 `sourceid_force_reauth_prompt_login`，默认false，配置JSON不可导出。仅 `SourceIDEnable` 的业务授权链路应用该选项；其他OIDC Provider不因该布尔值增加prompt。

| 文件 | 修改内容 |
| --- | --- |
| `internal/config/config.go` | 新字段与现有环境覆盖机制 |
| `.env.example` | 默认关闭示例；不填真实审批或期限 |
| `internal/application/service/user.go` | 统一授权URL注入、保存事务元数据；已批准local_approved+authorization缺少强制配置时拒绝开始；OIDC验证后关联授权证明 |
| `internal/application/service/sourceid_reauthentication.go` | URL去重、持久化请求事实、回调恢复、签名auth_time检查、固定枚举诊断 |
| `internal/application/service/sourceid.go` | 单次消费授权事务后检查强制请求证据；向准入解析器传递事实 |
| `internal/config/sourceid_local_session.go` | authorization本地会话策略必须同时满足强制认证、独立契约确认和无Refresh |
| `internal/application/service/sourceid_local_session.go` | 实际授权证明也必须包含prompt请求事实，不能仅检查配置声明 |
| `internal/types/sourceid.go` | 进程内准入请求增加布尔事实，不增加数据库列 |
| `docs/integration/sourceid/business-test/export_token_diagnostics.py` | 将新的固定枚举事件纳入原有严格白名单导出器 |

所有前端重新登录仍经过服务端 `GetOIDCAuthorizationURL`，不接收前端传入prompt作为策略。强制模式覆盖授权端点URL原有prompt，包括重复prompt=none/consent，最终只有一个prompt=login。端点预置max_age时拒绝，不降级或发送SID不支持的参数。不开启该选项时保持兼容；它不会替任何模式填写审批引用或打开身份门禁。

强制请求事实（版本、prompt固定值、服务端请求创建时间）保存在现有 `oidc_transactions.result` 中。授权结果交换使用独立的result键空间，不与授权事务混用。单次消费时原有机制清除元数据及verifier。**无需数据库迁移，保持3027。**

旧版或开关启用前的事务，没有实际请求prompt的证据，不能在新强制策略下继续兑换并用于业务登录。失败后要求重新开始新的授权事务，不补发普通authorize。

## 3. auth_time及“请求”与“执行”的区别

强制模式仅检查**通过签名、Issuer、Audience、时间声明及nonce验证的ID Token**中的auth_time，且在UserInfo合并之前执行。UserInfo同名字段不能覆盖或制造该证据。

- 提供数字时间时，必须是整数秒，不早于本地强制请求创建时间的秒级下界，也不能在当前时间之后。非法类型、过旧或未来值拒绝。
- 没提供时，记录 `NOT_PROVIDED_CONTRACT_REQUIRED`；不生成或补填SID认证时间，不标记加密新鲜度校验PASS。
- 有效签名auth_time记录 `SIGNED_AUTH_TIME_VERIFIED`，非法/过旧记录 `INVALID_OR_STALE`。只输出固定枚举和本地Trace ID，不输出时间声明值、员工信息或原Token。
- 历史已保存的公共Metadata摘要没有提供足以证明auth_time实际返回的证据。本轮没有读取真实Token，也未发起真实SID请求；SID实际auth_time行为仍为**NOT_VERIFIED**。

本地持久化prompt证明只能证明服务端构造了该授权请求，不能独立证明SID收到了它并执行了交互。若SID静默忽略prompt，且不提供可信auth_time，客户端不能凭Token200识别这一行为。此时只能依赖已正式确认并获安全批准的SID强制认证契约与受控联调，不能声称已完成独立新鲜度验证。当前运行环境没有被配置为确认或放行。

两项控制共同生效：实际prompt事务事实 + 独立的 `SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED` 及政策审批。新参数支持不等于自动批准此开关。缺少有效准入证明/审批时，不建立本地Session。

## 4. 授权、准入与会话安全边界

流程为：服务端策略 → 签名state/nonce及单次持久化事务 → prompt=login授权 → callback校验 → 一次Token兑换 → 严格ID Token → UserInfo sub一致性 → 独立准入及会话策略 → 复用RJEMAIL/RJGH绑定 → 原user_id本地Session。

SourceID首期authorization + local_approved模式必须满足：

- 原protocol_only/contract/email/auto-binding及政策引用门禁已正式批准；没有默认全开。
- `SOURCEID_FORCE_REAUTH_PROMPT_LOGIN=true` 且本笔事务确实由强制模式生成。
- 准入重新检查契约已确认，本地政策和准入政策引用真实有效。
- `SOURCEID_LOCAL_REFRESH_ENABLE=false`，有效Access/Absolute/Idle/Admission期限均显式批准。

本地Session使用现有随机ID和原账号权限。过期后新登录仍走相同授权构造器；既有SID Cookie不构成免重新认证的理由。刷新页面不修改Session绝对期限；Refresh不能续期；SwitchTenant沿用既有Session及合法租户权限，不创建更长期限。退出、管理员撤销和绑定撤销沿用既有失效机制。SID统一登出仍是第二期能力。

oauth_form、Token正文、callback、PKCE、state/nonce、RS256、Issuer/Audience及原时间声明校验未修改。原严格签名分类/JWKS诊断继续保留，未为偶发signature错误放宽验证。其他OIDC Provider和原邮箱密码登录保留。

## 5. 90秒Code及残余风险

本地state事务从请求创建开始计时，原有效期10分钟；SID Code从**SID实际签发**开始计90秒。两者不是同一个时间点。不能用state时间推算精确Code签发时间，也不能把本地事务仍有效当作Code仍有效。

回调通过原有原子消费机制防并发和重放；消费后才兑换，标准请求一次发送，不在400/401或校验失败后重试。现有安全HTTP诊断继续记录callback到发送的耗时及实际请求次数；保持现有HTTP超时，不额外队列、串行尝试参数或复用Code。90秒到期由SID校验，过期时本地安全失败，不尝试“抢救”Code。

设获批本地绝对期限为L，停用传播上限为D，Code最长寿命C=90秒，Token接受至本地Session建立的处理时间为P。保守风险评估至少考虑 **D + C + P + L**。这不是已测得的SID SLA：D未知、P未作为整体截止期限获批，不能承诺固定最大窗口。

10分钟L候选下，C+L本身可达11分30秒，另加D/P。Idle期限或提前退出可缩短实际可用时间，但不能作为最坏上限。签名auth_time检查有助于发现旧SSO认证，不会让已签发Code在员工停用后自动失效。本地准入证明期限与Session期限的原有较小值约束继续生效，但不能倒推兑换时员工仍有效。

Mock明确演示：停用前生成的有效Code仍可换到Token，并在**仅合成审批的授权策略**下建立短Session；新prompt认证则被拒绝。这是待审批的残余风险演示，不是“即时撤销已实现”的证明。真实环境始终未放行。

## 6. 合成与CI验收

功能提交：`c24c1cf21abc6da9d34c60c26a152952c5eefaf8`。最新90秒风险测试提交：`479416c9086d91de4484dbbd2cc5f912cdcdb697`，候选基于此完整版本。

新增或增强测试：

- SourceID首次授权、到期后新授权均只有一个prompt=login；无max_age；默认关闭及非SourceID均不追加。
- 持久化请求事实、旧事务不能升级冒充、callback重复消费拒绝。
- 已有Mock SSO Cookie仍执行重新认证；停用员工与不支持prompt拒绝，Token请求次数不增加。
- Provider忽略prompt并返回旧签名auth_time拒绝；UserInfo不能修复该证据；缺少auth_time且审批未完成时不创建Session。
- 普通账号与system_admin+owner+跨租户标记首次及再次关联保留原user_id/tenant/角色；完整既有全角色、冲突、停用、撤销、并发测试继续运行。
- 90秒过期Code、已消费Code无Session；有效停用前Code的残余风险、重复callback不发第二次Token请求；无Refresh续期。
- 既有无效state/nonce/签名和身份冲突回归；真实Vue+Mock SID浏览器登录及失败回调回归；其他OIDC、本地登录、MCP和飞书回归。
- 脱敏导出只接受新鲜度固定枚举，拒绝额外auth_time、subject或任意内容。

Windows配置测试及Python导出测试已通过；Windows完整service测试受既有DuckDB/MinGW链接ABI问题阻塞，没有将它记为PASS。Linux固定打包基线及GitHub CI承担完整服务验收，不更换系统编译器来掩盖问题。

固定打包基线 Go1.26.7、network=none、race 开启的6个包专项测试全部通过：config、service、handler、repository、middleware、utils。服务二进制构建成功。真实Vue+Mock SID浏览器CI也已通过；这些测试没有真实凭据。

[最终CI四项全部通过](https://github.com/suoak/WeKnora/actions/runs/38840870012)：linux、postgres、frontend、browser。已下载并核对目标SHA及构建证据完整性；专项测试320条、完整认证/MCP回归4830条、飞书组合测试248条通过记录（包含子测试），失败均0。完整CI还通过server及管理工具构建、PostgreSQL迁移/并发/会话测试、前端类型/回调/构建和真实Vue+Mock SID浏览器验收。

机器证据见 [CI汇总](evidence/prompt-login-ci-20261016.json)。前一个仅功能提交的CI因加入90秒测试后的新提交被取消，最终结论依据最新479416c90完整运行，不将取消运行标为成功。

候选 APP revision：`479416c9086d91de4484dbbd2cc5f912cdcdb697`。

候选 image ID：`sha256:85f9d6c052298e7f52e7d7c4533e131ce19326d461521378e90c6df3b01fa49c`。

候选镜像仅已构建并核对OCI revision，尚未替换默认APP，也未做部署后的真实SID认证验证。

## 7. APP-only发布与回滚——仅准备

本轮没有修改运行服务或门禁，没有迁移、切库、员工登录或真实绑定。当前默认APP仍035f35e1d、Frontend1ba24ef5f、DB3027、oauth_form。

候选采用已验证离线Go1.26.7/运行镜像基线，只替换候选二进制。首次镜像封装把sha256 image ID误识别为Docker Hub仓库而失败；合成测试及二进制构建未失败。修正为先校验本地基线image ID，再用本地别名封装，保持同一基线和pull=false，没有更换镜像源。

已生成权限0600的候选及回滚pin：

- `/home/aqyfzx/ai/knowhub-prompt-login-479416c90/prompt-login-app-pin.yml`
- `/home/aqyfzx/ai/knowhub-prompt-login-479416c90/prompt-login-app-rollback-pin.yml`

候选pin固定不可变镜像并仅增加默认false的强制认证配置传递；原旧镜像已确认仍可用。回滚pin仅固定旧APP，回滚执行时恢复原APP环境，不能遗留试验开关。

获单独部署批准后：备份受保护配置与容器元数据，核对默认035/前端1ba/DB3027；保留现有4层Compose顺序，最后追加仅包含APP目标镜像的prompt pin；只对APP执行 `up -d --no-deps --no-build --pull never --force-recreate app`，不触碰Frontend/数据库/Redis/DocReader或文件卷。

新选项默认false。发布本身保持原企业门禁；将来联调启用必须通过受保护运行配置显式传入，并实测服务端生成的授权URL确含prompt=login。

失败时只用APP rollback pin恢复035f35e1d原不可变镜像及原APP配置，保留oauth_form、Frontend1ba、DB3027、所有数据卷和原门禁。不执行数据库down或整体服务栈回滚。强制模式试验前排空授权事务；撤回新能力前关闭试验入口、撤销试验Session并丢弃在途事务，不能静默降级已有授权流程。

## 8. 最小真实联调方案与待审批事项

先独立批准候选APP部署；无真实身份的健康/配置/失败回调回归通过后，再单独批准一笔真实协议联调。

第一笔仅验证强制认证：保持protocol_only与身份写入门禁，启用受控诊断及prompt选项。由本人操作经验证到打包机的浏览器；已有SID SSO Cookie情况下确认SID确实重新认证，SID负责人按时间/安全Trace核对员工准入执行。新Code只兑换一次，记录Token、签名、nonce、UserInfo及auth_time存在性/安全分类；不保存Code、Token、Cookie、工号或邮箱。任一失败立即停止，不自动重试。协议阶段仍不创建业务Session。

后续业务验收需另行批准：全角色自动关联范围、账号归属及流量隔离、真实测试账号和原权限基线、local_approved政策引用、明确Access/Absolute/Idle/Admission期限、无Refresh、退出/撤销负责人和步骤。10分钟仍只是候选值，不是本轮批准值。

SID无需重复确认prompt支持、SSO绕过规则或90秒Code已知行为。仍需补齐：D的可承诺上限/测量依据；实际是否提供签名auth_time及其语义和时钟一致性证据。普通SSO约8小时配置可归档核实，但不能替代本地会话审批。

安全评审还需明确接受停用前Code有效的C窗口、P的整体处理期限或可接受边界、D未知时是否允许任何真实业务试验及隔离要求。未批准前保持 `SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED=false`、protocol_only=true、contract_confirmed=false、auto_binding=false、local_approved未启用。最终真实OA放行另行授权。


## 9. 本轮机器证据

- [候选构建收据](evidence/prompt-login-candidate-status-20261016.json)
- [最终CI与具体合成测试](evidence/prompt-login-ci-20261016.json)
- [离线基线竞态测试](evidence/prompt-login-build-tests-20261016.json)
- [APP-only发布与回滚准备](evidence/prompt-login-release-preparation-20261016.json)
- [默认运行服务及门禁不变](evidence/prompt-login-live-unchanged-20261016.json)
- [首轮镜像封装失败摘要](evidence/prompt-login-build-attempt1-20261016.json)
