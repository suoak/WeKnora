# 给 SID 管理员：KnowHub Token 401 最小核查材料

本轮未重新执行真实 SID 登录、未修改 SID、未提供任何原始凭据。经后续授权已升级打包机诊断版本并完成本地 Mock 验收，OIDC/诊断最终关闭；没有新增正式 SID 返回数据。

**请优先查北京时间 2026-10-14 17:09:00.746727 附近的网关及 Token 服务日志。** 对应 UTC `2026-10-14T09:09:00.746727197Z`，KnowHub 本地 Trace `d69e9f7f-e165-4b56-b67b-ed2875cf2677`。Trace 为本地关联标识，不代表已发送到 SID 的 Header。

| 已有实际证据 | 值 |
| --- | --- |
| 请求来源环境 | 打包机 172.30.232.42；SID 看到的出口 NAT IP UNKNOWN |
| 请求 | POST https://sid.ruijie.com.cn/oidc/accessToken |
| Client ID / 认证 | knowhub / client_secret_basic；Header 存在且格式检查通过，不证明凭据有效 |
| Content-Type / 字段 | application/x-www-form-urlencoded；grant_type、client_id、redirect_uri、code |
| grant_type / callback | authorization_code / http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback |
| 排除的表单字段 | 无 client_secret、无 code_verifier；无 PKCE |
| state / 发送延迟 | state PASS；callback→dispatch 5206 ms，→WroteRequest 5320 ms |
| 返回 | HTTP 401，212 ms，正文 0 字节/EOF，Content-Type 未提供 |
| WWW-Authenticate | Basic；realm 存在但未保存原值 |
| 关联 Header | X-Request-ID、X-Correlation-ID、Traceparent 均未出现 |
| Server/Via/网络协议 | NOT_CAPTURED；拒绝组件 UNKNOWN |

这是 **KnowHub 后端 HTTP 诊断记录，并非浏览器 Network 抓包**。浏览器 `/api/v1/agents` 等业务 API 401 不作为此 Token 401 的证据。

已知 2026-10-13 knowhub 从 OAuth2 切换 OIDC，且对接人确认切换后 Postman Basic + URL 编码表单成功返回 200。请核对：

1. 修改的精确生效时间，以及失败请求实际节点的客户端注册配置/缓存版本；是否全部节点一致。
2. 失败请求是在网关 Basic 校验、Token 客户端认证还是授权码校验被拒绝；请提供安全原因类别及组件名。
3. knowhub 当前认证方式和凭据版本，是否与切换后成功 Postman、人工更新的 KnowHub 凭据属于同一版本；不发送值或 Basic 头。
4. 成功 Postman 的时间、服务节点、来源 IP、callback、非秘密参数及 Basic 组件编码策略。是否接受 RFC 6749 的凭据组件表单编码；是否与普通 Basic 的直接 UTF-8 算法不同。
5. 失败请求的实际来源 IP、域名/Host/IP 策略命中情况；code 是否有效、是否已消费。不能据 state PASS 或 HTTP 401 推断这些结果。

当前 Go 构造为 Client ID/Secret 各一次 QueryEscape，再 SetBasicAuth Base64；未发现重复 Header 编码。特殊字符的两种 Basic 算法确有差异，但未证明这是根因；不会先行更改策略。Postman 成功示例同时包含 Basic 和正文 client_id，因此该组合本身没有被证明不兼容。

新增诊断已部署并通过合成验收：请求结构、实际 DNS/TCP/TLS/远端地址/写出/首字节时间、HTTP 版本、响应长度和安全 Header 分类。**尚无新增真实 SID 数据**。如果历史服务端日志不足，请说明最少缺哪项；下一次真实认证需用户另行批准，不自动重试。

附件：[机器可读历史及缺失字段](evidence/token-http-diagnostics-package-status-20261014.json)、[脱敏证据图](evidence/token-http-diagnostics-evidence-20261014.png)、[详细实现及验收说明](SOURCEID_TOKEN_HTTP_REQUEST_RESPONSE_DIAGNOSTICS.md)。
