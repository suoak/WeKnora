# SID Token 请求诊断增强与验收

日期：2026-10-14。最终实现提交：`3be5d3f9268fe8819d66485a2b990c6237827e25`（诊断主体含 dffc68832、b057e23ce、4eb9001ac）。开发验证后，用户追加明确部署授权，现已在打包机 172.30.232.42 部署并通过合成验收。**未读取真实 SID 凭据、未发起新的真实 SID 登录或 Token 请求、未操作生产服务器**。部署细节见 SOURCEID_TOKEN_DIAGNOSTICS_DEPLOYMENT_REPORT.md。

## 1. 业务登录时间线与已有证据

- SID 对接人反馈：2026-10-13 将 knowhub 从 OAuth2 切换为 OIDC；切换后 Postman 使用 Basic、URL 编码表单成功兑换 Token。具体生效时间、配置版本、节点、凭据版本及成功请求时间仍需服务端确认。
- 历史业务诊断包保留 20 条 Token HTTP 401 记录，时间范围为北京时间 2026-10-14 16:38:59.549717 至 17:09:00.746727。这不等于仅执行过 20 次登录，也不补造此前 15:40 的响应头。
- 选定事务：`d69e9f7f-e165-4b56-b67b-ed2875cf2677`，发送事件 UTC `2026-10-14T09:09:00.746727197Z`，北京时间 `2026-10-14T17:09:00.746727197+08:00`。
- 后端实际访问 `POST https://sid.ruijie.com.cn/oidc/accessToken`，HTTP 401、212 ms、正文 0 字节并到达 EOF，Content-Type 未提供，WWW-Authenticate 为 Basic，realm 只保留存在性。
- 这是服务端到 SID 的 HTTP 请求；浏览器 F12 中 `/api/v1/agents`、`/api/v1/auth/me` 等 KnowHub API 的 401 属于另一链路。

原始历史包 SHA-256：`83654de0c100fd421f564162a3358194d9e029b5acb82cd2f424821633d1b5de`。本轮不更改历史包。

## 2. Callback/state 与安全门禁

历史选定事务的 callback_state 为 PASS（1 ms），不代表授权码被 SID 判定有效或未消费。callback 到 dispatch 为 5206 ms，到 Go WroteRequest 为 5320 ms；不能将 212 ms 的 HTTP 耗时当成 callback 到请求发送时间。

新增上下文标志仅在真实 Handler 的 state 及持久事务检查通过后设置。Token 发送时 nonce 尚无法校验，明确记录 `NOT_YET_EXECUTED`；nonce、签名、Issuer、Audience、有效期及 sub 继续在原认证逻辑中严格验证。保留协议验证模式和身份契约门禁，不创建账号、不绕过审核、不改变 JWT 签发规则。

## 3. Token 请求构造前状态

审计的 `exchangeOIDCCode` 保持原逻辑：

1. 表单使用 `url.Values` 设置 grant_type、code、redirect_uri、client_id，再执行一次 `Encode()`。
2. Basic 模式对 Client ID 与 Secret 各执行一次 `url.QueryEscape`，随后 `SetBasicAuth` 完成 `username:password` 的 Base64 编码。SetBasicAuth 自身不执行 OAuth 表单编码。
3. Basic 模式不将 client_secret 放入表单；PKCE 关闭且无 verifier 上下文时不发送 code_verifier。
4. POST，Content-Type 为 application/x-www-form-urlencoded，Accept 为 application/json。一次 `client.Do`，未新增自动重试或参数切换。

新增观察器只读检查 GetBody 的副本，不消费或替换发送正文；不解码或输出真实 Basic 凭据。`client_secret_loaded=true` 只说明配置字符串非空，**不证明密钥正确、有效或与服务端版本一致**。

## 4. 最终 HTTP 请求脱敏结构

沿用 `SOURCEID_DIAGNOSTICS_ENABLE`，新增两条阶段事件，扩展原有最终事件：

| 事件 | 记录时机 | 内容 |
| --- | --- | --- |
| sourceid_token_request | 进入原 Transport 发送前 | 原有白名单请求诊断，加 transport 构造状态 |
| sourceid_token_send | client.Do 返回后 | 本次连接、写出、首字节及响应头观察快照 |
| sourceid_token_http | 原有响应读取/关闭后 | 原有状态和响应分类，加完整 transport 时间线及 outcome |

请求字段包括：服务端生成 Trace ID、UTC/北京时间、Scheme/Host/Path、配置中的 Client ID 和 redirect_uri、POST、Content-Type、Accept 分类、User-Agent 存在性/安全分类、Basic 类型/格式、构造策略、SetBasicAuth 使用状态、Secret 加载布尔值、Authorization 值数量/重复状态、表单白名单字段名及各字段值数量、重复字段标志、grant_type/code 单值非空和配置一致性检查、PKCE 关闭状态、state 结果、callback 到 dispatch/写出耗时。

未知表单字段仅计数，不保留名称或值。Authorization 写出回调只累计值数量；默认 User-Agent 只识别 `Go-http-client/1.1`，其他值归类为 other。Token Endpoint 不输出查询值。

