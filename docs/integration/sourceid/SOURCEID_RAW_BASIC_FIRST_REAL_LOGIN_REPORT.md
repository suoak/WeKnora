# raw Basic 首次真实业务登录报告

2026-10-15；服务器仅为 172.30.232.42，版本 `468410e893de4a34918e9a5201bbc3870dadc22c`。未访问或操作正式生产主机，未重新构建、升级代码或修改数据库结构。

**结论：raw Basic 已在真实 Token 请求中使用，但仍返回 HTTP 401，未变为 200。协议闭环未完成。只执行一笔新授权事务，没有重试。**

## 关键结果

| 阶段 | 实际结果 | 证据与边界 |
| --- | --- | --- |
| 浏览器路由 | PASS | 独立 Chrome 绕过系统代理，KnowHub 连接均为 127.0.0.1:80，经固定指向打包机的 SSH 转发；运行配置 raw/enabled=true |
| authorize | PASS | 一次 KnowHub 登录入口请求；Client ID=knowhub、scope=openid、原 callback、state/nonce 存在，无 PKCE 参数 |
| 员工交互认证 | 用户确认完成 | SID 返回新 code，未保存 code；不等于 KnowHub 已登录 |
| callback/state | PASS | 后端 callback_state 通过；这不证明 SID 已验证 code 的有效性 |
| Token | FAIL | POST https://sid.ruijie.com.cn/oidc/accessToken，HTTP 401，620 ms |
| ID Token、RS256、Issuer/Audience、nonce、有效期 | NOT_TESTED | Token 阶段失败，无 ID Token；未放宽任何校验 |
| UserInfo 与 sub | NOT_TESTED | 未进入该阶段，未取得员工字段 schema |
| 企业身份契约门禁 | 保持配置，未到达 | protocol_only=true、contract_confirmed=false；未创建/绑定用户或签发业务 JWT |

浏览器返回 `/#oidc_error=login_failed` 并回到登录页。原专用窗口由采集脚本在事务完成后主动关闭；不能据此认定浏览器崩溃，也不能宣称历史 STATUS_BREAKPOINT 已修复。

## 提交 SID 管理员的最小定位信息

- 本地 Trace ID：`0cd6678f-e23b-4445-9fc2-758753a3f4c1`。这是 KnowHub 本地关联号，未证明 SID 接收了它。
- Token 分派 UTC：`2026-10-15T02:32:24.861333298Z`，北京时间 `2026-10-15 10:32:24.861333298 +08:00`。
- 请求写完 UTC：`2026-10-15T02:32:24.970714383Z`，北京时间 `2026-10-15 10:32:24.970714383 +08:00`。
- 首字节：UTC `2026-10-15T02:32:25.481653456Z`，北京时间 `10:32:25.481653456`。
- Client ID=knowhub；POST `/oidc/accessToken`；Host=sid.ruijie.com.cn；Content-Type=application/x-www-form-urlencoded；Accept=application/json。
- 认证为 client_secret_basic；策略 `raw_then_set_basic_auth_base64_once`，不预先执行 OAuth 凭据表单转义。一个 Authorization 值，格式检查通过，实际写出也只观察到一个。
- 表单字段仅 client_id、code、grant_type、redirect_uri，各单值；grant_type=authorization_code；code 单值非空；redirect_uri=`http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback`，与配置一致。client_secret、code_verifier 均不在 Body。
- 响应 HTTP 401，Content-Type absent，Content-Length=0，实际读取=0，EOF=true；WWW-Authenticate scheme=basic，realm 存在但不保存原值；无标准 OAuth error 可解析。
- Server 安全分类 nginx，Via absent，Proxy-Authenticate absent；X-Request-ID/X-Correlation-ID/Traceparent 均未返回。拒绝组件 UNKNOWN，不能仅凭 nginx 或 Basic challenge 定位到网关或 SID Token 服务。
- 实际连接 `39.107.195.76:443`，TLS1.3，HTTP/1.1；后端 Transport 未配置代理；一次完整写出，无 Redirect、无 Retry。该远端是 SID 目的地址，不是 SID 看到的客户端出口 NAT 地址；出口来源须由 SID 日志确认。

## 时序与新增线索

