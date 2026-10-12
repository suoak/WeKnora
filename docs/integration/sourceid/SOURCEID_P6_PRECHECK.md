# P6 联调前检查报告（2026-10-12）

结论：P6 文档准备完成；真实联调未执行，`IMPLEMENTED_LOCAL / BLOCKED_EXTERNAL` 保持。`READY_FOR_SID_INTEGRATION=false`、`SID_INTEGRATION_VERIFIED=false`、`READY_FOR_RELEASE=false`。还存在启用前日志门禁，见下表。全部 SID 开关保持关闭。

## 基线与边界

- 分支：`feature/knowhub-sourceid-oidc`。
- 本轮开始 HEAD：`39bfe24ddc4f24971a01b906c4c4656b362ed73a`。
- P6交付HEAD由`git rev-parse HEAD`取得；提交只包含此目录文档/模板，后续代码仍须与下面验证SHA单独比较。
- 运行代码验证 SHA：`8ae91bc4439dcc93be11844ada5ec1479f2730bc`。[最新 Actions](https://github.com/suoak/WeKnora/actions/runs/38242327216) API 返回 completed/success，head_sha 与基线一致。
- `git diff 8ae91bc44 HEAD -- . ':!docs/integration/sourceid/**'` 无差异。HEAD 多出的 5 个文件均为交付文档。本轮仅添加/更正文档与占位配置模板，没有认证代码或代理运行配置改动，不需要重新声称一次代码 CI。
- 开始时已有 8 个 tracked 改动（5 个 locales、McpServerIntegrationSettings.vue、agent-editor 的 final-gate.config.ts/run-gate.mjs）及 QA/MCP/agent-editor 未跟踪文件。本轮保留，不纳入交付。
- 已逐一核对 PROTOCOL_REVIEW、TEST_REPORT、DEPLOYMENT、ROLLBACK。实现验证统计和原始 artifact 的提交号/摘要沿用既有报告。
- 用户指定本机预检查；未提供服务器终端。本轮不访问数据库、不提交映射、不登录/登出任何真实用户、不POST backchannel通知、不部署或合并主分支。除DNS/TCP/TLS外仅对批准路径各执行一次无凭据HEAD，未调用认证业务动作。已有密钥由用户持有，本轮未读取或注入。

## 协议证据完整性

企业飞书 OIDC revision 20 的全文与工作簿读取结果已核对。此前“13 张参数表”的报告有计数错误：实际为 **13 张工作表，其中 12 张是正文引用的参数表，1 张 Sheet1 为空白**；12 个引用全部匹配，没有遗漏正文参数表。

参数表分别为 2sGGnP/EcMQ42（授权请求/返回）、t00Ydv/NxUaqH（Token 请求/返回）、qyPuKg/9WQjQk（Profile 请求/属性）、RLTxMK/ZoGI8P/i3zcH5（demo 授权接口/请求/返回）、X9Qri1/fnzJQm/VgvIkG（demo Token 接口/请求/返回）。未复取或公开原始密钥、JWT、员工信息；本轮基于已授权读取的 revision 20 快照。

[SourceID 应用登出文档](https://sourceid.ruishan.cc/linkid/authentication/introduction/logout.html) 本轮只读复核：POST 通知、logoutRequest/SessionIndex 与 CAS ST、传统 OAuth tgt 有说明；OIDC 的对应来源、消息认证和响应契约仍缺失。表单 body 与公开 URL query 示例存在兼容性待确认项，见 [契约清单](SOURCEID_CONTRACT_CHECKLIST.md)。

## 实测：本机参考网络

时间：2026-10-12 11:41:19 UTC（19:41:19 Asia/Shanghai）。Windows 开发工作站；Python direct socket，不使用 HTTP 代理；每个已知主机 DNS 最多 8 秒、首个解析地址 TCP/TLS 最多 5 秒，无地址扫描、无重试。TLS 使用系统信任与主机名验证，没有关闭证书校验。未发送 HTTP 请求或尝试发现路径。

| 目标 | DNS | TCP | TLS | 解释 |
|---|---|---|---|---|
| sid.ruijie.com.cn:443 | PASS，1 个地址 | PASS | PASS，TLSv1.3；证书到期 2027-02-05 01:53:26 UTC | 只证明本机可建连接，不证明 issuer/Discovery/JWKS 或业务接口有效 |
| knowhub.ruijie.com.cn:80 | PASS，1 个地址 | PASS | 不适用（批准 URI 为 HTTP） | 不证明任何 HTTP 路由、代理、运行版本或通知信任有效 |

只记录数量，不公开 IP。未测试未确认的正式 Token/Profile/JWKS URL。实际 KnowHub 容器出站、SID 通知发送节点入站均为 NOT_RUN；不能用本机或 GitHub runner 结果替代。

补充只读HTTP检查：2026-10-12 11:58:53 UTC（19:58:53+08:00），本机直连批准域名，各一次HEAD，无query/凭据/Cookie/重试/重定向跟随。callback和backchannel路径均返回401、application/json、X-Request-ID存在、无Location。没有记录Cookie/header原值或响应body。只能证明这两条路径有HTTP响应；HEAD不是批准GET callback/POST通知方法，且可能在认证中间件前被拦截，不能证明目标handler、运行SHA、方法转发或来源认证已验证。

## 八项预检查

| 项目 | 当前证据/结果 | 剩余动作与通过条件 |
|---|---|---|
| 1. 服务器到 SID DNS/TLS/网络 | 本机参考 PASS；实际服务器 NOT_RUN | 在真实应用网络命名空间验证正式主机 DNS、443、证书链/主机名和超时；只读 GET 已确认 Discovery/JWKS，不请求 authorize/Token/Profile。记录时间、位置、状态与脱敏请求 ID |
| 2. 正式端点配置 | STATIC_PASS：issuer/discovery/authorization/token/userinfo/jwks 均有配置及 Compose 透传 | 正式值未提供。显式端点可完全配置；SID 仍要求显式 issuer。配置 Discovery 不意味着总会请求：全部必需显式端点齐全时可跳过 Discovery。确认 Token urlencoded POST、Profile 模式、RSA 算法与实际契约兼容 |
| 3. 批准路径 | STATIC_PASS：GET callback 和 POST backchannel-logout 路由匹配批准的完整 URI；SID callback 有固定 URI 校验 | SID 应用注册截图/审批凭据和实际网关域名配置尚未取得。保持 HTTP 原审批值，HTTPS 变更需新审批及代码固定 URI 校验适配 |
| 4. 反向代理 | STATIC_PARTIAL：frontend/nginx.conf 的 /api/ → upstream /api/ 保留两条路径、方法/body、Host；没有专用 SID 认证入口。本机HEAD两路径401，不能证明实际handler | 无部署 nginx 配置/关联upstream证据，不能称“正确转发已验证”。须确认无重定向、无 SPA fallback、无 body 改写，通知在来源认证后经可信内部链路转发，后端直连不可绕过 |
| 5. SID→KnowHub 网络/白名单 | BLOCKED_EXTERNAL：无 SID egress/NAT CIDR、来源认证或通知测试授权 | SID 节点到批准域名80端口（现审批）的解析/路由；网关验证实际来源；应用 SOURCEID_LOGOUT_CIDRS 使用直接 socket peer（通常最后一跳代理），不是 X-Forwarded-For 或 SID egress 地址。两层白名单分别验证 |
| 6. Secret 安全注入 | STATIC_PASS：OIDC_AUTH_CLIENT_SECRET 环境变量/配置；JSON json:"-"，公开 config 不返回 secret；Compose 透传；SQL 日志关闭 | 尚无部署证据。用受限部署 secret manager 注入实际进程环境，禁止 git/命令参数/docker compose config 输出。当前没有 _FILE 读取能力，不能只挂载 secret 文件就声称已注入；部署需要受控注入适配 |
| 7. 安全关闭/故障 | STATIC+MOCK_PASS：无配置 disabled；SID contract/issuer/callback 缺失拒绝；签名/nonce/claims/subject/状态失败拒绝；通知门禁503、信任403、格式400/415、存储503；已有 SID token 不因关闭开关而跳过会话验证 | 本机进程四开关均 UNSET；.env/.env.local/.env.production 均不存在；仓库 config 无 OIDC 段，Compose 四开关默认 false。没有部署实例证据。URL错误/SSRF/TLS/JWKS/数据库异常须在隔离环境实测。已发行且未撤销 SID 会话不因 disabled 自动失效；SID 故障也不会自动撤销已登录会话 |
| 8. 本地应急管理员 | STATIC+CI_PASS：POST /api/v1/auth/login 独立于 OIDC；密码登录/验证/本地退出回归存在 | 未执行真实管理员登录，未核验账号活跃/密码/权限。授权后在隔离环境验证；保留用户可直接访问的本地登录页和受控管理员凭据，不用临时修改管理员标记绕过 |

### 新发现的日志门禁（未修改代码）

`internal/middleware/logger.go` 的 readRequestBody 会采集 form-urlencoded 内容；sanitizeBody 仅针对 JSON 字段，不能保证 logoutRequest XML 脱敏。sanitizeQuery 也未列出 logoutRequest。仓库默认 Nginx /api/ 的 access_log 使用默认格式（含 callback query），现有 api_no_query 格式包含 Referer，单独复用仍不能保证授权码不经 Referer 进入日志。

**启用真实联调前须处理并验收**：后端对 OIDC/SID 敏感路由排除请求/响应 body 采集，并对通知 query 整体排除；网关 callback/通知/result 的访问日志只含路径、方法、状态、耗时与关联 ID，不含 query/Referer/认证头/body/Set-Cookie。网关 error/debug/APM 同样检查。仅降低 log level 或靠事后采集过滤不算通过。若需代码修改，应单独实施这些路由的最小日志排除补丁并跑中间件/认证专项及云端验证，不能沿用本轮基线冒称修复。

## 现场只读检查方法

1. 发布负责人提供隔离测试服务器/容器终端、运行 commit、部署配置模板和入口拓扑。使用测试环境，只查看四开关与 secret 是否存在，不打印 secret/DSN/JWT，不执行 env 全量或 docker inspect/compose config 全量输出。
2. 从应用容器命名空间使用 `getent ahosts sid.ruijie.com.cn`、`openssl s_client -connect sid.ruijie.com.cn:443 -servername sid.ruijie.com.cn -verify_hostname sid.ruijie.com.cn -verify_return_error </dev/null`；限定 shell 超时。私有 CA 经批准安装信任链，不用 insecure 参数。
3. SID 确认正式无凭据 Discovery/JWKS URL 后，用有限超时、无自动重定向、无 retry 的只读 GET 检查；不得从示例拼接路径。端点需要非公开权限时停止，不尝试绕过。
4. 代理部署负责人离线核对匹配与重写、认证、直接 peer、网络隔离和脱敏日志。`nginx -t` 只验证语法，不能证明请求经过来源认证。配置片段可能含秘密，只归档脱敏版本。
5. 在隔离且 SID 关闭的部署，仅经人工许可用 HEAD 检查确切通知路径，或离线 mock upstream 验证 POST 转发。禁止对真实 backchannel POST 构造任何 SessionIndex。“405/401/503”单独不能证明命中目标 handler，必须对应网关/upstream 请求 ID；精确 HTTP 行为留待受控用例。
6. SID 方从发送节点执行不含业务消息的 DNS/TCP检查，并提供脱敏结果；生产通知探测等待单独授权。

## 交付索引与状态

- [正式协议契约清单](SOURCEID_CONTRACT_CHECKLIST.md)
- [正式配置占位模板](SOURCEID_P6_CONFIG.env.example)
- [SID 最终问题清单](SOURCEID_SID_QUESTIONS.md)
- [审核测试账号绑定](SOURCEID_P6_ACCOUNT_MAPPING.md)
- [A–I 真实联调用例](SOURCEID_P6_E2E.md)
- [生产前安全门禁与回退](SOURCEID_P6_RELEASE_GATE.md)

已完成：代码基线锁定、四份原报告核对、协议快照完整性核对、正式契约差异表、本机 DNS/TCP/TLS、源码配置/路由/代理/secret/故障/本地入口检查、审核映射操作与真实用例设计。

未完成：实际服务器与 SID 发送节点网络、已部署代理/secret/管理员验证、受控账号申请/数据库快照/CSV dry-run/apply、A–I 执行、现场备份恢复和回退演练。全部未操作真实数据库。

阻塞：SID 正式身份/会话/签名/通知契约、环境与测试用户及人工联调授权、可信入口与敏感日志门禁、已审批 HTTP 传输风险验收。等待正式参数和人工授权后再进入真实联调，不以云端 CI 代替发布验收。
