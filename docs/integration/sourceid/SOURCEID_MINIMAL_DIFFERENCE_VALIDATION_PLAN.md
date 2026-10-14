# SID Token 401 最小差异验证方案

日期：2026-10-14。状态：**合成验证 PASS；真实验证 NOT_STARTED，等待 SID 认证配置和凭据版本确认**。本轮不修改 SID/KnowHub 参数，不读真实凭据，不生成授权事务，不兑换 Token。

## 已知最终请求

历史样本 Trace：2196147b-8d94-4ad1-aa1f-d69493da1331；北京时间20:04:14.419494239 / UTC12:04:14.419494239。

- POST https://sid.ruijie.com.cn/oidc/accessToken；Host 与 endpoint 相符，无 query。
- client_secret_basic；Authorization 构造和写出均为一项，格式检查通过。
- 策略：Client ID/Secret 各一次 QueryEscape，再 SetBasicAuth/Base64；Secret非空不代表凭据有效。
- Content-Type=application/x-www-form-urlencoded，Accept=application/json，实际User-Agent分类Go-http-client/1.1。
- 正文grant_type、client_id、redirect_uri、code，各单值；grant_type=authorization_code，client_id=knowhub，redirect_uri=http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback；配置一致检查通过。
- 无正文client_secret/code_verifier/state。state在授权和callback校验，nonce加入授权；进程scope检查为openid。完整授权URL没有保存，不伪装成逐字抓包证据。
- 源码无Token Cookie/Proxy-Authorization/Client CookieJar；这些不是当前真实日志逐Header抓包字段。
- 实际连接39.107.195.76:443、TLS1.3、HTTP/1.1；配置Transport无代理，透明网关及SID看到的NAT来源UNKNOWN。
- 一次写出、无观察到重试/重定向；callback到写出294ms。
- HTTP401/194ms、正文0字节EOF、Content-Type absent、Basic challenge、Server产品分类nginx；拒绝层UNKNOWN。

## 与成功 Postman 的未确认差异

| 差异 | 当前状态 | 谁确认 |
| --- | --- | --- |
| Basic由认证助手还是显式Header生成；普通UTF-8 Basic还是OAuth组件编码 | UNKNOWN | SID管理员，仅说明算法/配置，不提供真实Header |
| 成功请求凭据版本与服务注入版本是否一致 | UNKNOWN；用户确认已更新DPAPI | SID与用户在安全渠道核对版本 |
| 是否额外Cookie、是否依赖既有企业会话 | UNKNOWN；响应Cookies不能证明请求Cookies | SID仅提供请求存在性/作用 |
| 代理、Proxy-Authorization、出口IP、DNS/服务节点 | UNKNOWN；本机Windows有代理不代表管理员 | SID仅提供非秘密配置 |
| Token state是否必填 | 截图有，标准不要求，SID要求UNKNOWN | SID明确是否为扩展契约 |
| 同一成功事务redirect_uri及code来源 | 描述callback1、正文callback，截图code不同 | SID确认同事务两阶段一致 |
| nonce在管理员成功授权中是否存在 | 所给手工URL无；KnowHub有 | SID确认支持带nonce的同等流程 |
| Postman Accept/User-Agent/HTTP版本/隐含Header数量、重定向策略 | UNKNOWN | SID只提供安全值/分类 |

正文Secret和PKCE verifier不是待补字段；管理员已确认Basic Secret仅在Header、SID不支持PKCE。

## 合成 Basic 验证

重跑既有独立Go工具的9类合成凭据，未输出真实值或Basic：普通安全字符等价；+、/、=、%、冒号、空格、非ASCII及包含字面编码序列时，两策略不同。OAuth组件编码经一次表单解码还原原值，单纯Base64解码未还原这些特殊字符。合成loopback请求POST、Basic、四字段及一次兑换语义均正确，Mock401保持不变。

另有已通过的合成补充：短横线/下划线/字母数字组合等价。真实凭据只确认含短横线、无反斜杠和首尾空白，不能推定其他字符类别。若真实值完全无需转义，切换Basic算法没有实验意义。

