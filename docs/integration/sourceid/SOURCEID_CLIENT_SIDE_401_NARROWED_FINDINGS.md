# SID 401 排查收敛结果

2026-10-14。用户确认当前 Secret 无反斜杠、无首尾空白，并确认已更新既有 DPAPI 文件。本轮继续只读/合成分析，无新真实授权或兑换，无运行配置修改。

## 已降级的假设

- 合成反斜杠 env_file 缺陷已复现，但用户确认真实值无此字符，不能将该缺陷作为当前故障根因。
- 远端 Secret 文件写入为北京时间 10 月 14 日 16:34:39；app 于 20:00:06 创建、20:00:08 启动，读取配置晚于文件写入。Secret 文件在 env_file 顺序最后，公开 Compose environment 没有 OIDC_AUTH_CLIENT_SECRET 覆盖项。没有证据支持“文件写入后容器没重建”或空 mode.env 覆盖最终 Secret。
- DPAPI mtime 为 10 月 13 日 16:51:05。用户确认已更新该文件，故不将时间戳当作过期判据。mtime 不能确认 SID 当前注册凭据版本或与成功 Postman 值相同。
- 新合成 Basic 对照：短横线、下划线及字母数字组合的 QueryEscape+SetBasicAuth 与直接 SetBasicAuth 完全相同；加号、百分号不同。用户只确认包含短横线，并未确认只包含 URL-safe 字符，故特殊字符编码仍为条件性假设，不能自动切换算法。
- scope、表单 Secret/code_verifier 缺失、服务器八小时时差、客户端自动 Token 重试/重定向、多数事务 callback 超时均缺乏支持。
- RS256、nonce 及员工契约验证位于 Token 成功之后，不能直接解释目前 Token HTTP 401。

## 当前无法从客户端独立确认

1. 管理员成功 Postman 与服务是否用同一有效凭据版本。当前服务 Secret 非空、Header 存在/结构正确，都不等于版本匹配。
2. 成功 Postman 是否有隐含请求 Cookie、额外 state、自定义代理/认证助手、不同入口或来源策略。响应 Cookies 数量不能证明请求 Cookie 存在。当前 KnowHub 新建 Token 请求没有 Cookie Header，也没有 Cookie Jar。
3. SID 入口 nginx 的 401 来自哪个组件、是否进入 Token 服务、Basic 认证和 code 校验的安全结果。Server nginx 和 Basic challenge 都不能定位拒绝层。

## 现在最有价值的下一步

请 SID 在成功 Postman 上只提供以下非秘密检查结果，而不是再次分享 Token：

- 成功请求时间、入口 IP/节点和客户端凭据版本是否与当前 KnowHub 配置属于同一版本。
- 请求 Header 的名称清单（无值），尤其 Cookie 是否存在；Authorization 是否只有一项；认证助手是否改写显式 Basic。
- 正文是否必须包含 state，或只是客户端额外传入；scope=openid 与 nonce 是否均按相同规则发送。
- 代理使用布尔状态、实际来源 IP及网关策略命中结果。
- Basic 凭据是否先做 OAuth 组件编码，可用纯合成含 +/% 的凭据说明，不能输出真实值或哈希。

优先拿 20:04:14.419494239 失败样本与成功样本比较网关 access 的 upstream_status/upstream_addr/安全请求关联值。Token 应用没有错误日志并不说明网关认证成功，也不证明请求未到达。

若服务端确实无法提供任何拒绝原因，下一次有价值的受控实验是**用户本机 Postman 使用与当前服务同一受控凭据版本、同一请求结构，只改变请求来源**，用独立批准的接码流程取得全新 code 并只消费一次。这样才能区别“管理员的凭据/会话”和“网络来源”；不是拿已被 KnowHub 消费的 code 再试。此实验未启动，仍需明确单次授权及安全接码安排。

本轮没有发现可直接证明当前失败的 exchangeOIDCCode 构造缺陷；因此不盲加 state、不改变 Basic/PKCE/nonce/签名。凭据版本及请求上下文与服务端拒绝位置，是剩余的核心缺口。

证据：[配置元数据和合成 Basic 结果](evidence/client-side-credential-metadata-and-basic-20261014.json)。
