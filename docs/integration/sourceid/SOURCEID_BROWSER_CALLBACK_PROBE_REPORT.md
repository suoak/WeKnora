# 已审批 callback 的专用浏览器复用实测

2026-10-13 14:18（Asia/Shanghai）。**浏览器 → 本机 SSH 转发 → Linux 独立 Probe：PASS。真实 SID OIDC 闭环：NOT_TESTED / BLOCKED_EXTERNAL（缺正式 Metadata）。** 不申请独立 SID 环境、不以新增 callback 为前提；不修改正式 DNS、全局 hosts、网关、生产数据库或现有 Linux 80/8080 服务。

## 实际结果

| 验证项 | 已实际验证的结果 |
|---|---|
| Windows 本机入口 | 127.0.0.1:80 实际绑定成功，仅供本机；SSH direct-tcpip 将原始 HTTP 字节转发到测试机 127.0.0.1:18183，不引入业务代理/handler |
| 浏览器解析 | 专用 Chrome 154.0.8037.98、新非持久化 context，使用 `--host-resolver-rules=MAP knowhub.ruijie.com.cn 127.0.0.1, EXCLUDE localhost` 和 `--no-proxy-server`。DevTools 仅提取连接 IP/端口、scheme/status，未导出 Cookie/HAR/请求正文 |
| 已审批 URI | `http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback` 保持逐字一致；实际 Host 及浏览器 URL 使用该域名，没有 localhost callback 替换或 URI 重写 |
| 实际 TCP 路径 | callback 响应的浏览器 remoteIPAddress 全部 127.0.0.1、remotePort=80；SSH 目标全部 172.30.232.42:22 → 127.0.0.1:18183。没有连接 10.51.134.114；系统 DNS 仍解析到该生产地址，hosts 文件前后摘要相同 |
| synthetic 启动 | /probe/start 返回 302，state/nonce/S256 存在。此次使用明确的临时 synthetic Metadata，浏览器拦截 Mock 授权目标，不向真实 SID 或示例站点发授权/Token 请求，不视为正式配置 |
| synthetic callback | 授权事务启动后，错误 synthetic state/code 请求在已审批路径收到独立 Probe 的 400，响应 validation.state=false；未配置的 /probe/callback 返回 404 |
| HTTPS→HTTP | 已正常读取正式 SID 公开登录页（200），无账号输入；HSTS 存在但不含 includeSubDomains。之后 HTTP callback 仍落本机。另用浏览器内 Mock 302 实测 HTTPS-origin → HTTP callback，收到本机 Probe 的 400；这不是一次真实 SID authorize |
| 代理/企业策略 | 当前 Chrome/Edge HKLM 策略键存在，但选定的 Proxy/HTTPS-only/URLBlocklist/强制扩展项未发现设置；HKCU 对应策略键不存在。当前浏览器实际未走代理、未升级 callback 到 HTTPS。没有关闭证书验证、修改策略、禁用 HSTS 或使用 ignore-certificate-errors |
| 敏感日志门禁 | 在 query、Cookie/Authorization header 及 POST body 放 synthetic code、Secret、Token、个人值；GET 400、POST 405，响应无这些值。浏览器 stdout/stderr 和 Probe stdout/stderr 全部不存在这些值，stderr 为空；SSH 转发不解析或记录应用字节，SDK logging 全部禁用。正式网关、业务 Nginx、业务 logger/APM 没有进入 callback 路径 |
| 完成后清理 | 专用浏览器关闭，Windows 80 监听及 SSH 连接关闭，Linux 18183 关闭，临时 Metadata 删除。Probe exit1 是未执行 Token 交换的 synthetic 事务终止，不代表真实登录成功 |
| 原服务保护 | 现有 80/8080 容器及独立业务测试实例 ID 未变；四个业务 SID 开关仍 false。未调用业务登录、绑定、JWT 生成或数据库写入 |

以上实测覆盖当前机器、浏览器版本、策略及 SID 公开登录页。真实授权流程尚未执行，不能保证其未提供的中间跳转行为；若后续出现父域 HSTS、HTTPS 强制升级或其他策略冲突，停止测试，不关闭校验来继续。

## 最小代码变更与云端验证

