# SID 成功 Postman 截图的新差异

2026-10-14。依据用户提供截图及现有业务源码，只做分析；未请求 authorize/Token，未保存或解码截图中的完整 code、Token、Basic 或 Cookie。

1. KnowHub scope 为 openid，与管理员链接及成功响应 scope 相同，不是已发现差异。
2. 截图 Token 表单增加 state 字段；KnowHub 表单没有 state，但在 callback 严格校验 state。标准授权码 Token 请求不要求 state；SID 是否有自定义要求仍需确认。不能仅凭截图认定缺少 state 导致 401。
3. 管理员描述的 authorize 使用 callback1，Token 截图却使用 callback；两截图可见的 code 也不同。因此不能证明此 Token 200 消费的就是浏览器 callback1 那一笔。应让管理员确认同一次事务的实际 redirect_uri；标准要求授权和兑换的 redirect_uri 相同。
4. 截图 ID Token 可见头部看起来标为 HS256。这仅是未验签截图观察，不能视为可信声明或推断 claims。与已发布 RS256 Metadata 及当前仅接受 RS256 的业务配置不一致。需管理员确认客户端级签名算法、注册配置版本和正确验签方式；不改变算法、不以 JWKS RSA 公钥或其他猜测值验证 HS256。
5. 成功响应区域显示 Cookies (2)。不能据此推断 Token 请求携带 Cookie，更不能推断必须 Cookie 认证。请只确认成功请求是否携带 Cookie 的布尔值及是否依赖它，不提供原值，不将企业浏览器 Cookie 导入后端。
6. callback1 返回 missing authentication 的浏览器 401，是未注册路径进入业务鉴权的结果；源码只有 callback 的公开 Handler，没有 callback1。该页面并不能证明 code 已消费或未消费。未知 callback1 仍可能经过访问日志/APM，不以随意改路径作为安全的 code 采集入口。

请 SID 只确认三组最小信息：Token 正文 state 是否必填；同一成功事务 authorize/Token 的 redirect_uri 是否一致；knowhub 正式 ID Token 为什么显示 HS256、应使用哪种经确认的签名契约。成功请求凭据版本、Basic 组件编码及请求 Cookie 存在性也只提供非秘密状态。

若后续安排 Postman 单次测试，需要经批准的接码入口和新的未消费 code，并在授权请求保留随机 state、nonce。不使用固定 abc123 代替业务 state，不调用业务 callback 消费后再兑换，不复用截图旧 code。本轮没有创建新回调或修改已批准 callback。

参考：[RFC 6749 §4.1.3](https://www.rfc-editor.org/rfc/rfc6749#section-4.1.3)。