## 5. 实际网络连接与发送时间线

`httptrace.WithClientTrace` 合并原有观察回调；不使用 DumpRequestOut，不修改请求、HTTP Client 超时、TLS 校验、Basic 编码或原重定向策略。

实际观察：DNS start/done（最多 8 个有效 IP）、TCP start/done、TLS start/done、获得连接、请求头写出、请求写出完成、首字节、交换处理结束，各事件均有 UTC/北京时间；DNS/TCP/TLS 配对事件附耗时。记录实际远端 IP:端口、连接复用、TLS1.2/1.3、ALPN 和响应 HTTP 版本。

边界必须保留：

- 连接复用、自定义解析器等情况下可能没有 DNS/TLS 事件；记录 NOT_CAPTURED 或事件缺失，不能补造握手。
- `proxy_policy=no_proxy_in_configured_transport` 是对当前自定义 Transport 的代码结论，**不是证明不存在透明网关**。远端地址也不代表 SID 看到的出口 NAT 地址。
- 写出次数来自 WroteRequest 回调；它与最终 HTTP 记录数共同帮助发现重发，但不能证明 SID 实际消费次数。客户端不新增重试。
- `redirect_response` 仅表示观察到 3xx；原 Token 客户端不跟随重定向，不能冒充完整多跳计数。
- 日志时间是进程观察时间，不是网络抓包或 SID 接收时间。

## 6. 完整脱敏响应元数据

保留 HTTP 状态、耗时、Content-Type 安全分类、声明长度、实际读取字节数、EOF、读取错误、诊断前缀截断及响应结构。新增 Date 的规范 UTC 时间、Server 产品分类、Via 存在性、Proxy-Authenticate 存在性、HTTP 版本、WWW realm 存在分类。

Server 仅允许 nginx/openresty/envoy/apache/microsoft-iis/other/absent；不输出版本或任意原文。Via、realm 不保存值。关联 Header 沿用既有白名单格式校验和安全指纹机制，仅用于服务端请求关联，不生成任何 Secret、code、Token 或身份数据指纹。

标准 OAuth error 只接受既有固定枚举。自定义错误码沿用既有字符、长度和敏感值过滤；error_description 只记录存在性，不输出原文。HTTP 200 仅记录已审查 Token 字段的存在性及类型；不将“字段存在”标为验签成功。

`empty_401_basic_challenge` 专门识别 HTTP 401 + 空正文 + Basic challenge。JSON OAuth 错误、HTML 响应、网络/TLS 错误和非法 Token 响应分别分类。拒绝组件统一为 `UNKNOWN`，不会仅凭 Basic challenge 或 Server 产品分类认定网关/Token 服务拒绝。

保持原响应读取行为：业务错误处理沿用 2048 字节丢弃上限，既有诊断 Transport 会预读最多 4 KiB 并恢复给调用方；成功解析沿用 1 MiB 限制。诊断只在内存保存最多 4 KiB 前缀用于白名单结构提取，**不落盘原文，本次未增加 drain 行为**。只有 EOF 等证据满足时才认为读取完整；其余情况显式保留读取/截断状态。

源码反向代理核查：`frontend/nginx-http.conf` 的 api_no_query 只记录方法和 `$uri` 等访问元数据，不记录 args、Referer、Cookie、Authorization；`nginx.conf` 的 `/api/v1/auth/oidc/` 专属路由已设置 `proxy_next_upstream off`，避免 callback 上游重放。后端 Token HTTPS 请求不经此前端 Nginx。后续授权部署已通过实际 callback 路由和 app/frontend 日志隐私合成检查，详见部署报告。

## 7. Go/Postman 合成差异验证

9 组纯合成凭据覆盖字母数字、+、/、=、%、冒号、空格、组合和非 ASCII 字符。比较 OAuth 组件表单编码后 Basic、直接 SetBasicAuth，以及普通 Basic 的候选算法；同时验证单次解码还原和重复编码差异。未访问真实凭据。

- 普通字母数字时，两种策略可能得到相同结果；包含特殊字符时可能不同。
- 当前 Go 实现为每组件一次 OAuth 编码，再 Base64；表单正文 Encode 不会再次编码 Header 中的凭据。
- Postman 实际成功请求的组件编码方式 **UNKNOWN**，不能从 `Authorization: Basic [REDACTED]` 推断。
- SID 明确确认 Basic、无 PKCE、nonce 回传；飞书本轮读取 revision 20 正文及 12 个可读取嵌入表格，未找到 Basic 组件编码规范。示例表单 client_secret 不能推翻后续正式确认。

