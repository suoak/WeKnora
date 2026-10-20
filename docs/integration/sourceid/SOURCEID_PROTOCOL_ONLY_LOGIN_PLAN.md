# 单次只读 SID OIDC 协议验证

2026-10-13。当前真实验证：NOT_TESTED / BLOCKED_EXTERNAL。独立入口已实现为 cmd/sourceid-probe，验证和运行说明见 [Probe 实现](SOURCEID_PROTOCOL_PROBE.md)。尚未在真实测试环境启动该入口；现有健康业务实例不承担 probe callback。

## 本轮边界

仅一位普通员工在正式 SID 浏览器页面手动认证。完成 authorize → callback → code 兑换 → ID Token 验证 → UserInfo 获取，不登录 KnowHub 业务账号，不绑定用户，不创建企业会话，不签发本地 Access/Refresh Token，不迁移权限，不调用真实登出。

员工审核映射、员工状态字典和 SessionIndex 对应关系不再作为本轮协议观察的前置条件；自然人工号字段只报告候选，不能据字段名、字符串格式或旧 OAuth 示例宣布已确认。

## 现有实现与必要差异

基线 8ae91bc4439dcc93be11844ada5ec1479f2730bc 的业务 callback 调用 LoginWithOIDC。其后续流程会进入 loginSourceID、账号解析、会话及本地令牌生成。因此直接开启现有业务登录入口无法满足本轮范围；关闭 SourceID 校验也可能进入通用 OIDC 自动开户分支，禁止采用。

应新增独立测试诊断进程/入口，复用现有 exchangeOIDCCode、verifyOIDCIDToken 和 fetchOIDCUserInfoWithTransport 的协议能力，保留现有认证模块。新入口必须在调用任何用户仓库、身份解析、会话创建或 GenerateTokens 之前结束；通过不提供数据库依赖及测试中禁止这些调用来证明不会落库。

诊断进程只接受一笔由测试操作员创建的短期事务。state、nonce、PKCE verifier、授权码及 SID Token 仅在进程内短期持有；一次消费 state，拒绝未知、重复、超时事务。浏览器所需的短期绑定凭据不得导出、记录或持久化到报告；不导出 SID Cookie、密码或 HAR。不通过聊天复制授权码。

必须验签、校验 issuer/audience/azp、有效期、nonce、subject，并核对 UserInfo.sub 与已验证 ID Token.sub 一致。失败立即结束，不通过未验签 JWT decode 获取“可信”员工证据。UserInfo 获取失败不能用 ID Token 字段假装实际 UserInfo 返回。

不申请 offline_access，不主动使用 Refresh Token。提供方若仍返回 Refresh Token，仅短期接收后丢弃，不保存或使用。诊断代码不得缓存完整 Token 响应、记录 HTTP 请求/响应或返回原始错误正文。

## 允许输出

| 项目 | 允许保留的证据 |
|---|---|
| Issuer/端点 | 已确认、无敏感查询参数的公开地址；实际使用地址；metadata 一致性 |
| Token 行为 | POST 方法、Content-Type、client auth 类型、HTTP 状态、JSON 字段名/类型、各令牌是否存在；不含值 |
| ID Token | JWT 格式是否成立、alg、签名及每项声明校验布尔结果、声明字段名与类型 |
| UserInfo | 真实 HTTP 结果、字段路径、类型、层次、subject_equal 布尔结果；不输出任何个人字段值 |
| 工号候选 | 字段路径/类型/是否存在，注明语义待 SID/HR 确认；不输出工号或脱敏值的可逆片段 |
| 会话字段 | sid/tgt/SessionIndex 等候选字段是否存在和类型；本轮不确认统一登出关联 |

字段结构采集需要限制深度、字段数量、名称长度；不复制数组元素值或原始字符串。错误仅报告固定类别和 HTTP 状态。响应采用 no-store/no-referrer，不把诊断结果或凭据放在 URL 中。

## 实施与验证顺序

1. SID 提供正式非秘密 Metadata：Issuer、Discovery（如支持）、Authorization、Token、JWKS、UserInfo、client auth、scope、nonce/PKCE 和签名算法。不根据 /cas/oidc/* 示例推断；不探测未知发现路径。
2. 实现上述独立只读诊断入口，以 Mock 测试验证成功、错误签名、issuer/aud/nonce/sub 错误、重放/超时、UserInfo 失败、凭据不出现在日志/响应以及零数据库/用户绑定调用；完成受影响云端验证。
3. 按临时 callback 方案明确实际入口和转发，取得相应变更批准或 SID 新 callback 审批。不得直接修改现有 80/8080 服务。
4. 操作员从交互终端将 Secret 输入诊断进程内存，不写 env/脚本/凭据文件，不进入 argv/URL/日志。当前 SID Secret 尚未注入；已有文件注入说明不用于此次“不得保存 Secret”的严格诊断模式。
5. 用户打开本次诊断授权入口，手动完成一次 SID 认证；callback 自动立即兑换 code，输出上表脱敏证据。
6. 无论成功或失败，关闭诊断入口并清除内存事务/凭据；业务 SID 和 backchannel 始终关闭。停止诊断进程不能保证 Go/OS 对所有内存副本物理擦除，不承诺可抵御同权限调试或内存转储；禁止启用 core dump/调试抓包。

## 当前已具备及未执行

独立 knowhub-sourceid-p6 实例已健康，入口 127.0.0.1:18182，新库 schema3024 clean，用户0；现有 80/8080 未替换。基线 runtime 镜像已导入；SID 开关关闭、Secret 未注入。临时观测隔离配置已准备，完整源码日志专项尚未云端验证。

正式 Metadata 未提供；独立 probe 已实现，真实入口未启动，新 /probe/callback 尚未审批/连接。尚未 authorize、接收真实 code、兑换真实 token 或获取真实 UserInfo。部署/路由未按本方案发生变更，READY_FOR_RELEASE=false。
