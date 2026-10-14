# KnowHub / Postman 401 客户端排查更新

2026-10-14。只使用源码、已有脱敏真实记录及合成凭据；未读取真实 SID Secret、未复用 Token/code、未发起真实登录、未修改运行中的认证配置。

## 可确认结果

1. scope 为 openid；端点、POST、表单四字段、Basic、无 PKCE 均与管理员最新成功 curl 结构一致。缺少正文 client_secret/code_verifier 已不应作为缺陷。
2. 实际日志 state PASS、Header 构造及写出各一次、无重试/重定向；多数 callback 到发送不足 350 ms 仍 401，因此不能将全部问题归于处理超时或客户端重复兑换。
3. 完整 Basic 被白名单日志抑制，Header 格式通过不能证明服务端接受其凭据值或版本。
4. Windows 有系统代理，KnowHub Transport 直连；当前两个直连 DNS/TLS 入口相同。成功 Postman 的实际路径及凭据版本仍 UNKNOWN。

## 发现的具体配置问题

审计原 Secret 写入工具 sourceid-env-install-remote.py：写入单引号 Compose env_file 前，将反斜杠替换为双反斜杠。以同样代码生成纯合成 env_file，再用打包机已安装的 Docker Compose 加载到无网络、独立、一次性 Python 容器，输出仅相等性布尔结果：

| 合成类别 | 原值是否保留 |
| --- | --- |
| 普通字母数字 | true |
| + / = : % 和中间空格 | true |
| 美元符号及 # | true |
| 单引号 | true |
| 反斜杠 | **false** |

这是已复现的写入工具特殊字符缺陷；**不是当前真实凭据已被破坏的证据**。本轮未读取 SID Secret，无法判断它是否含该字符。现有业务容器和私有配置没有改动；只运行独立无网络合成检查容器并自动删除，临时文件亦清理。

另见 internal/config/config.go 的环境覆盖：对 OIDC_AUTH_CLIENT_SECRET 使用 strings.TrimSpace。这会改变合法凭据的首尾空白；现有写入工具会拒绝首尾空白输入，因此正常经此流程导入的凭据不会触发，但其他配置来源仍有兼容风险。不能在缺少实际字符类别证据时认定这一项是当前根因。

## Basic 编码差异

exchangeOIDCCode 对 Client ID 和 Secret 分别执行一次 QueryEscape，再 SetBasicAuth Base64。9 类合成测试证明：普通安全字符与直接 Basic 相同；+、/、=、%、冒号、空格、非 ASCII 等则可能不同。没有发现代码执行两次 Header 组件编码。

RFC OAuth 组件编码与普通 HTTP Basic 的 UTF-8 原值编码是不同候选策略。管理员提供显式 Basic Header，不包含生成算法；不能读取或解码其真实 Header 来证明哪种策略相同。请由 SID 用合成凭据确认其解析器是否在 Base64 解码后对两个组件做表单解码。

## 成功 Postman 的其他差异

- 正文额外 state，标准 Token 请求不要求，SID 自定义要求未知。
- 手工 authorize 未含 nonce，返回 nonce 为空；KnowHub 含随机 nonce，后端严格校验。需要 SID 确认带 nonce 的授权码流程，不应关闭校验。
- callback1 与 callback 显示不一致且截图 code 不同，不足以证明同一成功事务的 redirect_uri 不一致。
- 响应 Cookies 数量不证明请求依赖 Cookie；只需 SID 确认请求是否携带 Cookie 的非秘密布尔状态。
- 最新样例头部标记 RS256，但未验签，不能作为业务验签成功证据。Token 401 发生在此阶段之前。

## 根因优先级及下一步

优先核对有效凭据版本/加载保真及 Basic 组件解析，再核对 Postman/KnowHub 的入口、出口及网关策略。上述方向符合 401 空正文 Basic challenge，但客户端仍不能区分拒绝层。

建议按顺序：

1. 用户与 SID 在受控渠道确认同一当前凭据版本。由用户仅确认凭据是否包含反斜杠或首尾空白，不发送值、长度、哈希或 Basic Header。
2. 如确实包含反斜杠，先修复写入工具并用合成矩阵验证，再由人工安全重新注入；不先试真实登录。
3. 如无此类字符，由 SID 对比合成 Basic 解码策略及服务端客户端认证结果。对普通不需转义凭据，切换编码不会产生差异，不应盲测。
4. 不要同时添加 Token state、改 redirect_uri、关闭 nonce 或切换 Basic。确认服务端契约后才能设计单变量真实验证。

本轮最重要新增发现是**合成反斜杠 env_file 不保真**。根因仍未确认，尚未更改现有应用、Secret 或请求策略。

证据：[client-side-synthetic-env-roundtrip-20261014.json](evidence/client-side-synthetic-env-roundtrip-20261014.json)、[既有三阶段真实诊断](evidence/business-real-token-diagnostics-20261014-2000.json)。