参考：[企业飞书 OIDC 文档](https://ruijie.feishu.cn/wiki/OMhSwh3G1iApPBklVW6cLzC2nVb)、[RFC 6749 §2.3.1](https://www.rfc-editor.org/rfc/rfc6749#section-2.3.1)、Go 标准库 `net/http/request.go` 的 SetBasicAuth 实现。文档授权读取使用 Lark CLI，只读；未将正文中的示例凭据复制入报告。

## 8. SID 侧必须确认

以 2026-10-14 17:09:00.746727 北京时间为主，关联 15:40 附近及协议切换前后记录：

1. 10 月 13 日 OIDC 配置修改的精确生效时间、各 Token/网关节点的注册配置版本和缓存刷新情况。
2. knowhub 允许的客户端认证方式、当前凭据版本是否与人工更新版本匹配（无需向 Agent 提供值）。
3. Postman 切换后成功请求的时间、节点、来源 IP、非秘密配置，以及 Basic 组件编码规则。
4. 该 401 到达哪个组件；Basic 认证结果、授权码校验/消费结果、Host/IP 策略命中结果。

Discovery 公开可访问不证明特定 knowhub 客户端注册配置已生效。

## 9. 根因假设与证据等级

| 假设 | 当前证据与限制 |
| --- | --- |
| Basic 客户端认证/凭据版本/注册配置拒绝 | 401 空正文 Basic challenge 提供方向性支持；无法区分错误密钥、客户端注册、节点缓存或解析策略 |
| OAuth2→OIDC 注册切换或配置不一致 | 有明确配置变更及切换后 Postman 成功反馈；没有服务端配置版本/节点证据，不认定根因 |
| 前置网关/来源策略 | 不能排除；旧记录无 Server/Via/远端/TLS，Basic challenge 不能定位拒绝层 |
| Basic 组件编码兼容差异 | 合成测试证明特殊字符存在算法差异；不知道成功 Postman 的实际算法，未证明真实请求因此失败 |
| 授权码失效/redirect_uri/PKCE | 请求结构检查通过且无 PKCE；state 通过不证明 code 有效。对方反馈重复 code 为 500，不能据 401 推定重复消费 |
| KnowHub 构造缺陷 | 尚无可直接确认的认证构造缺陷；未为通过测试擅自修改协议策略 |

## 10. 验收、日志治理与下一步

**验收 PASS**：[GitHub Actions 38556052305](https://github.com/suoak/WeKnora/actions/runs/38556052305) 对最终实现 SHA `3be5d3f9268fe8819d66485a2b990c6237827e25` 的 linux、postgres、frontend 三项均成功，含竞态测试、过滤/导出、合成 Basic、实际日志输出隐私及三阶段断言、完整认证/MCP 回归、迁移及构建。机器记录见同目录 evidence/token-http-diagnostics-verification-20261014.json。本地 Go 1.24.5 仅执行隔离的标准库观察器测试；完整项目使用既有云端 Go 1.26.8 验证，打包机构建沿用 Go 1.26.7，未更改既有基线。

新增测试覆盖：开启/关闭诊断的真实 exchangeOIDCCode Mock 集成（请求语义及一次兑换保持一致）；401 空正文 Basic、OAuth 错误及敏感描述、HTML 网关页、200 合成 Token/非法响应；特殊字符 Basic；已有 httptrace 回调保留；并发及事件限额；重试/重定向/TLS 错误观察；导出器非法字段、嵌套日志格式、Trace 一致性和泄露负例。另运行原认证、Handler、迁移、MCP、前端回调及构建检查。

诊断默认关闭，只在既有 SourceID + Diagnostics 开关及服务端上下文同时满足时生效。日志单条最多 32 KiB，每进程最多 10 MiB，首次诊断后最多 30 分钟；触及限额仅停止输出并记录一次固定限额类别，不改变认证。网络事件最多 64 条，多余只计数。

导出工具：`business-test/export_token_diagnostics.py` 从 stdin 流式接受日志，只保留三种白名单事件，最多 256 条/8 MiB，拒绝覆盖历史文件，Linux 创建权限 0600。不保存输入日志、不调用 Docker、不读取 DPAPI/Secret。示例：

```sh
# 将受控日志流经 stdin 输入；不要先转储整份日志到文件。
python3 docs/integration/sourceid/business-test/export_token_diagnostics.py --output /private/path/token-diagnostics-unique.json
```

`business-test/diagnostics-log-limits.yml` 提供仅 app 的 Docker 轮转配置（10m × 3），后续授权部署已将同等配置应用到打包机 app。启用真实诊断前仍需限制日志读取权限、审核 Docker/root 可读范围，并安排最迟 24 小时内清理诊断副本；进程输出窗口不等于存储保留期。不得为重置诊断窗口擅自重启运行中业务。

当前已具备程序级三阶段采集和严格过滤导出能力，已部署并在真实业务容器上通过四项 Mock 浏览器验收。旧证据已经足够 SID 按时间查日志，但不足以单独确定拒绝层或根因。新版本的正式 SID 请求仍为 **NOT_TESTED**；历史 Server/Via/DNS/TLS/远端等为 NOT_CAPTURED，不回填。

下一步请 SID 对照历史请求和切换后 Postman 的服务端结果；部署已完成，真实员工认证仍等待另行授权。首次真实诊断请求保持原协议参数，新增变量仅为诊断版本；不盲改 Basic、PKCE、nonce、state 或 redirect_uri。

已修正回调根路径在消费结果前提前加载 Portal 的问题，浏览器合成验收未请求 PortalHome 资源；未复现真实 Chrome STATUS_BREAKPOINT，不宣称已解决该原生崩溃。