- 新验证 SHA：`a4fdb8052df13ab2f1286b6b22b91a497337ad77`；[CI 38353155370](https://github.com/suoak/WeKnora/actions/runs/38353155370) 的 Linux、Postgres、frontend 全部 success。
- 仅修改 `internal/application/service/oidc_probe.go` 及其测试。callback path 从已确认 redirect_uri 派生；继续允许 /probe/callback，额外允许上述**完整、精确的已审批 URI**。其他业务路径 URI、端口/协议/尾斜杠/query 变体仍拒绝。没有安装任何业务路由，也没有调用 LoginWithOIDC、loginSourceID、GenerateTokens。
- 新增 approved_callback 用例验证授权及 Token 请求中的 redirect_uri 不变，合法 Mock state 下 Token/JWKS/ID Token/nonce/UserInfo 完整通过，其他路径不访问提供方；原失败、重放、脱敏测试保留。
- 下载并核对 CI head.txt、JSONL：专项 67 个顶层测试 / 124 个通过事件，回归 3146 个顶层测试 / 4649 个通过事件，fail=0。
- Probe artifact `11791539516`，ZIP SHA256 `33badda4c7b954e527b038bf8ee080f5461097a06458c1efb0af376e73058a43`；二进制 SHA256 `1c1575b748ecc570db16c7b55c3a46ddc32bc78ba2f58f225e5e8a5fc2c38314`，Windows/远端一致，内嵌 vcs.revision 匹配新验证 SHA。
- 新二进制部署目录 `/home/aqyfzx/ai/sourceid-protocol-probe-a4fdb8052df1`；保留 c855 二进制，复用已验证的私有运行库和公共字典，不升级主机系统库。正式启动包装 run-probe.sh 已准备，未输入真实 Secret。

字段级结果、摘要和关闭证据见 [脱敏 JSON](evidence/browser-callback-probe-20261013.json)。[浏览器 synthetic 驱动](linux-probe/browser-chain-driver.mjs) 不输入员工凭据、不记录 HAR；SSH 认证帮助程序留在受保护的本机环境，不提交凭据或凭据存储文件。

## 自行查找 Metadata 的实际结果

企业飞书正文、全部工作表及补充资料的完整调查见 [契约核对](SOURCEID_FEISHU_OIDC_CONTRACT_REVIEW.md)，其中端点/Issuer 为示例，不能填入正式配置。

本轮按标准发现规则，只请求一次正式 SID origin 的 `https://sid.ruijie.com.cn/.well-known/openid-configuration`：**HTTP500、text/html、没有 OIDC Metadata**；没有尝试 /cas/oidc 或其他前缀。此请求不表示已确认 origin 就是 Issuer，也不证明服务没有 OIDC。登录页未发现明确链接的 OIDC/Metadata/config 资源。没有读应用后台、服务器私有配置、Secret、Cookie 或未授权资料。

仍未获得可信正式的：Issuer、Authorization、Token、JWKS、UserInfo、Token Client Authentication Method，以及实际 Token/Profile 编码。正式 scope、state/nonce 回传、S256 的支持也没有现场证据；不依据示例设置为已验证。

**真正需要外部人员提供的只有正式协议配置与兼容信息**：请 SID 平台技术负责人/运维或 knowhub 应用配置负责人给出准确 Discovery URL，或直接导出上述非秘密参数，说明 Token/Profile 编码及支持的 scope/state/nonce/S256。目前不需要他们提供独立 SID 测试环境或新增 callback。

## 首次真实登录的最后操作顺序

1. 取得正式非秘密 Metadata，核对 HTTPS、Issuer/JWKS、客户端认证及编码，按实际 S256 能力配置；保持签名、audience、state、nonce、subject 验证。如确需 transport 适配，仅处理正式契约差异并补专项/CI。
2. **暂停等待用户确认真实登录**。确认后在 Linux 本人终端隐藏输入 Secret，用 builtin printf 经 stdin pipe 传给新目录 run-probe.sh；不从聊天、env、历史文件取值。包装使用干净环境并禁用 core dump，正式 metadata.json 不含 Secret。
3. Windows 本人终端建立 `ssh -N -L 127.0.0.1:80:127.0.0.1:18183 -o ExitOnForwardFailure=yes aqyfzx@172.30.232.42`。使用全新临时 user-data-dir 和 incognito 专用 Chrome，带上述解析与 no-proxy 参数；不复用日常浏览器或保存登录资料。
4. 在专用浏览器打开 `http://knowhub.ruijie.com.cn/probe/start`，由用户在正式 SID 页面手动认证。真实 callback 仍使用已审批 URI；Probe 即时交换 code、验签、获取 UserInfo，只保留端点、alg、字段名/类型和校验结果。不要人工复制 code 或导出 HAR。
5. 成功或失败都关闭 Probe、SSH 隧道及专用浏览器，确认端口释放，撤销/清理本次临时浏览器目录。任何验证失败都安全停止，不签发 KnowHub JWT，不绑定、开户、refresh 或登出 SID。

当前停在第 1 步，真实 ID Token/UserInfo、工号与 SessionIndex 候选仍 **NOT_TESTED**。这次实测已经解决回调入口问题；不会因为 CI 与 synthetic 路径通过就开始真实员工登录。
