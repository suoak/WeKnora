# SID Postman / KnowHub：state、Basic、Cookie 专项对比

2026-10-14。仅源码、既有脱敏记录和无网络外发的合成 Mock；未读取真实凭据、未调用 authorize/Token、未修改认证或运行配置。

| 项目 | KnowHub 已确认 | SID 成功 Postman | 结论/待确认 |
| --- | --- | --- | --- |
| scope | 启用前进程配置检查为 openid；源码 query.Set(scope, Join(cfg.Scopes)) | 示例 openid | 配置和构造一致；历史完整授权 URL 未保存，不冒充逐字抓包验证 |
| nonce | 源码每次生成随机 nonce 并加入授权 URL | 管理员手工 URL 未含 nonce，样例空 nonce | KnowHub 的发送逻辑确认；单笔授权 URL 的原值/完整报文 NOT_CAPTURED，不能以 Token 401 推断 SID 是否接受 nonce |
| redirect_uri | 公共配置为批准 callback；Token 诊断与配置一致，授权/回调代码沿用同值 | 链接 callback1，兑换截图 callback，且截图 code 不同 | 尚不能认定同一 code 配了不同 URI；请 SID 核对同事务一致性 |
| state | 授权 URL 带服务端签名 state；callback 验证并单次消费；Token Body 不含 state | Token 截图包含 state | 标准 Token 参数不要求 state；SID 自定义要求 UNKNOWN，不自动加字段 |
| Basic | 两组件各一次 QueryEscape 后 SetBasicAuth；Header 构造及真实写出均为一项 | 显式 Basic，生成算法和凭据版本 UNKNOWN | 合成证实特殊字符可有差异，不能解码真实头比较 |
| Cookie | 新建 Token 请求不设置 Cookie，Client Jar=nil，Transport 不复制浏览器请求头 | 响应 Cookies(2)；请求 Cookie 是否存在 UNKNOWN | 响应有 Cookie 不等于请求携带，也不证明认证依赖 Cookie |
| 代理认证 | 默认自定义 Transport Proxy=nil；无 Proxy-Authorization 设置；实际无 Proxy challenge | 系统/自定义代理及代理认证 UNKNOWN | 透明网关仍未知；不向业务 Token 请求复制浏览器 Cookie/认证信息 |
| Header 重复/重试 | 实际 Authorization 数量1、写出次数1；无重试/3xx | UNKNOWN | 客户端证据未发现重复 Header 或兑换重试 |
| 返回 | 401、空正文、Basic challenge，远端39.107.195.76，Server产品nginx | 截图200 | 拒绝层仍UNKNOWN；须对照同凭据版本及服务端路径 |

## 标准核查

[RFC 6749 §4.1.1/4.1.2](https://datatracker.ietf.org/doc/html/rfc6749#section-4.1.1)：state 关联授权请求与浏览器回调、用于防护 CSRF。KnowHub 不把浏览器传入的 state 当成任意 Token 参数。

[§4.1.3](https://datatracker.ietf.org/doc/html/rfc6749#section-4.1.3)：授权码兑换定义 grant_type、code、redirect_uri 及客户端认证相关 client_id；没有要求 Token Body 的 state。若 SID 自定义必填，应正式确认适用条件、返回类别及说明，不能因成功请求包含就认定必要。

[§2.3.1](https://datatracker.ietf.org/doc/html/rfc6749#section-2.3.1)：Basic 的组件应先表单编码。Go SetBasicAuth 自身只做 username:password 的 Base64，所以调用方负责组件编码。Header 编码与表单正文 Encode 属于不同输入，没有发现 Header 两次 QueryEscape。

## 本轮合成验证

纯合成请求复制业务构造模式，进入 loopback Mock：POST、四个表单字段、Basic一项、无Cookie、无Proxy-Authorization、无Token正文state、无CookieJar，收到Mock401，只发一笔请求。无真实SID通信。

既有9类Basic合成测试覆盖特殊字符与非ASCII；本次补充对照字母数字、短横线和下划线策略等价，+/%不同。真实凭据未检查，仅用户确认无反斜杠/首尾空白，不能推定真实值完全由无需转义字符组成。

真实现网 Cookie/Proxy-Authorization 的最终写出数量没有独立诊断字段，因此“无Cookie/代理认证”是代码和合成验证事实，不冒充最终报文抓包；未知中间层行为不排除。

## SID 需要确认与后续门禁

1. Postman实际使用的认证助手/显式Header、是否仅一个Authorization、当前凭据版本、组件编码策略；只提供类型/布尔/版本，不提供值。
2. 请求Cookie及代理认证是否存在；有无系统/自定义代理及实际出口、入口节点差异。
3. Token state是否必填；若不是，成功示例中的额外state不应指导改代码。
4. 若同一code在不同redirect_uri下仍能兑换成功，请核查code与原授权redirect_uri绑定、客户端绑定、注册URI匹配和精确字符串校验。两个截图不是同事务证据，不据此直接断言SID漏洞，也不利用差异绕过校验。

等待上述非秘密配置确认。确认某单一差异后再提出只调整一个变量的受控方案，使用新的未消费code；本轮不加state、不改Basic/nonce/redirect_uri、不启动真实验证。

证据：[合成Header验证](evidence/token-header-synthetic-20261014.json)、[真实脱敏诊断](evidence/business-real-token-diagnostics-20261014-2000.json)。
