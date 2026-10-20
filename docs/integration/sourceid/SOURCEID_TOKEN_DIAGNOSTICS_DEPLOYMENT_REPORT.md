# KnowHub SID Token 诊断开发、部署与验收报告

后续状态更新：用户于本报告验收后授权持续开启 OIDC。2026-10-14 20:00 已执行，见 [当前开关状态](SOURCEID_PERSISTENT_OIDC_STATUS.md)。下文的“最终关闭”是当时部署验收快照，不代表最新运行开关。

2026-10-14。**已完成打包测试机 172.30.232.42 的 80/8080 覆盖升级及合成、业务验收。未发起新的真实 SID 登录，未访问生产服务器。**

## 代码与 CI

- 分支：feature/knowhub-sourceid-oidc。
- 实际构建、部署代码：`3be5d3f9268fe8819d66485a2b990c6237827e25`。
- [GitHub CI 38556052305](https://github.com/suoak/WeKnora/actions/runs/38556052305)：linux、postgres、frontend 全部成功。包括 Go 竞态及认证/MCP 回归、Mock、Basic 特殊字符、脱敏过滤/导出、数据库迁移、前端 23 项认证回调检查、类型检查及构建。
- 打包机沿用既有 Go 1.26.7、Rust、Node、离线依赖缓存和 runtime-8ae91bc44 基线；云端沿用既有 Go 1.26.8。未改 go.mod、go.sum、前端锁文件或数据库迁移。
- Git 源码归档 SHA-256：`e61614169f49bf6987465a9f41e51423a52ec6908dd6b26ec195a3b278b9aa0b`；只归档上述提交，不包含本地其他未提交开发。
- app 二进制 SHA-256：`39f582b85040e408b8d00f73528aa763a4a7d21abbf524cc928edb17d564635e`。

后续报告提交仅增加文档和验收证据，不改变此运行代码 SHA。

## 诊断实现与代码位置

| 位置 | 能力 |
| --- | --- |
| internal/application/service/user.go，exchangeOIDCCode（1790 起） | 保留原 Token 构造，接入请求前、发送后和响应读取后的诊断 |
| internal/application/service/sourceid_token_telemetry.go（27、125 起） | 严格白名单结构及 httptrace：DNS/TCP/TLS、远端地址、HTTP 协议、写出/首字节时间、连接复用、观察到的请求次数 |
| internal/application/service/sourceid_diagnostics.go（29、75 起） | 沿用 SOURCEID_DIAGNOSTICS_ENABLE；统一 Trace ID、三阶段事件、分类、输出限额 |
| internal/application/service/oidc_probe_diagnostics.go | 复用已有安全请求/响应检查，不重新实现认证 |
| internal/handler/auth.go（474） | state 和持久授权事务检查通过后，设置诊断 PASS 标志 |
| frontend/src/main.ts（53）、utils/oidcCallbackEntry.ts | 在初次路由加载前，将根路径 OIDC 回调结果送入登录页，避免提前加载 Portal |
| docs/integration/sourceid/business-test/export_token_diagnostics.py | 日志流经过封闭字段校验，导出受限 JSON，拒绝覆盖历史文件 |

三阶段事件为 sourceid_token_request、sourceid_token_send、sourceid_token_http，共用服务端生成的 Trace。请求记录 Client ID、正式 endpoint 的安全结构、Basic 策略和存在性/格式、Secret 加载布尔值、表单字段及重复检查、state、PKCE 和 callback 延迟；不输出任何凭据值或凭据指纹。nonce 在 Token 发送时标为 NOT_YET_EXECUTED，后续维持严格校验。

响应记录 HTTP 状态、耗时、类型、声明长度/实际读取/EOF/截断、WWW/Proxy challenge、安全 Server/Via/Date 分类、允许关联字段和 OAuth 枚举。空正文 401 + Basic challenge 自动分类为 `empty_401_basic_challenge`。拒绝组件仍为 UNKNOWN，不据响应外观猜测 SID 或网关根因。

诊断默认关闭；启用后单条 ≤32 KiB、每进程累计 ≤10 MiB、首次输出后窗口 ≤30 分钟、网络事件 ≤64 条。触及限额只停止诊断，不改变认证。app Docker 日志已设置 10m × 3 并压缩。存储保留期限仍需运维管理，输出窗口不等于日志自动删除。

## 实际运行状态

目录：`/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05`。

| 服务 | 镜像 | 最终容器 ID 前缀 | 端口 |
| --- | --- | --- | --- |
| app | knowhub-sourceid-business/app:3be5d3f92 | 8c41fa2ae75b | 0.0.0.0:8080 → 8080 |
| frontend | knowhub-sourceid-business/frontend:3be5d3f92 | 60280b103291 | 0.0.0.0:80 → 80 |

app 镜像 ID：`sha256:0890f309f89397e82ea5b1ecd901f2ddb37de142b78f854e9925a05430c34c1d`。

frontend 镜像 ID：`sha256:ae4a1207900234b131605ea3cdd4af8e99845faebdd1be87e00af150d34dbf34`。

两镜像 revision 标签均为完整代码 SHA。PostgreSQL、Redis、DocReader 的容器 ID 与升级前一致；原数据库、files、doc-tmp、Redis 数据卷未删除或重建。

Windows 实测 `http://172.30.232.42/login`、80 的 /health、8080 的 /health、/api/v1/auth/oidc/config 均返回 200；OIDC 配置返回 enabled=false。

## 备份、启动问题和回滚

最终升级备份：

`/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/upgrade-backups/token-diagnostics-20261014T114018Z`

- Compose 和非秘密模式配置已备份；原 private 配置文件未修改，SID Secret 文件仅核对权限 0600，未读取内容。
- PostgreSQL 自定义格式备份 353346 字节，pg_restore -l 目录验证通过，SHA-256 `b8744c1b829078e13c959476ed628a3fc85bf7421fc61174c8ded03edbdb8f7d`。
- files 卷以只读挂载备份，归档 181 字节；原卷保留。
- 旧 app/frontend 的 db1b2cd05 标签与镜像 ID 保留，并增加 rollback-db1b2cd05-token-diagnostics 标签；未执行 prune 或 down -v。
- 数据库 migration 仍为 3024；本次没有新增迁移。

首次升级因新镜像启动检查失败，自动回滚 **PASS**，恢复旧版本 80/8080。后续检查发现新归档配置文件权限为 0600，启动日志有配置/权限错误标志；为镜像内 config/migrations 补齐 appuser 的读取权限后重新打包，升级成功。保留原 runtime 用户及 entrypoint 的降权行为，不放宽密钥文件权限。首次失败记录独立保存，未覆盖为成功。

可执行回滚（只回退 app/frontend，保持 OIDC 和诊断关闭，不恢复/覆盖数据库）：

```sh
bash /home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/upgrade-backups/token-diagnostics-20261014T114018Z/rollback.sh
```

随后检查 80/8080 /health 以及 OIDC config enabled=false。首次失败已实际验证旧版本恢复；最终版本未再为演练额外回退。备份库只在另有数据恢复授权时使用，不自动执行覆盖恢复。

## 合成认证验收

使用真实 KnowHub frontend/app、专用无持久资料 Chrome、Windows localhost:80 → SSH → 打包机 80；Mock provider 经独立受信任测试 CA 验证，不关闭 TLS。浏览器仅允许测试域名和受控 Mock 授权拦截，不访问生产 IP。

测试配置明确移除 private/sourceid.env，使用合成 Secret；正式 SID 未参与。未进入审核绑定阶段、未创建员工账号或企业 JWT。

| 案例 | 实测结果 |
| --- | --- |
| 401 + 空正文 + Basic challenge | PASS；正确生成 empty_401_basic_challenge 和长度/HTTP/TLS/连接时间线 |
| 401 + OAuth invalid_client，描述含敏感回显 | PASS；仅保留安全错误类别，敏感描述未进入日志 |
| 401 + HTML 敏感错误页 | PASS；HTML 分类，原文及敏感回显未进入日志 |
| 200 + 合成签名 ID Token / UserInfo | PASS；流程到达 identity_contract_pending，不绑定、不签发企业 JWT |
| 三阶段关联及兑换次数 | 每个事务恰好三条阶段记录，同一 Trace；Mock 计数与 WroteRequest 均为一笔 |
| 日志隐私 | Secret、code、JWT、Token、身份样例、Authorization 原文均未发现；结构经过白名单校验 |
| 回调前端 | 四案例均未请求 PortalHome 资源、无观察到的 renderer crash |
| 收尾 | Mock 停止、真实配置恢复；Windows 80/18443 释放；用户/身份/SourceID 会话/auth_tokens 数量与合成前一致 |

机器记录内的 Mock Host/Path 保持真实合成值。验收工具仅在验证副本中将已确认 Mock endpoint 映射到正式白名单以复用 schema 校验，并明确标记；未将 Mock 数据冒充正式 SID 证据。

本次验证了“失败回调提前加载 Portal”修正及合成环境不崩溃，**没有复现原生交互式 Chrome STATUS_BREAKPOINT，不宣称该浏览器问题根因已经解决**。

## 业务回归和最终安全门禁

实际通过：既有本地合成账号登录、/auth/me、知识库/agents/models/MCP/capabilities API；知识库和手动草稿创建读取；MCP endpoint 初始化、tools/list 和工具调用；登出后本地 Token 被拒绝。验收创建的 MCP endpoint 和知识库已通过 API 清理。

升级时原记录数量完全一致。之后业务验收增加并软删除了一组知识库/知识记录，物理总行数由 1/1 变为 2/2，最终有效行数均为 0；旧实例原有记录没有迁移或删除，不宣称总行数未变。本地测试登录正常签发并撤销自己的测试会话，与企业身份门禁禁止签发企业 JWT 不冲突。未执行真实模型问答或外部知识源任务，不把基础 API 验收扩展为全部 RAG 质量验证。

Callback 无有效 state 时实际返回 302 + invalid_state；合成有效事务进入新版 **AuthHandler.OIDCRedirectCallback → userService.LoginWithOIDC → exchangeOIDCCode**，不是 Protocol Probe。

最终在服务进程内仅核对这些布尔变量：

```text
OIDC_AUTH_ENABLE=false
SOURCEID_DIAGNOSTICS_ENABLE=false
SOURCEID_PROTOCOL_ONLY=true
SOURCEID_CONTRACT_CONFIRMED=false
SOURCEID_LOGOUT_ENABLE=false
```

Mock CA 变量和 SSRF 白名单均为空；Mock 容器停止。Nginx 查询/请求头脱敏和 callback 禁止上游重放沿用已验收配置。凭据、授权码、Token、Cookie、HAR 和原始报文没有写入交付材料。

## 下一次真实验证新增的证据

等待用户另行授权后，可在同一业务链路采集实际 DNS/TCP/TLS、远端 IP/端口、HTTP 协议、写出/首字节时间、Basic 构造策略与数量、表单重复检查、Secret 非空布尔值、响应 Date/Server/Via/Proxy challenge 和完整安全响应分类。

连接复用或 Transport 未触发的事件标为 NOT_CAPTURED；本地远端 IP 不代替 SID 看到的出口 NAT。不能凭 Basic challenge 定位拒绝组件，也不能证明注册凭据版本或 code 服务端状态；这些仍需 SID 日志确认。2026-10-13 OIDC 切换配置版本及切换后 Postman 成功请求差异仍应重点核对。

**真实 SID 401 根因尚未确认；本轮没有新增真实 SID Token 请求。下一次员工认证等待单独授权。**

附件：[完整机器验收包](evidence/token-diagnostics-deployment-20261014.json)、[诊断字段与限制](SOURCEID_TOKEN_HTTP_REQUEST_RESPONSE_DIAGNOSTICS.md)、[SID 管理员沟通材料](SOURCEID_TOKEN_HTTP_401_SID_ADMIN_HANDOFF.md)、[历史脱敏证据图](evidence/token-http-diagnostics-evidence-20261014.png)。
