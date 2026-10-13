# 单次 SID OIDC Protocol Probe

2026-10-13。验证提交：c855338b9530d2a7c6d14ce8485b4474cde0d1e8。云端验证：[38327545586](https://github.com/suoak/WeKnora/actions/runs/38327545586) 全部通过。真实 SID：NOT_TESTED / BLOCKED_EXTERNAL；未启动真实 probe 入口，业务 SID 保持关闭。

## 实现范围

独立命令 cmd/sourceid-probe，监听默认 127.0.0.1:18183。仅提供 GET /probe/start 与 GET /probe/callback，拒绝原业务 callback 地址；不复用 KnowHub handler/router/container，不需要数据库或 Redis。

authorize 只创建一笔内存事务；使用随机 state/nonce，按正式确认配置发送 PKCE S256。callback 一次消费 state，立即换取 Token，复用现有 ID Token RSA/JWKS 验证和 UserInfo 请求能力。保留 issuer/audience/azp/exp/iat/sub 校验以及基线两分钟时钟宽限，并额外强制 nonce、UserInfo.sub 一致。签名算法支持范围沿用 RS256/RS384/RS512；其他算法安全失败，不能关闭验证。

未初始化 userRepo/tokenRepo/tenantService；不调用 LoginWithOIDC、loginSourceID、GenerateTokens 或身份绑定。没有 User/tenant member 写入、业务会话或 KnowHub JWT。令牌 decoder 无 Refresh Token 字段，提供方即使返回也忽略，不请求 offline_access，不进行 refresh 或 logout。

正常业务认证仅增加可选 HTTP client 注入位置，默认仍用原 client；probe 使用禁止重定向的专用 client，阻断服务端 Token/JWKS/UserInfo 重定向和 TLS 降级。没有修改现有认证判定逻辑。

## Metadata 与安全配置

使用 [公开模板](sourceid-probe.metadata.example.json)。必须由 SID 提供正式 Issuer、Authorization、Token、JWKS、UserInfo，以及 client auth、UserInfo 请求方式、scope、nonce/PKCE 支持；人工核对后才设 metadata_confirmed=true。模板不是已验证配置，禁止根据 /cas/oidc/* 示例填写。

程序先验证公开 Metadata，再读取 Secret；未确认、缺字段、非 HTTPS 提供方端点、端点含 userinfo/query/fragment、未知 auth method、offline_access 或业务 callback 均拒绝。Client Secret 不支持 JSON/env/CLI 参数/文件来源，只从 stdin pipe 接收一行，限制长度。监听仅允许 loopback，禁止直接开放公网。

独立 redirect_uri 必须为 SID 批准的 http(s)://<测试入口>/probe/callback，不能直接用目前批准的 /api/v1/auth/oidc/callback。本轮未申请新登记或修改转发；临时入口必须另行确认与审批。

## Linux 运行步骤（正式 Metadata 到位后，由用户操作）

1. 获取 CI 成功产物 sourceid-probe-linux-<验证SHA>，核对来源与摘要；确认目标运行环境兼容。当前尚未部署该二进制到测试机，不以云端构建代替 Linux 现场执行验证。
2. 将 SID 正式非秘密 Metadata 填入受控 metadata.json，不放 Secret；核对独立 callback 的审批及路由。
3. 在本人控制的 bash 终端中执行以下命令。read 的 -s 禁止回显；builtin printf 通过管道传值，Secret 不进入外部进程 argv、env 或文件。不要开启终端录制、set -x、core dump、抓包或 APM。

```bash
set +x
ulimit -c 0
read -r -s -p 'OIDC Client Secret: ' probe_secret
printf '\n'
{
  builtin printf '%s\n' "$probe_secret"
  unset probe_secret
} | ./sourceid-probe -metadata ./metadata.json -listen 127.0.0.1:18183
unset probe_secret
```

命令中的 Secret 只由用户交互输入；本轮未执行此命令，未读取聊天提供的 SID Secret。程序还会强制禁用 Linux core dump，拒绝直接从回显的 TTY 读取。主机 root/同账户调试器仍可能读取进程内存；不承诺 Go/OS 对所有字符串副本物理擦除。

4. 在临时无痕浏览器窗口中，通过已审批的短期受限转发访问 /probe/start；浏览器跳转正式 SID，由用户本人输入企业账号密码，不保存密码。仅一次普通员工，不读取或导出浏览器密码、Cookie/HAR，不人工复制 code。
5. callback 响应及进程 stdout 只包含 endpoint、alg、claims 名称/类型、userinfo_schema、validation。Token/Secret/code、nonce/state 值、个人资料值和原始提供方错误正文不会输出。字段名/深度/数量受限；数组仅标类型，不保存元素值。
6. 成功或已匹配事务的失败都自动关闭 probe；未知 state 不会换取 Token。十分钟超时、SIGINT/SIGTERM 也会关闭监听。停止时清除持有的事务/Secret 引用；HTTP server 与 logger 的原始错误输出丢弃，不设置 Cookie、不写报告文件。
7. 操作员只保留允许的脱敏 JSON，关闭临时无痕窗口并撤销临时转发规则；不调用 SID logout。业务 SID/统一登出保持关闭；不能把 probe 成功等同业务账号登录或 READY_FOR_RELEASE。

退出码：0=单次协议验证完成；1=验证未完成/中止；2=公开配置、stdin、Linux 安全条件或监听条件不满足。任何失败不得通过开放公网入口、关闭 TLS/JWKS/nonce 验证重试。

## 自动化验收

Mock 覆盖：授权跳转及 PKCE、post/basic 兑换、签名/issuer/audience/expiry/nonce 拒绝、UserInfo subject/HTTP 失败、提供方错误、Token/JWKS/UserInfo 重定向拒绝、错误与重复 state、超时及关闭后重放。成功/错误输出均检查 synthetic Secret/个人值不出现，且无 Set-Cookie。

Linux 进程测试使用 Mock TLS SID 与测试公有 CA，真实执行 stdin→监听→callback→脱敏 JSON→退出，检查端口关闭及只留下公开 Metadata/CA 测试文件。测试没有真实企业凭据；未连接真实 SID。

云端专项包含 -race、原认证/MCP 回归、PG17、frontend 验证、go vet 与 probe 构建，均 PASS。专项 67 个顶层测试/123 个含子用例事件通过；回归 3146 个顶层测试/4648 个含子用例事件通过；失败0。Linux 进程闭环和上述 Probe 专项均已在 focused.jsonl 中核对，不把通过事件重复计算为独立测试。

测试证据工件 11782301153 的 ZIP SHA256 为 ba6a9937533d4dfb8abd79534ab9e406722a51464ebd54ba9537d29b1a8a42c3，已下载核对摘要、head.txt 和 JSONL 测试结果。Probe Linux 工件 11782345941，名称 sourceid-probe-linux-c855338b9530d2a7c6d14ce8485b4474cde0d1e8，GitHub ZIP digest 为 df268fdb38cb5e2d35116b01c86d7dc4bd3a342ab89f921d9a9da130a3ea43a7；该二进制尚未导入测试机或启动真实入口。

Windows 本地尝试未通过：Go1.26.8 的依赖链接被现有 MinGW 缺失 rstrtmgr 库阻断；未更改全局工具链或降低校验，依用户既有授权使用 Linux 云端验证。首次云端 38326548190 因测试文件未使用 import 编译失败，已修正；其结果不能作为最终通过证据。
