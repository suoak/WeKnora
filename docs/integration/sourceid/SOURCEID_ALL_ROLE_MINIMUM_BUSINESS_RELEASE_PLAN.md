# KnowHub全角色OA登录：最小业务放行配置、验收及回滚

2026-10-16。**PLAN_READY / NOT_ENABLED**。本文件为最新产品基线，替代先前按普通账号/管理员分别批准关联的设计。

## 1. 统一流程及审计结论

所有现有账号使用同一入口、同一认证、同一首次自动关联、同一本地会话机制。owner、空间/组织/平台/系统/跨空间管理员不增加申请、人工绑定或专属登录流程。只有身份归属异常进入核查，与角色无关。

流程：普通SID SSO → 严格state/nonce/签名/Issuer/Audience/时间/UserInfo sub → issuer+RJGH查已绑定用户并核对sub → 未绑定时可信RJEMAIL规范化唯一匹配已有active账号 → 统一归属及冲突校验 → 自动审核记录与唯一绑定 → 原user_id的有限本地Session → Portal/auth/me/原权限。

现有`sourceid_binding.go`已经满足全角色自动关联核心要求，无需再移除角色限制或重新实现匹配：

- `ResolveSourceIDEmailIdentity`先查既有review/身份；拒绝RJGH/sub冲突、revoked、重复邮箱、停用和目标已有其他身份。
- 首次匹配使用LOWER(TRIM(email))，不推断邮箱别名、加号或Unicode等价；唯一且active才形成候选。
- `ResolveSourceIDAutoEmailIdentity`按统一政策自动批准并再次解析；数据库锁、唯一约束和审计继续复用。内部pending→approved是自动事务流程，不要求用户逐个申请。
- `reviewSourceIDBinding`无角色拒绝，不更新user或成员权限；保留原user_id、tenant_id、is_system_admin、can_access_all_tenants、成员及ACL。
- `TestSourceIDAutoEmailBindingAllRolesPreserveOriginalAccount`已有viewer/contributor/owner/tenant_admin/org_admin/system_admin/cross_tenant_admin及历史无成员账号的合成断言，检查两次关联、邮箱变化、原user完整值、成员及合成ACL不变、无重复用户、撤销后拒绝。

以上为源码及既有测试审查，不宣称本轮重新运行测试或完成真实全角色验收。原库向量/文件验收亦不能替代真实业务认证与实际权限断言。

## 2. 当前不能只改配置的原因

当前479416c90仍有两个独立阻断：

1. `loginSourceID`先检查PROTOCOL_ONLY/CONTRACT_CONFIRMED；进入local_approved后，`SourceIDLocalPolicy`和authorization resolver仍强制prompt=true/recheck=true。因此关闭protocol_only不等于普通SSO可以登录。
2. LOCAL_REFRESH_ENABLE=false会拒绝续期，但`generateTokensForTenant`仍签发并返回Refresh；SwitchTenant要求Refresh predecessor。不符合本期“不依赖Refresh”的完整目标。

仅需最小适配：新增默认关闭的普通SSO策略路径，复用已验证OIDC事务、绑定及Session；Access-only签发/存储并使合法SwitchTenant沿用同一Session；补统一试点范围/账号归属保护。保留原强制authorization模式及其他Provider行为，不删prompt代码，不重写全角色匹配。

高权限账号不是单独改造项。历史邮箱重分配风险是全角色统一检查项：目前代码能拒绝已知绑定冲突，但对从未绑定过、旧账号邮箱已转给新员工的情况，唯一邮箱不足以识别。需要可信历史归属/变更证据或经授权的数据基线；无法取得时对该异常账号拒绝，不能默认“无冲突即归属正确”。不以管理员身份本身判定异常，不要求每位管理员另申请。

## 3. 最小配置：当前值与目标

下表目标仅在最小适配、独立测试及批准后使用，**不是现版本可直接执行的配置清单**。当前所有业务门禁保持关闭，不以伪造recheck确认满足旧代码。

