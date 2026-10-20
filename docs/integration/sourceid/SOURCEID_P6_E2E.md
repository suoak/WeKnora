# 受控真实 SID A–I 联调测试

执行状态全部 **NOT_TESTED / BLOCKED_EXTERNAL**。本文件是测试计划，不是通过报告。每项填写测试环境/commit、批准人和动作范围、执行人/UTC时间、脱敏证据链接、PASS/FAIL/NOT_TESTED、清理结果。Mock/CI不能填入真实结果栏。

## 全局前提与证据规则

正式契约清单已签署；可信会话字段、nonce/JWKS与通知来源明确；隔离测试数据库/业务数据、备份、审核员工映射均获批准。经预检查证明批准callback进入该实例而非生产库；网关/应用/APM敏感日志门禁通过。Client Secret只由服务进程安全读取。需要用户再次确认首次员工登录、SID主动退出、身份关联及任何生产域名路由动作。

准备员工U1（普通测试员工）与U2、两套**独立SID会话**A/B、两个已存在空间T1/T2及有/无访问权限知识库K1/K2、本地应急管理员、只读业务API Key/MCP。浏览器profile独立并不证明SID索引不同，必须先证明A/B不同index；同index的多本地session会被一起撤销，这是预期行为。

access/refresh只留在批准的测试客户端内存或受限运行环境，不进入终端参数/聊天/HAR/CI。证据仅字段存在/类型、验签结果、匿名会话标签、请求ID、HTTP状态、数据库聚合row_count与哈希对比，不输出完整令牌/XML/工号/email/证件号/TGT。记录t0之前已开始请求与t0之后新请求的区别；不把已运行的SSE流是否立即终止当作本版承诺。验收撤销后的新请求/刷新必须拒绝。

### A. OIDC 登录成功

- 前提：正式端点、nonce/client auth/Profile编码、审核U1字段和会话来源、用户登录许可就绪。
- 操作：用户在批准的KnowHub测试页面点击既有OIDC按钮；后端产生一次性state/nonce，跳正式SID；用户本人输入凭据；SID自动回调，后端立即POST交换并验签、获取所需UserInfo，一次性cookie兑换登录结果。不要人工复制短TTL授权码。
- 预期：RS允许算法/JWKS/iss/aud/azp/exp/iat/nbf/nonce/sub全部验证；UserInfo模式下同subject、可信业务字段满足要求；登录成功且URL无本地JWT。secret不在URL。
- 证据：端点配置脱敏摘要、签名算法和各校验bool、回调/兑换请求ID及状态、state消费计数、schema类型树；不输出code/nonce/state值或Cookie。
- 通过：全部必需校验PASS、只兑换一次、无未知自动开户、无敏感日志。若字段未知或无session，不取消校验；停止并记录BLOCKED_EXTERNAL。

### B. 原账号与权限保持

- 前提：A通过；原UserID及users/成员/ACL/API Key作用域基线已审核。
- 操作：GET /auth/me，查看全部成员/空间、系统管理员能力与跨空间偏好；只读访问K1，尝试读取无权限K2；比较数据库基线。不进行权限/知识库写入。
- 预期：原ID完全相同，无额外账号/提权/成员或ACL改动；已授权读成功，未授权仍拒绝。企业岗位属性不改变本地权限。
- 证据：匿名UserID比较结果、成员/ACL规范化摘要diff、允许/拒绝读请求ID与HTTP状态、identity/enrollment新增计数。
- 通过：所有原有权限一致，K2未被授权；只有审核identity/session/token新增。任何UserID变化判FAIL并暂停。

### C. 本地退出

- 前提：A通过，A/B确为不同SIDindex，批准测试应用退出（不调用SID登出）。
- 操作：在A点击KnowHub本地退出；尝试A原access、refresh及派生token；B用新只读请求确认仍可用。
- 预期：A本地会话族撤销，SID本身不被KnowHub主动退出；B/U2和API Key不受影响。
- 证据：本地logout请求ID、A session.revoked/token.is_revoked聚合、A拒绝/B成功的状态；SID通知计数仅作观察。
- 通过：A全部新认证/刷新拒绝、B正常且无SID主动请求。不要把浏览器页面清空视为服务器撤销证据。

### D. SID 主动退出通知

- 前提：可信通知契约已验证；新A会话及派生token准备；明确此次SID退出是否会通知B；用户单独批准真实SID退出。
- 操作：用户在正式SID执行批准的退出动作；观察SID→可信网关→后端POST通知→数据库提交→ACK；立即验证A各凭据及不在通知集合内的B/U2。
- 预期：已收到的每个issuer+index对应会话精确撤销；其他index保持。ACK在持久化成功后返回。SID若明确全局logout，应按全部合法通知集合验收，而非要求B无条件存活。
- 证据：SID事件ID匿名别名、网关来源验证结果、peer验证bool、后端请求ID/ACK/耗时、相关会话撤销count、无关会话count不变、t0后请求拒绝。
- 通过：约定SLA内送达与撤销，通知集合之外无误撤销，日志无敏感资料。只有网关200而无DB/凭据验证不算通过。

