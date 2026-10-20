# 给 SID 管理员：新版 KnowHub 真实 Token 401 诊断

2026-10-14，打包机 172.30.232.42，业务版本 `3be5d3f9268fe8819d66485a2b990c6237827e25`。由用户手动操作浏览器；Agent 仅只读提取脱敏日志，没有发起登录、复用 code 或修改认证参数。

**结论：Token 接口仍为 HTTP 401，未取得 ID Token 或 UserInfo。新增诊断已取得实际连接及 Header 写出证据。请暂停重复点击，直接对照 SID 网关和 Token 服务日志。**

## 优先关联最后一笔

| 项目 | 实际记录 |
| --- | --- |
| 请求写出时间（北京时间） | 2026-10-14 20:04:14.419494239 +08:00 |
| 对应 UTC | 2026-10-14T12:04:14.419494239Z |
| 本地 Trace | 2196147b-8d94-4ad1-aa1f-d69493da1331；不代表已发送为 SID Header |
| 请求 | POST https://sid.ruijie.com.cn/oidc/accessToken |
| 实际远端连接 | 39.107.195.76:443 |
| DNS / SID TCP / TLS | DNS 5 ms；到该远端 TCP 46 ms；TLS 握手 52 ms，无网络/TLS 错误 |
| TLS / HTTP | TLS1.3 / HTTP/1.1；无 ALPN 协议协商值 |
| Client ID / 认证 | knowhub / client_secret_basic |
| Header | Basic 存在且格式检查通过；构造 1 个 Authorization 值，实际写出 1 个 |
| Basic 策略 | Client ID、Secret 分别一次 OAuth 表单编码，再 SetBasicAuth Base64；未更改原策略 |
| Secret 加载 | 非空=true；不证明与 SID 注册版本匹配，不输出值或指纹 |
| Content-Type / Accept | application/x-www-form-urlencoded / application/json |
| 实际写出 User-Agent 分类 | Go-http-client/1.1 |
| 表单 | client_id、code、grant_type、redirect_uri；各单值，无重复，无未知字段 |
| 参数结构 | grant_type=authorization_code；client_id/redirect_uri 与配置一致；code 单值非空 |
| 不存在的表单字段 | client_secret、code_verifier；PKCE 关闭 |
| redirect_uri | http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback |
| Callback state | PASS，1 ms；不代表 SID code 校验成功 |
| Callback → dispatch / 写出 | 189 / 294 ms |
| HTTP 返回 / 耗时 | 401 / 194 ms |
| 响应正文 | 声明 0 字节、实际读取 0 字节、EOF=true、无截断或读取错误 |
| 响应 Content-Type | absent，未提供 |
| WWW-Authenticate | Basic challenge；realm 存在，只记录存在性 |
| Server / Via | nginx 产品分类 / Via 未出现；不保留原始 Header 值 |
| Date | 2026-10-14T12:04:14Z |
| Proxy challenge | 未出现 |
| SID 关联 Header | X-Request-ID、X-Correlation-ID、Traceparent 均未出现 |
| 次数 / 重定向 | 本事务一次成功写出，未观察到重试或 3xx |
| 自动分类 / 拒绝层 | empty_401_basic_challenge / UNKNOWN |

客户端配置 Transport 不使用代理；这不能排除透明网关。39.107.195.76 是客户端连接的远端，不是 SID 看到的来源 NAT IP。时间为 Go 写出回调观察时间，不冒充服务端接收或抓包时间。网络事件中的 127.0.0.11:53 是容器 DNS 解析器，不是 SID HTTP 连接。

## 本次采集窗口

20:00 开启后，至 20:04:52 提取时发现 **8 个不同 callback Trace**，并非只有一笔。每个 Trace 都只有一次 Token 写出且 state PASS；不能将这 8 个事务解释成同一笔 HTTP 自动重试，也不能证明各 code 从未被其他方消费。

| 北京时间（2026-10-14，写出事件） | HTTP | 耗时 ms | callback→写出 ms |
| --- | --- | --- | --- |
| 20:01:16.712545625 | 401 | 195 | 286 |
| 20:01:31.766526364 | 401 | 451 | 5711 |
| 20:03:32.058449037 | 401 | 189 | 281 |
| 20:03:47.243932530 | 401 | 187 | 284 |
| 20:03:59.596366315 | 401 | 176 | 342 |
| 20:04:03.075821954 | 401 | 223 | 329 |
| 20:04:09.231360905 | 401 | 173 | 277 |
| 20:04:14.419494239 | 401 | 194 | 294 |

全部连接同一远端、TLS1.3/HTTP1.1、空正文 Basic challenge、Server nginx 分类。7 笔 callback 到发送不足 350 ms，另 1 笔为 5711 ms；较短延迟仍出现相同 401，因此不能将全部失败简单解释为 callback 排队触及 10 秒限制。SID 从签发 code 起计算的实际有效性仍需服务端判断。

Authorize 阶段有成功记录，浏览器 callback 接收到 code 且 state 通过。员工认证由用户报告完成；客户端没有获得经验证的员工身份数据，不能独立确认具体员工或服务端认证详情。

Token 失败后安全停止，ID Token 签名/Issuer/Audience/nonce、UserInfo/sub 和员工契约门禁均未到达，标为 NOT_TESTED；没有新增验证后的 claims/schema。不将 nonce 的 NOT_YET_EXECUTED 当作 nonce 校验失败。

## 与之前的差异及 SID 最小核查项

17:09 的业务请求同样为空正文 401 + Basic challenge；旧记录没有 Server、远端、TLS、HTTP 信息。本次补齐这些字段，仍未定位拒绝组件。Basic Header 格式正确、Secret 非空不能证明凭据有效；nginx 产品分类也不能证明 auth_basic 就是拒绝原因。

请 SID 管理员对照最后一笔时间，检查：

1. 39.107.195.76 对应的入口及实际服务节点；网关 access/error 中 status、upstream_status、upstream_addr、内部 request_id，以及是否进入 Token 服务。
2. Basic 客户端认证结果和安全拒绝类别；knowhub 注册协议、允许认证方式及凭据版本是否与切换后 Postman 200 相同。不要发送密钥或 Basic 原值。
3. 2026-10-13 OAuth2→OIDC 的配置生效/缓存版本是否在所有入口与节点一致。
4. 失败请求实际来源 NAT IP及来源策略；成功 Postman 的精确时间、DNS/入口 IP/节点、User-Agent、Accept、HTTP 版本和 Basic 组件编码规则是否一致，是否有针对客户端特征的网关规则。
5. 若确实进入 code 校验，确认签发时间、匹配客户端/redirect_uri、是否消费；不要据客户端 state PASS 或 HTTP 401 推断。

已有证据更支持优先排查客户端认证/注册配置或前置认证策略；仍不能区分凭据版本、编码兼容性与网关规则。暂不更改 Basic、PKCE、nonce、state 或签名验证，也不再盲测。

按用户要求 OIDC 持续开启，身份协议验证门禁保持不变；本轮没有为了采集日志关闭或重启服务。专项诊断仍受既有体积/时间限制。

附件：[完整三阶段脱敏 JSON](evidence/business-real-token-diagnostics-20261014-2000.json)、[时间线 JSON](evidence/business-real-token-summary-20261014-2000.json)、[证据图](evidence/business-real-token-evidence-20261014-2000.png)。后端 Token 请求不是浏览器业务 API 401，也不能通过普通 F12直接捕获。
