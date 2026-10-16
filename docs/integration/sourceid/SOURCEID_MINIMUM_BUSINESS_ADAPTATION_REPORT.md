# OA首期最小业务适配：候选实现及验收

2026-10-16。**CODE_AND_SYNTHETIC_CI_PASS / PILOT_DEPLOYMENT_NOT_EXECUTED**。

最终候选`832b6652adf541892545a7eac8b68733f846ab95`，分支`feature/knowhub-sourceid-oidc`；[最终CI四项通过](https://github.com/suoak/WeKnora/actions/runs/38871919491)。本轮未部署、启用业务门禁、切库或发起真实SID登录。默认APP479416c90、Frontend1ba24ef5f、DB3027保持原状。

## 1. 实现

| 模块 | 本次变更 | 仍保留的边界 |
| --- | --- | --- |
| M1 | 新增`oidc_sso_approved`，显式`SOURCEID_SSO_POLICY_CONFIRMED`及`local_approved`批准才能创建本地Session | 默认关闭；完整OIDC事务证明、身份合同、policy引用、期限和试点清单缺失拒绝；旧authorization仍要求prompt/recheck |
| M2 | LocalRefreshEnable=false的本地企业模式真正只签发/保存/返回Access；Refresh拒绝；SwitchTenant校验有效Access、原成员权限及同一Session | 绝对到期不滑动；Access不超过Session/身份接受快照期限；退出和撤销复用既有机制 |
| M3 | 复用全角色关联；新增角色无关的受保护归属清单与repository作用域 | 首次review/身份/映射写入前限制，锁定批准时再次核对；不创建用户/空间，不修改角色/ACL |
| 前端兼容 | 新登录无Refresh时清理旧Refresh内存及localStorage | 密码登录和其他Provider有Refresh时正常保存；回调/Portal策略不改 |
| M4准备 | 打包机原库/干净副本/备份/卷只读核查；提供独立试点操作卡 | 未启动副本或候选APP；未改变网络规则，不把历史验收当新候选实测 |

核心位置：`internal/config/sourceid_local_session.go`、`sourceid_pilot.go`；service的`sourceid_local_session.go`、`sourceid.go`及`user.go`；repository的`sourceid_binding.go`与`sourceid.go`。前端改动只涉及App/Login/router持久化Refresh及Store清理，不修改OA入口样式或OIDC协议。

协议不变：oauth_form/client_secret_basic、scope=openid、无PKCE、原callback、state/nonce、RS256、Issuer/Audience/时间、UserInfo/sub。普通SSO不要求prompt或auth_time新鲜度，也不把成功Token当员工实时有效证明。新模式不能回退到external模式或借Mock resolver放行；旧强制认证模式保留。

没有新增数据库表/迁移，继续3027；`SourceIDPilotAccount`仅是内存结构，不是数据库模型。候选镜像尚未构建/发布，不能宣称当前默认APP已具备新行为。

## 2. 试点归属清单

新增`SOURCEID_PILOT_ACCOUNTS_FILE`指向受保护、只读挂载的JSON。文件版本1，最多100账号/64KiB，包含issuer、统一policy_ref、ownership_ref及每个原user_id、邮箱快照、独立核验的employee_no（RJGH）。所有角色使用同一清单，不新增管理员申请流程。

POSIX要求普通文件、无组/其他访问权限；Windows检查实际NTFS DACL，仅当前owner、SYSTEM及本机管理员可访问，不能以os.Chmod伪装私密。拒绝符号链接、未知字段、重复账号/邮箱/RJGH、空归属引用及错误issuer/policy。文件不可用或用户从范围移除，已有企业Access也在后续鉴权中拒绝。

部署应只读挂载固定的受保护目录，文件在该目录内原子替换；不能依赖单文件Docker bind mount在宿主机原子替换后自动切换inode。范围更新后需核验进程读取效果；永久撤销仍使用数据库业务撤销，不以临时移除清单代替。

首次关联必须同时匹配原user_id、规范化邮箱及预先核验RJGH；这避免邮箱转给另一个员工后，仅凭相同邮箱接管旧账号。已绑定时继续核验issuer/RJGH/sub及范围，不因后续邮箱变化改绑。冲突、停用、revoked及归属不明统一拒绝，角色不是拒绝条件。

ownership_ref是由授权数据负责人提供的外部依据引用，代码不能证明批准文件真实性。不得从当前OA成功结果自动生成“历史归属证明”。应从原账号历史/已确认目录关联批量核验原user_id与员工标识，再由受保护工具写清单；缺乏可信关联的账号不列入可绑定集合。无需逐位管理员额外申请，但不能将旧账号邮箱本身当完整历史证据。

清单含员工数据，不进入Git、CI、镜像、普通报告、命令参数或日志；代码及测试仅使用虚构值。存放目录0700、文件0600、只读挂载，引用与原数据基线归档于受保护位置。

## 3. 候选配置

获得部署及策略批准之后，独立试点使用：

```dotenv
SOURCEID_FORCE_REAUTH_PROMPT_LOGIN=false
SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED=false
SOURCEID_BASIC_ENCODING=oauth_form
SOURCEID_SESSION_MODE=local_approved
SOURCEID_ADMISSION_MODE=oidc_sso_approved
SOURCEID_SSO_POLICY_CONFIRMED=true
SOURCEID_PROTOCOL_ONLY=false
SOURCEID_CONTRACT_CONFIRMED=true
SOURCEID_EMAIL_BINDING_ENABLE=true
SOURCEID_EMAIL_CONTRACT_CONFIRMED=true
SOURCEID_AUTO_BINDING_ENABLE=true
SOURCEID_CLAIMS_SOURCE=userinfo
SOURCEID_EMPLOYEE_CLAIM=attributes.RJGH
SOURCEID_LOCAL_REFRESH_ENABLE=false
SOURCEID_LOGOUT_ENABLE=false
AUTO_MIGRATE=false
```

还必须提供真实`SOURCEID_AUTO_BINDING_POLICY_REF`、`SOURCEID_LOCAL_POLICY_REF`、`SOURCEID_ADMISSION_POLICY_REF`、受保护清单路径及四个显式期限。以上不是本轮运行配置，不能直接全员启用。OIDC/SOURCEID本身开启、正式端点和受保护凭据继续按已验证配置。

Access/Absolute/Idle/AdmissionMaxAge的**合成候选值**分别60m/60m/30m/60m，未视为获批。空闲按已认证API活动计算；后台轮询可保持活动，绝对期限不变。普通SSO可在本地过期后复用SID登录态，停用总风险窗口仍非60m保证；SID SSO寿命及停用边界未知须明确接受。旧流在到期后是否立即终止未新增保证。

## 4. 验证状态

最终CI及机器可读摘要见第7节。验收范围包括：

- 配置默认关闭、缺项拒绝、外部模式拒绝绕过、旧强制模式兼容；受保护清单格式/权限检查。
- 服务全角色Access-only：viewer/contributor/owner/tenant_admin/org_admin/system_admin/cross_space_admin/组合管理权限；原用户与角色不变、重复登录无新用户、邮箱变化不改绑。
- 真实AuthHandler＋HTTP Mock SID：普通SSO不含prompt/max_age，严格OIDC callback、结果消费、auth/me200、合法/非法空间切换、无Refresh响应及DB记录、本地退出后401；覆盖所有角色及多空间成员。
- 原强制模式和新普通SSO分别使用真实Vue＋Mock SID进入Portal、恢复登录态、清理预置旧Refresh、读取合成KB列表、退出、拒绝Mock无效授权。
- 撤销绑定/Session、空闲和绝对期限、缺失清单、范围外/错误归属/RJGH不符在写入前拒绝；PostgreSQL并发首次绑定复用锁及唯一约束。
- 现有OIDC安全、密码登录、MCP、飞书、迁移及其他Provider回归。

Mock浏览器中的Portal业务API部分使用合成夹具，不能当作原库全文读取、真实RAG或真实SID已验证。时间到期用受控数据库时间边界驱动实际鉴权拒绝，不宣称已经等待真实60分钟。未因合成浏览器通过宣称历史Chrome STATUS_BREAKPOINT已解决。

全角色HTTP测试针对真实认证及SwitchTenant链路，未逐个调用全部管理功能端点；原业务权限实测仍是独立副本部署后的验收项。未授权空间43确实存在，普通账号没有成员关系；不存在空间999仅测试参数/资源错误，不作为越权拒绝证据。跨空间管理员按原能力允许43，不伪造其必须被拒绝的KB资源。

本地Windows配置/repository测试、23项前端认证测试、类型检查和构建已通过；service/handler尝试遇到DuckDB/MinGW依赖链接问题，完整后端和浏览器以固定Linux CI结果为准。初轮CI的两个PostgreSQL旧夹具缺失Token有效期，补齐测试数据后重跑，未放宽真实校验。

## 5. M4只读现状及未完成项

15:52:53北京时间：原库79 clean，用户3、系统管理员1、跨租户标记账号0、有效KB1、文档3、非空分块及embedding各2717；tenant_members共5条，均owner，organization_tenant_members无记录。未读取员工身份或文档原文。干净候选PG仍停止、network=none，两卷及四份受保护DB/文件备份存在，磁盘约253GB空闲。默认APP原镜像及容器healthy，80/8080仍对0.0.0.0发布；实测prompt=false、protocol_only=true、contract/email/auto-binding=false、oauth_form。

这不证明快照全部内容与源库最新一致；复用前需成套新鲜度核验，变化则冻结窗口重采。源库没有跨租户标记账号，不能宣称已在原库真实覆盖该类型；本轮只完成合成覆盖。候选停止，未查询候选最新行数，更未用新候选验证原文档或原权限。既有干净副本的旧版本验收保留作为前置证据。

当前0.0.0.0入口尚非受限试点网络。下一步必须按操作卡启动独立环境、使用独立成套DB/文件副本及新密钥、限制端口/来源并取得允许和禁止来源的实际证据。不能仅靠hosts或文件存在判定隔离成功。

## 6. 发布与回滚边界

见[SOURCEID_MINIMUM_BUSINESS_PILOT_OPERATION_CARD.md](SOURCEID_MINIMUM_BUSINESS_PILOT_OPERATION_CARD.md)。新业务试点需要包含小范围前端Refresh清理改动；旧Frontend1ba24ef5f可能残留上次登录的Refresh，不能直接宣称纯APP升级即可完整满足要求。APP-only快速回滚可保留新前端，因为其兼容旧的有Refresh响应，但仍须恢复原门禁并定向撤销新Session。

本轮仅候选代码/Mock/CI及只读准备。新镜像、独立环境部署、真实数据录入及真实SID业务认证仍等待明确批准。所有角色共用入口和策略，不部署第二套管理员认证。

证据：[只读候选及原库核查](evidence/sso-minimum-candidate-readonly-20261016.json)。

## 7. 最终验收结果与运行环境复核

CI Run `38871919491`与候选832b6652a完整SHA一致，linux/postgres/frontend/browser均success：

| 项目 | 最终结果 |
| --- | --- |
| Linux | race定向认证、诊断过滤、飞书/迁移兼容、完整认证/MCP回归、go vet及server/工具编译均通过 |
| PostgreSQL | 11个PASS事件，含试点范围内并发首次绑定、Access-only签发与撤销竞态、迁移及旧模式兼容 |
| Frontend | 类型检查、23项认证回归测试、构建通过 |
| Browser | 真实Vue＋Mock SID的原强制模式与普通SSO两项均通过，Portal/auth/me/登录恢复/无Refresh残留/退出与Mock授权拒绝验证 |
| 8项重点父测试 | 全角色、证明/范围/期限拒绝、策略默认关闭、清单校验、全角色HTTP、绑定并发、撤销竞态及关闭SourceID不得回落通用OIDC，全部PASS |

Linux日志统计5511个Go PASS事件、40个SKIP事件，包含子用例及多轮执行，不是5511个独立功能测试；跳过场景不视为已覆盖。核心PostgreSQL及浏览器场景另在专用job执行。现有编译器/Node基线未改，未触发镜像构建workflow。

首轮失败及修正保留：旧PostgreSQLToken夹具未填写有效期，及新增跨空间HTTP夹具遗漏批量租户查询；均补齐测试夹具，没有放宽认证。Windows最初的manifest权限失败通过实际私有DACL夹具修正，没有关闭权限校验。最后审查补上新模式缺失SourceID开关时禁止通用OIDC回落的安全断言。

16:28:16北京时间独立只读复核：默认APP/Frontend容器ID与开发前一致，APP479416c90 healthy、Frontend1ba24ef5f、DB3027 dirty=false；用户2、企业绑定0、本地企业Session0、活动未消费事务0。80/8080及配置代理HTTP200。prompt=false、protocol_only=true、contract/email/auto-binding/recheck/logout=false、external默认、oauth_form不变；OIDC及诊断保持原已批准true，AUTO_MIGRATE=false。

- [最终CI脱敏机器收据](evidence/sso-minimum-final-ci-20261016.json)
- [默认服务未变化复核](evidence/sso-minimum-live-unchanged-20261016.json)

交付状态：可审阅的代码候选及合成验收完成。下一步等待批准构建/部署独立试点、录入受保护归属数据及网络验证；真实员工、原库新候选权限和正式期限仍未验收/批准。