| 配置 | 当前/本期目标 | 前置条件 |
| --- | --- | --- |
| SOURCEID_FORCE_REAUTH_PROMPT_LOGIN | false → false | 普通SSO，不附加max_age，不以auth_time新鲜度为首期要求 |
| SOURCEID_BASIC_ENCODING | oauth_form → oauth_form | 不改已验证协议参数 |
| SOURCEID_PROTOCOL_ONLY | true → false | 普通SSO本地会话策略及隔离验收齐备 |
| SOURCEID_CONTRACT_CONFIRMED | false → true | 本期统一身份/有限会话政策已批准，不表示实时员工状态确认 |
| SOURCEID_EMAIL_BINDING_ENABLE | false → true | 可信UserInfo/RJEMAIL、原账号存在、统一归属校验 |
| SOURCEID_EMAIL_CONTRACT_CONFIRMED | 已确认事实；目标true | RJEMAIL可信为USER_CONFIRMED，无需重新询问SID |
| SOURCEID_AUTO_BINDING_ENABLE | false → true | 统一政策覆盖全部角色，试点范围技术限制有效 |
| SOURCEID_AUTO_BINDING_POLICY_REF | 空/保持 → 实际批准引用 | 同一份全角色策略，不能填虚构编号 |
| SOURCEID_SESSION_MODE | 默认external → local_approved | DB3027、随机本地Session、期限及撤销验收 |
| SOURCEID_ADMISSION_MODE | 未启用 → oidc_sso_approved（拟新增） | 当前代码不支持，需完成最小策略适配 |
| SOURCEID_SSO_POLICY_CONFIRMED | 尚无此字段 → true（拟新增） | 普通SSO风险接受；默认false |
| SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED | false → false | 不能把普通SSO当作新员工状态复核 |
| SOURCEID_CLAIMS_SOURCE / EMPLOYEE_CLAIM | 目标userinfo / attributes.RJGH | 严格类型、issuer+RJGH唯一、sub一致 |
| SOURCEID_LOCAL_POLICY_REF / ADMISSION_POLICY_REF | 目标实际批准引用 | 有限会话和普通SSO接受策略，不能捏造 |
| SOURCEID_LOCAL_ACCESS_TTL / ABSOLUTE_TTL | 目标60m / 60m，待批准 | Access到期不超剩余Session/接受快照期限 |
| SOURCEID_LOCAL_IDLE_TTL / ADMISSION_MAX_AGE | 目标30m / 60m，待批准 | 后者只是本地身份接受快照上限，不是员工状态新鲜度 |
| SOURCEID_LOCAL_REFRESH_ENABLE | 目标false | 不签发、不返回、不保存、不续期；只作用本期企业模式 |
| SOURCEID_LOGOUT_ENABLE | false → false | 本地退出有效，SID统一登出二期 |
| OIDC_AUTH_ENABLE / SOURCEID_ENABLE | 目标true / true | 当前受保护有效凭据，正式Discovery/JWKS |
| SOURCEID_DIAGNOSTICS_ENABLE | 试点受控启用，结束恢复批准值 | 严格白名单、访问/体积/保留期 |
| AUTO_MIGRATE | false → false | 不在启用时隐式迁移 |

固定scope=openid、Authorization Code、client_secret_basic、PKCE=false、原审批callback；所有协议安全校验不变。不配置猜测的状态字段或SessionIndex。

60m绝对/30m空闲仍为候选。当前空闲定义是认证API活动，后台轮询可能保持活跃；绝对期限不能滑动。已有流在到期后的处理需批准明确最大持续时间或实现中途检查。

普通SSO到期后可能复用SID SSO建立另一笔本地Session，故员工停用风险不是60m硬上限。SID原SSO硬期限/滑动规则及停用传播仍未知；试点需明确接受该风险，不能宣传实时停用。prompt验收不再是首期前提。

## 4. 干净副本及试点隔离

当前默认3027库不能代表原账号/KB数据，不拿当前viewer替代原管理员。优先复用已经完成干净准备的`knowhub_legacy_clean`与同代独立文件卷，先验证快照新鲜度及资源存在；候选准备批准不自动等于当前默认服务切库批准。

原库79→3027迁移已做隔离验证；若原数据未变，不重复无价值迁移。数据已变化则在获批冻结窗口重做同代DB/文件副本，执行完整迁移，不能跳版本/改schema_migrations。候选保持原用户/租户/管理标记/成员/KB及ACL、3份文档和2717非空分块及embedding引用；实际数量以新快照核验为准。旧会话/API/MCP Key撤销，独立JWT/AES，禁外部同步/队列，文件副本可用但不写原卷。

真实SID需网络可达，原候选network=none不能直接做真实联调。新试点环境必须另批：仅指定来源可达Frontend/API，必要出口仅SID及获批组件；实际验证允许/禁止来源、80/8080、IPv6、Docker直连及代理路径。原默认服务、生产库/DNS不变，原审批域名在测试浏览器受控路由到打包机试点。hosts只是选路，不是隔离。

全局AUTO_BINDING影响所有能到达入口的人。采用**角色无关的试点集合**及服务端写入前限制，配合网络隔离和固定窗口；不能用“只有一个人测试”代替技术边界。正式全员开放时移除试点范围限制需另批，不恢复角色限制。真实账号数据及副本启动/部署按环境授权办理，不按管理员身份另外申请登录。

