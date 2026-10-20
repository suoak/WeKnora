# KnowHub SourceID 审计（2026-10-12）

审计基线：`integration/tencent-upstream-20261009`，`ac7dbe5e2607af5ab4e74c062137e0cad48d0e50`。
实现分支：`feature/knowhub-sourceid-oidc`。交付 SHA 以本目录测试报告和 Actions 的 `head.txt` 为准。
开始时工作树含 MCP 集成 UI、国际化、agent-editor 和 QA 等既有未提交改动；本任务不提交这些文件。

## 源码结论

| 模块 | 审计结果与改动 |
|---|---|
| `routes_auth_tenant.go` | 已有 config/url/start/callback、login/logout/refresh/switch-tenant/validate；新增批准路径 backchannel-logout 和 result |
| `service/user.go` | OIDC 授权码 POST 交换、Discovery、SSRF 安全 HTTP、RSA JWKS 验签可复用；原登录按 email 查询并自动开户，SID 改为审核映射 |
| `utils/oidc_state.go` | HMAC、10 分钟有效期、未来时间限制；nonce cookie 绑定浏览器；原来只清 cookie，无服务端单次消费，SID 增加持久化事务 |
| ID Token | 原有 RS256/384/512、iss/aud/exp/sub 校验；补 nonce、iat、azp 和 UserInfo sub 一致性 |
| JWKS | 原实现每次 HTTP 获取，无缓存；保留每次刷新，避免引入未经验证的轮换缓存。网络失败拒绝认证 |
| 本地 JWT | Access 24 小时、Refresh 7 天，数据库每次验证撤销；新增随机 jti 防止同秒签发相同 JWT，SID 会话 ID 随派生令牌保持 |
| Refresh | 原刷新撤销写入错误被忽略；SID 使用会话锁和旧刷新行条件更新，在同一事务内消费并写入新令牌 |
| Logout | 原本按 user_id 撤销所有本地令牌；SID 本地退出改为当前会话族，保留本地登录既有语义 |
| 空间切换 | 现有成员角色、跨空间判断、落地偏好保留；SID 派生会话从已认证 access token 继承，提交 refresh 时另校验归属 |
| User | UUID ID；username/email 唯一；is_active、is_system_admin、can_access_all_tenants、preferences、软删除；绑定不写这些字段 |
| 成员/共享 | `tenant_members` 及知识库授权仍由现有服务判断；SID 身份成功不授予额外权限 |
| 前端 | 复用已有 OIDC 按钮、权限加载和落地恢复；保留邀请与目标页暂存；配置接口公开 callback URI、不公开 secret；原 fragment 令牌改为 HttpOnly opaque cookie + 一次性 POST 兑换 |
| API Key/MCP | 独立认证保持；只增加两条无本地 JWT 的认证路由，登出处理另做信任验证 |

## 协议证据

读取用户提供的三份 HTML。OIDC 飞书 HTML 正文仅导出到 code 换 token 的部分，出现的是示例域名、`/cas/oidc/authorize`、`/cas/oidc/accessToken`、`scope=openid`。
不能据此拼接正式 SID 生产接口。OAuth2 导出描述工号、多身份和传统 profile，不证明 OIDC 使用相同字段。

[SourceID 官方应用登出](https://sourceid.ruishan.cc/linkid/authentication/introduction/logout.html) 确认服务端 POST 的 `logoutRequest`、XML 的 SessionIndex、传统 OAuth 的 tgt 关联。公开文档未给出压缩实现、通知认证和响应约定。
[企业 OIDC 文档](https://ruijie.feishu.cn/wiki/OMhSwh3G1iApPBklVW6cLzC2nVb) 已在用户授权后通过飞书 CLI 读取 revision 20 全文及全部12张正文嵌入参数表，另检查1张空白Sheet1（工作簿共13张；更正原报告计数）。示例描述 `/cas/oidc/profile` 的 access_token 表单参数；新增显式 UserInfo 请求方式配置，保留通用 OIDC 的 Bearer GET 默认值。文档没有确认企业员工号字段、状态字典、正式 issuer/JWKS、nonce/PKCE，亦没有 OIDC 会话与 SessionIndex 的对应关系。证据与未确认项见 [SOURCEID_PROTOCOL_REVIEW.md](SOURCEID_PROTOCOL_REVIEW.md)。

## 必须保留的外部门禁

`BLOCKED_EXTERNAL`：正式 issuer、Discovery/JWKS、签名算法、客户端认证、PKCE/nonce 支持；真实员工与岗位身份规则；员工状态字段；OIDC 与 SessionIndex 的等价字段及来源；来源信任、压缩和响应/重试契约。
实现没有这些字段的猜测性默认值。SourceID 默认禁用，需要显式确认契约后启用；入口网关认证是本地拟定方案，尚不代表 SID 原生支持。

实施顺序：迁移 → 审核账号映射 → Mock/数据库测试 → Linux 回归 → 真实协议确认 → 获授权的测试环境联调 → 发布审核。
