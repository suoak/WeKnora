# 飞书 OIDC 协议核对（2026-10-12）

来源：[用户指定企业 OIDC 文档](https://ruijie.feishu.cn/wiki/OMhSwh3G1iApPBklVW6cLzC2nVb)，revision 20。经用户授权，使用飞书 CLI 的用户身份读取全文，并读取正文全部 13 张嵌入参数表的完整 used range。此前 HTML 导出正文不完整的问题已解除。原始正文、表格、示例密钥、JWT 和个人信息不提交仓库。

| 证据 | 对实现的影响 |
|---|---|
| 授权码模式、scope 可选，示例 openid；code 单次使用、10 秒过期 | 保留授权码流程；回调需及时交换。生产有效期仍需确认 |
| 示例授权、Token、Profile 路径为 `/cas/oidc/authorize`、`/cas/oidc/accessToken`、`/cas/oidc/profile`，部分 demo 表格省略 cas 前缀 | 不能从 demo 拼接正式端点；继续要求确认后的显式配置或 Discovery |
| Token 接口支持 POST，client_secret 参数；正文描述 form-data 或 URL 参数 | 保留 POST client_secret_post；现有 Token 使用 urlencoded，需确认正式端点接受此 Content-Type。不使用 URL 传密钥 |
| Profile 支持 GET/POST，access_token 为必填参数，正文 POST 使用 form-data | 新增 `OIDC_AUTH_USER_INFO_TRANSPORT` 的 form_post/multipart_post；默认 Bearer GET 保留，SID 应按正式契约显式配置。不使用 URL 传访问令牌 |
| Profile 返回 sub、id、auth_time、attributes；表格中的 login 是用户输入的账号，可能 gid 或学工号，zjhm 是证件号码（学工号） | 不能将 login/gid/zjhm 自动认作企业自然人工号。嵌套 claims 机制可配置，但必须由企业身份负责人确认实际字段 |
| ryzxztdm 描述为在校状态码，有字典但未给出字典值 | 不能配置猜测的员工启用状态或允许值 |
| ID Token 示例为 RS256 JWT；示例 issuer、subject 与 Profile 示例不能构成同一次认证证据，nonce 示例为空 | 不关闭签名、nonce 或 sub 一致性校验；示例不证明实际 SID 支持 nonce，也不能充当真实密钥/issuer 配置 |
| 本文没有 tgt/SessionIndex 关联、通知认证、压缩和响应约定 | 精确登出所需会话契约仍为 BLOCKED_EXTERNAL |

尚需 SID 方提供：正式 issuer/Discovery/JWKS 与算法；支持的 Token/Profile Content-Type、客户端认证及 nonce/PKCE；企业员工自然人唯一标识和状态字典；每次外部会话对应 SessionIndex 的可信字段；通知来源认证、body/query 参数位置、压缩、ACK 与重试/外部 ID 生命周期。

文档仅作为协议参考；其中 demo 的 URL 传密钥、关闭 TLS 校验、输出令牌等做法未采用。没有发起真实 SID 登录或登出，功能开关保持关闭。