- 浏览器观察到 authorize：UTC `2026-10-15T02:31:32.890Z`。
- 浏览器观察到 callback：UTC `2026-10-15T02:32:16.972Z`。
- 后端 callback 到 Token 分派 7503 ms，到请求写完 7612 ms；Token 请求本身 620 ms。
- DNS 约 5 ms、SID TCP 约 48 ms、TLS 约 54 ms。DNS 期间的 127.0.0.11:53 事件是容器解析器活动，不是额外 Token 请求。
- callback 处理至发送约 7.6 秒，值得与 SID 的实际授权码签发时间及有效期一起核查；现有证据不能认定 code 已过期，也不能定位这段耗时的具体内部原因。没有复用旧 code 或第二次兑换证据。
- 采集器的 Playwright callback 路由拦截计数为 0，但 CDP 明确观察到携带 code/state 的 callback，后端也记录 callback_state PASS；该拦截计数不能作为真实 callback 次数。Token 请求次数以 httptrace 的一次写出及唯一 Token 结果为依据。

## 凭据与请求对比结论

受控运行进程从既有 DPAPI 解密，仅经 SSH/stdin 注入服务内存。Codex 未读取或输出明文、Basic/Base64、code、Token、Cookie 或个人资料。Secret 加载非空只说明已加载，不代表 SID 已接受该凭据。

raw 未消除 401，说明仅改变编码模式不足以解决当前失败；真实凭据两种编码是否等价仍 UNKNOWN。对方 Postman 与本机使用的凭据版本是否相同仍须 SID 管理员确认。

下一步优先让 SID 根据上述精确时间检查：

1. 此请求到达哪个节点/网关，Basic 客户端认证是否通过；节点上 knowhub 的 OIDC 注册、允许认证模式和凭据版本是否与成功 Postman 一致。
2. 若 Basic 未通过，核查当前凭据版本、Authorization Header 传递、网关策略与协议切换后缓存/节点一致性。不要把空正文 401 直接判为 Secret 错误。
3. 若 Basic 已通过，核查本次 code 的签发时间、首次兑换/消费记录、有效期、绑定 Client ID 与 redirect_uri，以及返回 401 的具体分支。不要把 state PASS 当作 code 有效证明。
4. 对照成功 Postman 的精确时间、同一应用及凭据版本、来源/网关路径；不交换真实 Basic/code/Token。客户端观察无法回答服务端内部拒绝原因。

不再自动登录、不切换 Post/PKCE/nonce/redirect_uri，也不添加 state 到 Token Body。待 SID 给出拒绝分支后再决定单变量验证。

## 结束清理与用户后续指令

本次单次采集结束后已关闭 OIDC 和专项诊断、专用 Chrome、SSH 隧道，Windows localhost:80 释放。版本和端口保持；PostgreSQL/Redis/DocReader ID 未变。users、审核映射、external_identities、source_id_sessions、auth_tokens 计数前后相同，无企业用户写入、绑定或本地 JWT 签发。正常 OIDC state 事务可能新增/消费，不将整个数据库宣称为字节不变。

随后用户明确要求改为常驻测试环境、普通浏览器手动点击。因此最终状态按更新指令重新开启 OIDC 与受限诊断，不再使用自动关闭浏览器/隧道。没有再次发起授权或 Token 请求。

```text
SOURCEID_BASIC_ENCODING=raw
OIDC_AUTH_ENABLE=true
SOURCEID_DIAGNOSTICS_ENABLE=true
SOURCEID_PROTOCOL_ONLY=true
SOURCEID_CONTRACT_CONFIRMED=false
SOURCEID_LOGOUT_ENABLE=false
```

服务从原 0600 受保护配置加载凭据。受控进程仅返回“当前服务凭据与 DPAPI 相等=true”，没有读取/修改凭据文件或输出值；这不等于 SID 凭据有效性已验证。诊断仍受单进程 30 分钟/10 MiB、事件限长及 Docker 轮转限制，不保证长期无限采集。

常驻入口：`http://knowhub.ruijie.com.cn/login`。本机 hosts 必须指向 172.30.232.42，并在普通浏览器/代理中对该域名及内网地址直连；hosts 不会覆盖代理端的 DNS。开发者工具查看该站请求远端应为 `172.30.232.42:80`。不要仅凭页面能打开就判定落点。当前 Token 401 未解决，不建议重复点击制造相同失败。

配置回退备份：`/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/upgrade-backups/raw-persistent-20261015T023459Z/compose.disabled.yml`，复制回部署目录 compose.yml 后只重建 app/frontend，即可恢复关闭状态；不改数据库和配套服务。

## 脱敏机器证据

- [本次真实事务三阶段诊断](evidence/raw-basic-first-real-login-20261015.json)
- [事务结束关闭核验](evidence/raw-basic-first-real-closeout-20261015.json)
- [用户要求的常驻测试入口状态](evidence/raw-basic-persistent-test-entry-20261015.json)
