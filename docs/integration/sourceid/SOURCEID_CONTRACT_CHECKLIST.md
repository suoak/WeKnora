# 正式 SID 协议契约清单

本表记录能力与待签署契约，**没有任何正式 SID 字段被标为真实联调已验证**。F = 企业飞书 OIDC revision 20 全文、12 张正文参数表及空白 Sheet1（工作簿共13张）；S = [SourceID 应用登出](https://sourceid.ruishan.cc/linkid/authentication/introduction/logout.html)。F 的示例域名/字段和值不用于生产。已明确 = 文档说明，不等于该企业 SID 配置已确认。

代码位置：C=internal/config/config.go，U=internal/application/service/user.go，I=internal/application/service/sourceid.go/sourceid_claims.go，R=internal/application/repository/sourceid.go，H=internal/handler/auth_sourceid.go，X=internal/sourceid/logout.go。逐项确认需 SID 负责人、KnowHub 负责人、验证日期及脱敏证据链接；空项视为阻塞。

| 协议字段/契约 | 文档是否明确 | 当前实现 | SID 方最终确认 | 验证方法 | 不符合时策略 |
|---|---|---|---|---|---|
| issuer / discovery | F 只有 JWT 示例 issuer，无正式 Discovery | C 可分别配置；SID 要求显式 issuer；U 如执行 Discovery 则精确比较 issuer | 正式 issuer（含路径/末尾斜杠）、Discovery URL/是否无认证公开、别名规则 | 对确认 URL 有限只读 GET；元数据与真实签名 token iss 比较 | 不猜 /cas；不关闭 issuer 校验；没有 Discovery 可显式端点，仍须可信 issuer/JWKS |
| authorization_endpoint | F 示例 /cas/oidc/authorize，demo 某处省 cas | U 显式或 Discovery；SSRF校验、302跳转 | 正式完整地址与允许响应类型 | 检查注册配置；授权后 A 用例 | 地址不符先改配置；不枚举候选 URL |
| client_id / redirect_uri | F 要求两者；项目审批 knowhub 与固定 callback | 固定 callback 校验；配置 client_id；GET callback | 应用注册精确 URI、应用租户/环境、是否强制 HTTPS | 审批凭据与注册配置双人核对；A验证 | 不静默改 HTTPS；新 URI 走新审批与必要代码适配 |
| response_type / scope / code | F code模式；scope可选；示例openid；code单用10秒 | U code+scope；回调立即POST交换；本地state一次消费 | 正式scope与claims授权、code真实TTL和单用规则 | 脱敏时间线、授权后成功/过期/重放受控测试 | 不重放真实code，不扩大scope；过期重新授权 |
| state / nonce | F 表格未列 state/nonce，JWT示例 nonce为空 | SID state持久化单用；浏览器nonce cookie；已签名token nonce必须精确匹配 | state是否原样回传，nonce是否进ID Token | A成功+隔离Mock负例；真实试验需授权 | 不关闭nonce/state；不支持时停止并要求SID协议升级 |
| PKCE S256 | F 未说明 | C可选S256，verifier存DB，公开state不含它 | 支持/要求与client类型 | SID正式说明；授权测试challenge/verifier | 未确认保持false；SID强制其他方式则补专项适配/CI |
| token_endpoint / HTTP编码 | F POST/GET、form-data或URL示例 | U只POST application/x-www-form-urlencoded JSON响应 | 正式地址，是否接受urlencoded或仅multipart | SID HTTP契约/测试端点说明；A授权后交换 | 不GET传secret；仅multipart须最小代码适配与CI，不能把Profile选项误用到Token |
| token客户端认证 | F client_secret参数；Basic无说明 | client_secret_post/basic，其他拒绝 | 正式方法、凭据轮换、是否要求mTLS/private_key_jwt | SID配置证据；A交换 | 不尝试多个方法；不支持的方法另评估最小适配 |
| access_token / id_token / token_type | F表格两令牌必填、类型/有效期 | SID必须ID Token与JWKS；userinfo选中时还必须access_token；解析JSON，限定响应体 | 实际JSON类型与字段、bearer语义、错误格式 | 脱敏结构与A交换结果 | 缺失/异常拒绝；不信任未验签token |
| expires_in / refresh_token | F token有效期；demo另列refresh_token，主接口未保证返回 | 外部refresh_token不用于本地刷新；本地access24h/refresh7天，SIDsession最多7天 | SID session TTL、是否要求在线刷新/状态同步、本地有效期是否符合策略 | 审批策略、E超时与F刷新 | 不把外部refresh当本地refresh；生命周期不符先停联调并评估 |
| jwks_uri / 密钥轮换 | F未给正式地址 | U用SSRF安全HTTP每次获取RSA JWKS，无缓存 | 正式无secret地址、CA/证书、kid/轮换窗口/故障策略 | 确认URL只读GET；签名和受控轮换验证 | 禁止手工相信示例公钥/关闭TLS；无可验签来源停止 |
| ID Token alg/kid/iss/aud/azp | F JWT示例RS256，不证明生产算法 | RSA RS256/384/512；issuer/audience/azp校验 | 生产算法、aud knowhub、多aud/azp、kid唯一性 | SID签名元数据与A；Mock算法/iss/aud/azp负例 | ES/HS等不支持则阻塞并最小适配；不接受alg none |
| exp/iat/nbf/sub | F示例含标准字段；无正式时钟/subject生命周期规则 | 验签后校验时效/iat；sub必须非空字符串 | 时钟偏差/NTP、sub自然人与岗位身份关系及稳定性 | 服务器NTP证据、A/B与身份负责人证明 | 不数字强转/空白归一化；sub轮换或多身份不得静默转绑 |
| profile_endpoint / transport | F /cas/oidc/profile GET/POST，POST form-data access_token | bearer_get（默认）/form_post/multipart_post，不URL传token、不失败降级 | 正式URL、精确编码、是否接受Bearer | SID契约；A受控Profile成功；现有Mock已覆盖三模式 | 按确认值配置；其他模式先最小适配/专项验证 |
| Profile sub 与 ID Token sub | F独立示例值不同，无同次认证证明 | UserInfo sub与验签ID Token sub精确一致，否则拒绝 | 同次调用的subject一致性、授权主体多身份规则 | 一次授权对应的脱敏结构证明；B | 不取消比较；SID不一致需SID修正 |
| 员工自然人工号 | F login可为输入gid/学工号，zjhm描述学工号，不是企业工号契约 | I仅显式dot-path字符串、保留前导零；审核enrollment | 可信claims来源和稳定自然人唯一字段、可变/离职再入职规则 | HR/SID双人核验；B及审计CSV | 不用email/login/gid示例猜；未知员工拒绝 |
| 员工状态 | F在校状态码及有字典标志，未给员工字典值 | I明确path+允许值；字符串或bool精确匹配 | 员工启用/冻结/离职值及更新时效 | 状态字典与隔离禁用测试 | 不允许缺字段默认启用；状态不符拒绝 |
| 业务claims来源 | F Profile attributes；ID Token不保证这些属性 | id_token或userinfo全选一来源；不混用业务字段 | 员工/状态/会话各字段能否在同一可信来源获得 | SID脱敏字段树，Profile subject验证 | Token Response来源未实现；需不同来源则先有正式契约再适配 |
| OIDC会话 ↔ SessionIndex | F缺；S仅说明传统CAS ST、OAuth tgt | I读取明确会话path，R按SHA256(issuer+NUL+index)持久化 | OIDC具体字段/来源、是否每设备独立、与通知值的等价关系 | 同员工两个独立SID会话的A/D/E/G脱敏相关性证明 | **不能工号/sub/email/jti/auth_time替代**；缺来源停止。若在Token Response中须适配 |
| 单设备/全局SID退出语义 | S介绍主动退出/超时后台通知，未保证OIDC设备范围 | 同一issuer+index全部本地派生会话撤销；其他index保留 | 不同浏览器是否共享TGT、一次logout是否有多个通知 | 两独立会话观测D/E/G通知集合 | 同index应一起撤销，不承诺设备隔离；必要时更改测试前提或SID配置 |
| 通知地址/方法/参数位置 | S POST logoutRequest，但URL例子携query；F无 | H仅批准POST路径、单body字段、拒绝任何query | 注册通知URI，body还是query，Content-Type及代理要求 | 正式原始HTTP结构（安全脱敏）与隔离mock转发 | query-only/multipart/XMLbody均当前不支持；不开放兼容接收，契约确认后最小适配 |
| XML结构/SessionIndex/NameID | S SAML2 LogoutRequest示例 | X检查namespace/version/ID/IssueInstant+单直接SessionIndex；NameID不用于撤销 | 实际namespace/version、多index/issuer/destination需求、index长度与字符 | SID真实脱敏结构；H恶意/格式负例 | 多index等不支持则拒绝/适配，不把NameID作为用户撤销键 |
| 压缩/编码 | S提到解压，不提供算法 | xml或显式base64+raw DEFLATE；限制64KiB展开、128KiBbody、depth≤8；拒绝DTD | 原始XML还是压缩、base64变体、DEFLATE/zlib/gzip、最大尺寸 | 供应方脱敏样本/测试fixture重放到隔离Mock | 不自动猜压缩；不超限/关闭XXE检查；按确定算法补适配 |
| 通知真实性/来源 | F/S没有可用认证契约 | H仅可信直接peer CIDR+≥32字节内部secret恒时比较，未验XML签名 | SID原生签名/mTLS/专网信任、来源IP/NAT/证书、入口认证与责任边界 | 两层来源认证证明、伪造H、禁止backend bypass | 内部header不是SID签名；禁止开放公网/仅信未验证头；必要时原生验签适配 |
| ACK/错误/重试/送达 | F/S未明确 | 空200成功；403/400/415/503失败；未知index也200+tombstone | ACK body/status、超时、重试/backoff、通知丢失处理/SLA | D/E正常+隔离数据库故障/重试测试 | 不提前200或吞错；不满足ACK则补适配，不声称故障时已注销 |
| IssueInstant/ID重放/index生命周期 | S仅示例时间/ID，无窗口/生命周期规则 | 时间格式检查，无新鲜度窗口；按index幂等tombstone，不自动清理 | 时钟偏差、合法重试时限、外部index不复用/长度、保留期 | 延迟/重复H、重启D/F、SID生命周期证明 | 不随意丢延迟通知或清tombstone；index复用不明保持阻塞 |
| 敏感日志/审计 | demo有不安全打印；不是采用依据 | 业务日志/SQL局部保护；通用HTTP body与默认代理access_log仍有风险 | SID/网关抓包保密与日志审计要求 | synthetic marker测试网关/应用/APM全部日志，见发布门禁 | 真实联调前完成敏感路由日志排除，必要的窄补丁与云端验证；不收集原始token/XML |

所有不符项先关闭测试SID入口并保留本地管理员；不能用删校验、扩大IP段或绕过身份审核推进。变更优先配置；只有获得具体正式协议差异后才修改受影响适配并重新验证。缺通知认证或会话等价证明时，A–I真实联调均保持等待，不能仅完成登录后宣称P6成功。