## 5. 全角色验收矩阵

所有用同一按钮、callback、匹配及Session流程；不预先手工创建SID绑定来冒充“首次自动关联”。可一个账号覆盖多个真实权限类型，但必须记录覆盖矩阵，不能只造角色名称。

| 类型 | 核对原字段/关系 | 业务断言 |
| --- | --- | --- |
| viewer/contributor普通用户 | 原user_id、tenant_id、成员角色 | 原空间可读KB；无授权的真实跨空间KB拒绝；写权限符合原角色 |
| 空间管理员/owner | tenant_members等实际空间管理关系 | 原成员/空间管理能力保留，未增新的owner空间 |
| 平台/组织管理员 | 实际管理字段及organization成员关系 | 原管理范围内功能可用，范围外行为与原规则一致 |
| 系统管理员 | is_system_admin及管理路由授权 | 原系统管理功能正常，标记未改变 |
| 跨空间管理员 | can_access_all_tenants及真实策略 | 原跨空间能力保留；不能人为要求本来全局授权的账号对照KB403 |
| 多空间成员 | 全量原成员集合及各空间角色 | SwitchTenant可切合法空间，原角色/ACL不变，非法空间拒绝 |

平台/空间等产品名称必须映射到当前真实模型及授权检查，不能假造新的role字符串。平台管理员若映射is_system_admin需按该实际模型记录；组织管理员不是自动等于系统管理员。权限基线决定允许与拒绝结果。

共同成功断言：严格OIDC各阶段PASS → 首次唯一邮箱匹配 → 统一策略自动建立issuer+RJGH/sub绑定 → 原user_id → 随机local Session → Portal → auth/me200。第二笔独立获批登录继续同一用户、无新增用户/租户/成员、邮箱变化不改绑、全部原KB实际读取权限一致。

异常断言：重复邮箱、无匹配、停用、RJGH/sub冲突、历史邮箱归属异常、撤销后自动重绑均拒绝；未创建有效企业Session或业务Token，不泄露身份原值。并发首次登录只有一个身份映射。

Session断言：无Refresh返回/本地存储/数据库记录；Refresh请求拒绝；刷新页面恢复Access会话；合法切空间继承原绝对期限；60/30分钟边界实际拒绝新请求；本地退出、管理员定向撤销、绑定撤销后旧Access立即失败。普通密码登录/MCP/飞书及其他Provider回归不变。

历史全角色单元测试、已有向量召回、管理员Mock成功是前置证据，不能代替本次独立环境真实角色/权限验收。真实模型调用、重排/完整RAG需已有授权，不能擅自传原文给外部模型。没有代表性账号或不允许访问的真实对照资源时，对应项NOT_TESTED，不勾选完成。

## 6. 执行顺序及快速回滚

1. 批准角色统一的普通SSO/会话策略及候选期限、试点账号集合、统一归属证据来源、日历窗口和负责人。不要求管理员另走申请流程。
2. 最小策略/Access-only/试点限定适配，复用全角色代码；Mock/CI及干净副本兼容验收。未经批准不发布候选或迁移。
3. 获批启动独立试点副本和必要网络隔离，保留原默认库及服务，验证两类网络来源；记录全部原权限基线，不输出员工值。
4. 另批真实业务窗口后加载目标配置，逐笔验证矩阵。任何失败停止，不自动重试；scope/签名/state/nonce等不降低。
5. 试点结束先停止新业务关联和入口，定向撤销试点Session及Token；保留绑定审计。异常绑定走业务撤销，保留revoked防重绑，不删除用户和原权限。
6. 如需应用回退，只恢复原APP479416c90及原受保护配置，Frontend1ba24ef5f不变，DB3027不降迁，oauth_form不变；prompt=false、protocol_only=true、contract/email/auto-binding/recheck=false、external。仅改开关不能替代旧Access失效核验。
7. 原库正式切换尚未实施；后续若批准切库，DB/文件成套冻结、备份及切回，保全新写入差异，不覆盖或删除原库/当前验收库。原Session/Key不恢复。

定向撤销复用repository业务能力，现未确认存在可用公开管理API；执行前准备受保护CLI及dry-run，不直接全表SQL处理。恢复后验证本地救援登录、auth/me、DB clean、KB及配套服务。

## 7. 本轮交付状态

已完成：现有全角色逻辑审计、统一配置依赖与最小差距清单、干净副本使用方案、全权限验收矩阵、回滚方案；旧方案的管理员额外审批表述已纠正。

未执行：新策略实现/新CI、候选APP发布、真实绑定、真实OA登录、网络切换、切库或生产操作。下一步是批准最小适配及独立环境验证范围，而非重新开发管理员登录。
