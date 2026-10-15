# 更新凭据后的真实业务验证

2026-10-15；仅只读采集打包机 172.30.232.42。业务版本 468410e89，raw Basic、无 PKCE、openid、严格 state/nonce/RS256 与身份门禁保持。Codex 未发起新授权或 Token 请求。

**更新凭据后，现有日志中的三笔真实 Token 请求均返回 HTTP 200，之前持续的 401 已不再出现。两笔完成 ID Token 与 UserInfo 校验，在身份契约门禁前安全停止；一笔 ID Token 验证失败，未绕过。**

| 北京时间（Token 分派） | Token 状态 / 耗时 | ID Token | UserInfo | 本地 Trace |
| --- | --- | --- | --- | --- |
| 2026-10-15T11:24:16.220528021+08:00 | 200 / 206 ms | PASS | PASS | `29ab100d-da7e-4326-83d5-3c21991c4152` |
| 2026-10-15T11:24:27.630887135+08:00 | 200 / 230 ms | FAIL | NOT_TESTED | `31657082-d225-4df7-a362-d4ba8a3981c2` |
| 2026-10-15T11:24:33.85662092+08:00 | 200 / 215 ms | PASS | PASS | `8d139894-11be-497c-b57a-7df19308d5a3` |

每个 Trace 均只有一次 Token 写出，未观察到重试。这是三笔独立的浏览器事务，不能描述为一次事务或同一个 code 被兑换三次；是谁及为何触发多笔，客户端日志无法判断。

Token 请求均为 raw_then_set_basic_auth_base64_once、POST、urlencoded、一个 Basic Authorization；code/client_id/grant_type/redirect_uri 四字段，不含 client_secret/code_verifier。callback state 均 PASS。

ID Token PASS 对应源码 verifyOIDCIDToken 及 nonce 一致性检查完成；UserInfo PASS 对应真实接口及身份一致性检查通过。返回数据仅保存字段名/类型，不保存 Token、code 或个人资料。验证声明包括 iss/aud/sub/exp/iat/nbf/nonce/jti/at_hash/client_id/state/preferred_username/amr。UserInfo 包括 id/sub/auth_time 和 attributes。

attributes 字段名包括 GH、RJGH、objectId、STAFFTYPE、HEADCOUNTTYPE、SFLBDM、POSITION、POSITIONCLASSIFY、RJEMAIL、RJXM、XM、XB、TEL、SZDW、SZDWCODE。不能仅凭名称认定哪个是稳定自然人工号或在职状态，也不能将 sub/jti/objectId 当成 SessionIndex。仍需 SID 正式确认字段语义、稳定性和允许状态值。

中间一笔失败类别仅记录为 id_token_verification，现有诊断不能确定是签名、密钥匹配、Issuer/Audience 或时间声明问题。不得将其他两笔成功扩展为所有节点/所有令牌均可靠，也不得自动降低校验。下一步可在不保存原始 Token/错误文本的前提下进一步细分验证错误类别，必要时请 SID 核查该 Trace 对应时间的 JWKS/签名节点和时间状态；本轮未修改代码。

身份门禁在 resolveOIDCUserInfo 成功后，由 SOURCEID_PROTOCOL_ONLY=true 返回 sourceid identity contract pending；前端安全错误为 identity_contract_pending。日志 identity_mapping/rejected 是该门禁的概括分类，不代表 Token 失败。未进入 local_session 阶段，不能期待当前测试直接建立企业业务登录态。

根因判断：在 raw 模式已生效但仍 401 的上一轮之后，只更新凭据即出现 200，强烈支持之前的 KnowHub/DPAPI 与成功 Postman 凭据不一致是 401 的关键原因。本轮不能证明 raw 编码本身是否必要，也不能确认所有历史 401 均同一根因；SID 服务端拒绝日志仍是最终依据。

用户要求常驻测试环境，未擅自关闭 OIDC。protocol_only=true、contract_confirmed=false、backchannel=false 继续保持。请停止重复点击，接下来应先核实员工身份/状态契约和中间一笔 ID Token 失败，不绕过审核映射来完成业务登录。

[完整脱敏证据](evidence/after-secret-reset-real-results-20261015.json)
