# ID Token 验证失败分析

2026-10-15。目标事务北京时间 11:24:27.630887135，UTC 03:24:27.630887135Z；本地 Trace `31657082-d225-4df7-a362-d4ba8a3981c2`。本轮未重新登录、消费 code、读取真实 Token 或密钥，也未修改运行服务。

## 历史事实

- callback state PASS，raw Basic Token HTTP 200，耗时 230 ms，一次写出、无重试。
- id_token FAIL，174 ms，类别 id_token_verification；没有该事务已验证的声明 schema，也没有 UserInfo PASS。
- 前后两笔 Token 同为 200，ID Token/nonce/UserInfo 均通过，随后在 identity contract pending 门禁停止。
- 原日志没有安全细分错误，不能追溯确定是签名、选钥、时间还是其他声明校验；不保存原始 Token 的限制意味着不能离线重新验那笔令牌。

## 当前代码实际检查

| 检查 | 代码行为 | 目标历史事务结论 |
| --- | --- | --- |
| JWT 解析/算法 | golang-jwt/v5；SourceID 仅接受 RS256 | 具体失败点 UNKNOWN |
| JWKS | 每次验证 fetchOIDCJWKS，正常 TLS/SSRF-safe client；当前没有应用级 JWKS 缓存或失败自动刷新重试 | 当时具体公钥 NOT_CAPTURED |
| kid | 有 kid 时精确匹配；不匹配拒绝；无 kid 只允许唯一可用 RSA 签名公钥，多个则拒绝 | 目标 JWT Header 未记录 |
| RSA 签名 | 可信 JWKS RSA 公钥验签 | UNKNOWN |
| iss/aud | 匹配配置 issuer/client ID；多 audience/azp 额外检查 | UNKNOWN |
| exp | 必须存在；库检查过期 | UNKNOWN |
| iat | 检查未来签发时间，另要求存在 | UNKNOWN |
| nbf | 存在时库检查尚未生效时间；不要求所有 Token 必有 | UNKNOWN |
| 时钟偏差 | 原固定两分钟，没有修改 | 当时主机/NTP 精度 NOT_CAPTURED |
| sub | 要求非空；UserInfo 必须与已验证 sub 一致 | ID Token 阶段失败，未到 UserInfo |
| nonce | verifyOIDCIDToken 成功后与授权事务严格比较；缺失事务 nonce 拒绝 | 没有 nonce mismatch 记录；不标 PASS |
| at_hash | 当前只有字段 schema，不执行 Access Token 哈希一致性校验 | NOT_IMPLEMENTED，绝非 PASS |