“Postman普通Basic”在此仅指直接UTF-8 username:password后Base64的候选算法，不冒充管理员实际配置。

## 不改业务代码能否独立复现

**可以独立使用受控Postman或一次性测试客户端执行授权码兑换，不必修改KnowHub。** 但目前真实执行门禁不足：缺少明确的成功认证配置/凭据版本，以及独立完整redirect_uri的确认和安全接码安排。现有无凭据Postman模板可导入，不代表已获得执行许可或新code。

如果选择callback1：

1. SID明确允许完整URI `http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback1`，不能仅因演示可跳转就认定正式已登记；本轮不申请或改SID登记。
2. authorize与Token正文必须使用同一完整URI，包括协议、主机、路径、端口。不能授权callback1却兑为callback。
3. 此路径不能进入KnowHub业务callback，不能被其自动兑换；也不能简单让浏览器落到未知业务路由并依赖401作为“未消费”证明。
4. 接码只能进入隔离的受控入口，先核查访问日志、错误日志/APM不记录query；随机state在接码端校验，nonce保留并在获得ID Token后校验。完整URL/code不发聊天、不落盘、不HAR。
5. 若无法保证上述边界，先确认另一个批准的受控redirect_uri，或用同一已批准callback在专用浏览器/局部路由中拦截到独立接码端；需要具体路由授权，不能直接占用业务Handler或生产网关。

Postman手工复制code可能触及短有效期；如采用此方式，应由用户本机内存中立即传递，不能人工复制到聊天等Agent处理。如果有效期不足，应使用同样隔离入口自动接码并一次兑换，而不是增加重复尝试。

独立兑换成功只证明Token交换；只有签名、Issuer/Audience/nonce/有效期/sub验证成功才可宣称完整OIDC验证。Postman返回200不是自动满足这些校验。

## 最小执行顺序（未启动）

**准备零次真实请求**：确认成功Postman认证策略和凭据版本；关闭其未明确需要的隐含Cookie/重复认证助手；确认来源策略、批准redirect_uri与隔离接码；其他协议参数保留。与KnowHub基准仍存在多项差异时，不宣称单变量实验，先列出并尽量对齐。

**优先验证客户端认证一致性**：SID先用纯合成含特殊字符的组件解释解析规则，并在安全渠道确认同版本凭据。若发现确实不同，再选唯一变量（例如组件编码算法），其余客户端、网络、redirect_uri、state/nonce行为都固定。每种需要授权的真实事务都获取自己的全新code，只兑换一次；不能把同一code分别给两个客户端作A/B对照。结果失败即停，不自动第二笔。

如仅需验证请求来源，应先完全对齐认证算法/版本，再使用独立客户端的一笔新code；同时固定Cookie、Token正文state等。管理员示例含state但尚未确认其作用，直接照抄后与KnowHub比较会混入额外变量，不能据成功归因来源。

**其次考虑Token state**：仅在SID确认这是必需扩展或同等认证验证仍指向该项时，另行提出仅改变Token正文state存在性的方案。使用对应授权事务的实际state，不能固定abc123；不改变callback原有校验。需要新增schema安全过滤和合成测试后才能适配，当前不修改。

所有真实执行均等待用户单独授权；任何401或安全验证失败停止，保留白名单证据。业务nonce、state、RS256及身份契约门禁不变，不开户、不绑定、不签发企业JWT。

## SID需要给出的最小答复

1. 成功Postman Basic的生成算法、认证助手配置和凭据版本是否相同。
2. 请求Cookie/代理认证是否存在且是否必要，来源入口是否一致。
3. Token state是否必填。
4. callback1完整URI是否允许，同一成功事务授权和兑换是否严格一致。

证据：[合成Basic结果](evidence/minimal-difference-basic-synthetic-20261014.json)、[历史完整脱敏诊断](evidence/business-real-token-diagnostics-20261014-2000.json)。标准依据：[RFC6749 §2.3.1](https://datatracker.ietf.org/doc/html/rfc6749#section-2.3.1)、[§4.1.3](https://datatracker.ietf.org/doc/html/rfc6749#section-4.1.3)。