### E. SID 会话超时通知

- 前提：SID方确认此OIDC应用支持超时backchannel；批准测试员工/会话的短超时或自然等待，不修改全局生产TTL；新A和独立B。
- 操作：按SID规则保持A无活动直至SID超时，避免自动请求误续期；记录SID事件和通知；尝试A访问/刷新，确认无关B/U2。
- 预期：SID超时产生匹配通知并在SLA内撤销A。本地7天最大session不能替代SID短超时通知。
- 证据：SID TTL/最后活动/超时时间（脱敏）、通知链路与ACK、撤销记录及A拒绝/B成功。
- 通过：有真实超时事件→实际通知→持久化撤销→凭据拒绝四段证据；无通知记FAIL/BLOCKED_EXTERNAL，不用手工伪造通知代替。

### F. Refresh / SwitchTenant 派生令牌

- 前提：A获准在T1/T2切换，隔离客户端可安全暂存各代token；D或E动作获批。
- 操作：刷新得到A1，确认旧refresh重放拒绝；切T2取得A2，再切T1取得A3（按当前API），使用当前access继承相同本地session；触发批准的匹配SID通知；分别验证A0–A3 access与当前/旧refresh。允许环境中另测应用重启后拒绝，重启需环境负责人批准。
- 预期：所有代的session相同，撤销后的新验证/刷新/空间切换拒绝；无错误接受另会话refresh。
- 证据：token代号→匿名session映射bool、重放拒绝、t0后各HTTP状态与撤销count；重启前后持久化count。
- 通过：没有任何派生凭据恢复会话、重启不复活。不要输出JWT decode原文或refresh值。

### G. 不同设备与用户隔离

- 前提：SID证明确实A/B不同SessionIndex（只存比较结果）；U2独立；批准仅影响A的SID退出动作。
- 操作：三个会话分别只读验证；触发A通知；分别做访问/刷新；若SID退出动作天然全局则由SID测试方产生真实仅A会话事件，不能伪造生产通知替代。
- 预期：A撤销，B/U2保持；同index的多个本地会话应一起撤销。
- 证据：index相等/不等bool、本地session别名、通知集合、三组撤销count和状态。
- 通过：精确集合一致、其他用户/独立index未变。无法建立独立index则NOT_TESTED，不宣称设备隔离已证实。

### H. 错误、重复、伪造通知

- 前提：独立Mock/隔离测试实例、人工批准负例与供应方非业务fixture，不向生产/真实SID发送伪造请求。可信入口机制已定。
- 操作：测试缺认证/伪造forwarded头、错误peer/secret、格式错误/XXE/超限/重复body字段/query、不支持编码；重送合法同index通知；发送经认证的不存在index fixture；隔离库故障验证503/重试。禁止使用未批准的真实其他会话index。
- 预期：来源不可信在网关/后端拒绝且无撤销；格式400/415、关闭503；同通知幂等；合法未知index可200并写tombstone但不撤销现有会话；DB失败不ACK成功。旧IssueInstant合法重试当前只验格式，不能假定一定被拒绝。
- 证据：每类fixture标签、网关/应用状态、调用/revoke/tombstone计数、无关会话变化为0。fixture仅模拟值，不能在日志放真实XML。
- 通过：拒绝/幂等/未知tombstone与上述契约一致，无其他会话误撤销。能通过入口认证且携真实index的恶意请求无法由当前parser识别，来源信任必须先证明，不把XML结构校验当签名。

### I. 本地登录、MCP/API Key 与业务

- 前提：应急本地管理员登录单独获批、低权限只读Key、已授权MCP endpoint与数据；原有UI/业务基线。
- 操作：SID关闭/隔离故障时验证本地登录及原本地logout语义；SID撤销前后分别只读知识检索/问答、MCP/API Key、正常管理员页面。问答如写会话历史必须纳入隔离测试授权，不能对生产盲测。
- 预期：本地认证和Key/MCP原独立认证/作用域保持，不获得新权限；SID异常只阻止新的SID认证，已发行SID会话的存活按通知/本地期限及撤销规则判断。
- 证据：匿名请求ID与状态、权限作用域比较、业务响应成功bool、无无关token撤销/权限变更。
- 通过：业务及应急入口可用、权限边界未扩展；所有后端会话凭据复查路径满足撤销。不是仅首页能打开。

## 停止与清理

任何签名/nonce/subject失败、身份冲突、未授权账号、错误环境/路由、敏感日志、误撤销立即停止，不继续真实重试。关闭新SID入口，按批准范围撤销测试会话，保留tombstone/审计；错误绑定按专门回滚说明处理。取消后不自动恢复真实配置、生产权限或旧会话。最终逐项报告PASS/FAIL/NOT_TESTED，未完成D/E/G不得称统一登出已验证。