不要将 nonce/at_hash 的字段存在当成校验成功。[OIDC Core 3.1.3.8](https://openid.net/specs/openid-connect-core-1_0.html#CodeValidation) 对 Authorization Code 流程的 at_hash 检查使用 MAY，因此当前未实施不能单独认定协议不合规；仍应明确产品/SID 的附加要求，不能报告为通过。本次不新增或绕过该校验以改变认证结果。

## 新的只读公开 JWKS 证据

2026-10-15 11:37:29 北京时间，从打包机正常 TLS 直连正式 `/oidc/jwks` 两次，均为 HTTP 200，均只返回一个 RSA 公钥，没有非空 kid；两次公开条目指纹不同，见机器证据。该首次指纹包含 kty/n/e/kid，未保留各分量，无法区分 RSA 材料变化与 kid 的 null/空字符串等元数据变化，不能据此认定实际公钥不同。

随后于 11:56:18 做了两次限定复核，指纹仅包含 kty/n/e，均为 HTTP 200、一个 RSA、无非空 kid，两次指纹相同。当前复核没有证明 RSA 材料不一致，也不能排除其他时间或节点的差异；服务端轮换/节点一致性仍是待核查假设，不是已确认根因。没有读取私钥或记录真实 Token，指纹仅描述公开数据，不是敏感凭据哈希。

若 Token 由 A 私钥签发，而下一次 JWKS 命中 B 公钥，无 kid 的单钥验证可能出现间歇性签名失败。客户端当前每笔重新获取 JWKS，不是旧应用缓存导致；中间代理/CDN/服务端缓存仍需 SID 核查。不能通过试遍未知公钥、跳过 kid 或接受不可信 JWT 的密钥来修复。

打包机只读时间检查：NTP=yes，NTPSynchronized=yes；主机 UTC 03:37:28.697090，容器 UTC 03:37:29，SID HTTP Date 为 03:37:29 GMT。当前未见明显时间漂移；这不是历史事务的精确时钟证明，也不能排除 SID 签发节点时间异常。

## 新增安全诊断（仅开发分支，未部署）

`sourceid_id_token_diagnostics.go` 新增固定枚举失败事件 sourceid_id_token_failure，通过与既有诊断相同的上下文 Trace、受限输出路径关联。原 stage 事件保留。只记录固定 categories 与 at_hash_validation=NOT_IMPLEMENTED，不记录错误对象、Header、JWT、kid 值或声明值。

类别：parse、algorithm、signature、jwks_fetch、key_selection、issuer、audience、expired、iat、nbf、required_claim、claim_type、iat_missing、authorized_party、subject_missing、nonce、nonce_missing、unknown。库可能同时返回多个声明错误，保留多个枚举；选钥/获取使用代码自有 sentinel 分类。未知错误不猜测原因。

业务 verifyOIDCIDToken 仅增加错误包装以保留 errors.Is 分类；没有改变 parser、验证参数、公钥规则、请求次数或时钟偏差。nonce 错误在现有分支增加固定分类。配套 strict exporter 只接受固定字段及枚举，拒绝原始错误和虚构 at_hash PASS。

合成测试新增：解析、拒绝 RS384、错误 RSA 签名、未知 kid、issuer/audience 不符、过期、未来 iat/nbf、缺失 iat，以及日志关闭/敏感错误不回显。另以本地 Mock 交替返回两个无 kid 公钥，验证匹配时成功、不同节点键时安全失败，验证每次 fetch 不缓存/重试，带未匹配 kid 不回退到无 kid 键。既有审核映射与 SourceID Session 测试随 CI 回归复核。

诊断代码提交 d0b5ad79ff8f2b05870e2e8c979e776fce15d8ba 的 [CI 全部通过](https://github.com/suoak/WeKnora/actions/runs/38677283016)，包含新增合成失败分支、无 kid 节点差异、竞态、认证/MCP、PostgreSQL 和前端回归。本地 Python 导出器与拒绝敏感字段测试 PASS。本轮没有部署诊断代码，当前运行版本仍 468410e89；后续阶段 B 提交的组合 CI 单独记录。

## SID 必须确认

1. 11:24:27.630887135 附近该笔 Token 的签名节点、RS256 公钥版本及该时段 JWKS 各节点返回是否一致；无需提供 Token 原值。
2. `/oidc/jwks` 在 11:37:29 的条目指纹变化是否只是 kid 元数据差异，还是实际轮换？11:56:18 的 RSA 材料复核一致。请核实所有节点是否共享签名密钥、是否存在每节点生成密钥/重启轮换，以及轮换重叠期、kid 和缓存策略。
3. Token 签发节点时钟、iat/nbf/exp 生成规则是否一致；当前成功不意味着历史所有节点时间正常。
4. 身份/状态/可信会话契约按阶段 B 清单确认；这些问题不再作为 Token 401 阻塞原因。

完成：历史 Trace 分析、代码检查、公开 JWKS/时间只读取证、安全分类开发、合成测试及 CI。仍未知：历史具体失败原因、节点/轮换原因。禁止以成功两笔掩盖中间失败；在部署诊断代码并取得另行授权后，才在真实测试中取得新类别。

证据：[真实事务](evidence/after-secret-reset-real-results-20261015.json)、[首次 JWKS/时间检查](evidence/phase-b-jwks-time-readonly-20261015.json)、[排除 kid 元数据的 RSA 材料复核](evidence/phase-b-jwks-key-material-readonly-20261015.json)。

组合阶段 B/自动关联提交 523d7ab49 的 [CI 也全部通过](https://github.com/suoak/WeKnora/actions/runs/38679739604)，[测试汇总](evidence/phase-b-auto-binding-ci-20261015.json)。本轮仍未部署，未重新发起 SID 登录。
