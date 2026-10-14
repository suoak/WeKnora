# SID Postman 差异后续只读核查

2026-10-14 北京时间约 20:31。未执行真实登录或 Token 请求，未修改认证配置，未访问生产服务器。

管理员补充确认 Secret 只在 Basic Header，不在正文。其成功 curl 也没有 code_verifier；当前 KnowHub 四字段结构与之相符。仍不能仅凭字段相符认定 Basic 凭据值、版本或组件编码相同。

本次实际检查：

| 项目 | Windows | 打包机 172.30.232.42 |
| --- | --- | --- |
| SID 当前 DNS | 39.107.195.76 | 39.107.195.76 |
| 直连实际 TLS 远端 | 39.107.195.76:443 | 39.107.195.76:443 |
| TLS 验证 | TLS1.3，通过正常证书校验 | TLS1.3，通过正常证书校验 |
| 公开 Discovery | 按当前系统代理配置请求，200 | 显式无代理直连请求，200 |
| 配置的代理 | Windows 系统 HTTP/HTTPS 代理 127.0.0.1:7897，urllib 对 SID 不绕过 | KnowHub 自定义 Transport 不使用代理；透明网关未知 |

两个路径的正式 issuer 和 Token endpoint 与已配置值一致。Discovery 未公开 client auth methods 和 PKCE methods；不能据字段缺失推翻管理员的 Basic/无 PKCE 确认，也不能据公开元数据成功证明 knowhub 注册及凭据有效。

**代理是待对照项，不是已确认根因。** 本机 Windows 配置不代表 SID 管理员电脑配置，Postman 是否使用系统代理或自定义代理尚未读取/确认；没有读取其私有配置数据库、Cookies 或凭据。

接下来请 SID 管理员只提供非秘密结果：成功 Postman 是否使用代理、当时解析/连接的 SID IP、实际服务节点及出口来源是否与失败请求相同，并在服务端确认两者凭据版本及认证拒绝原因。失败样本仍以北京时间 20:04:14.419494239、Client ID knowhub 为定位依据。

Windows 已安装 Postman，但自动启动客户端和资源管理器被执行策略拒绝（blocked by policy），未提供进一步原因；因此没有自动导入或发请求。可手动打开 Postman → Import → 选择 [无凭据模板](business-test/SID_TOKEN_REQUEST_TEMPLATE.postman_collection.json)。模板不包含真实 Basic 或 code，不能直接 Send；先安排新的、未被 KnowHub 后端消费的授权码和本地受控认证值，不使用聊天示例的旧 code。

机器证据：[postman-network-comparison-20261014.json](evidence/postman-network-comparison-20261014.json)。
