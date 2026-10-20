# SourceID raw Basic 兼容部署报告

日期：2026-10-15。状态：IMPLEMENTED / CI_PASS / DEPLOYED_PACKAGING；真实 SID 登录 NOT_TESTED。此次适配尚不能证明 HTTP 401 已修复。

## 代码与范围

- 分支：feature/knowhub-sourceid-oidc。
- 代码 SHA：`468410e893de4a34918e9a5201bbc3870dadc22c`。
- [GitHub CI：全部通过](https://github.com/suoak/WeKnora/actions/runs/38668598092)。代码归档 SHA256：`cefce5476089ca7f529d90678ff939412c7099b88c05268189150ab1b81d7695`。
- `internal/config/sourceid_basic_encoding.go`：新增 SOURCEID_BASIC_ENCODING，默认 oauth_form；仅 SourceID 生效，其他 Provider 忽略 raw 并继续 oauth_form。非法 SourceID 配置安全拒绝。
- `internal/application/service/user.go` 的 exchangeOIDCCode：oauth_form 保持 QueryEscape 后 SetBasicAuth；raw 直接 SetBasicAuth(clientID, clientSecret)。均由 Go 标准库执行一次 Base64、ASCII 冒号与单个 ASCII 空格。
- `internal/handler/auth.go` 与 `internal/types/user.go`：公开配置接口增加非秘密的 sourceid_basic_encoding，用于核对进程实际加载的有效模式。
- 原有诊断增加 raw 策略标签，Python 白名单同步允许；未复制诊断系统。
- 未改变 Token Body、state、nonce、PKCE、redirect_uri、HTTP Transport、TLS、超时、重试或身份门禁。没有数据库迁移。

## 合成验收与 CI

- 新增 TestSourceIDRawBasicExchangeModes：默认、oauth_form、raw、非 SourceID 忽略 raw，分别 Mock HTTP 200/401，共八笔独立合成兑换。每笔仅一次请求。
- Mock 验证只有一个 Authorization、准确的 Basic 字节格式和预期单次编码；raw 与管理员描述的 Base64(ClientID + ASCII 冒号 + ClientSecret) 一致。
- 表单仍为 grant_type、client_id、redirect_uri、code 四个字段，不含 client_secret、code_verifier 或 state。POST 与 urlencoded 不变。
- 使用合成特殊字符及普通字符完成两种编码比较；非保留字符时等价，+/=/%/:/空格/非 ASCII 等通常不同。字面量 % 编码一次不等于双重编码。
- 日志不含合成 Secret、完整 Basic/Base64、code 或 Token canary；诊断过滤与持久化测试通过。
- CI 专项含 185 个通过的测试条目（含子测试），回归含 4710 个通过条目，失败均为 0；前端类型检查、23 项认证/回调测试及构建、PostgreSQL 迁移与会话专项通过。
- 本机独立合成 Basic 模拟通过；完整 Go 验证以 CI 和原打包工具链测试为准。
- 打包机 anydoc 标签下业务 service/handler/config/middleware/utils/langfuse 与审核映射工具测试全部通过。
- 真实密钥未读取、解码、比较或输出；其两种算法是否等价：UNKNOWN。现有凭据版本及有效性仍须 SID 侧确认。

## 实际部署

仅对打包机 172.30.232.42 执行 SSH、构建和部署，用户 aqyfzx；没有对正式生产服务器执行 SSH 或部署操作。部署目录 `/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05`。

保持既有编译器、镜像来源、AnyDoc/DuckDB、缓存与运行基线，构建代码来自精确 Git 归档。打包机使用原 Go 1.26.7，CI 使用既有工作流 Go 1.26.8。镜像中仅修正非秘密 config/migrations 的可读权限以保留原非 root 业务启动方式；不更改受保护凭据文件权限。

| 服务 | 镜像 | 本地镜像 SHA256 | 实际发布端口 |
| --- | --- | --- | --- |
| app | `knowhub-sourceid-business/app:468410e89` | `sha256:e2feec2c9e25279140a811308ff5a4e61bbf5855e161b8fae39549f7159ee6ea` | 8080/tcp -> 0.0.0.0:8080 |
| frontend | `knowhub-sourceid-business/frontend:468410e89` | `sha256:f329c19f4c6a87d03caa6421870bc0fabb01b2b0821c6f15f8202b1bf4946919` | 80/tcp -> 0.0.0.0:80 |

app/frontend 的镜像 revision 标签均为上述完整代码 SHA。业务二进制 SHA256：`833a0e86f16a7c5bfde3efaede5cf547a99c7b015938f01598f40192c775c4ef`。镜像 ID 为本机构建内容摘要，不冒称远程 Registry digest。

运行中进程检查与经 frontend 转发的 `/api/v1/auth/oidc/config` 均确认有效模式 raw，而非仅修改文件。Windows 对打包机的只读 HTTP 验收见机器证据；80/8080 健康与登录页面均正常。

Windows 初次只读检查继承系统 HTTP 代理，虽请求目标写为打包机 IP，返回的配置却不含 raw 字段且显示默认 OIDC，不能证明请求实际到达打包机，代理下最终目标 UNKNOWN。随后仅针对打包机显式绕过代理，带 knowhub Host 的直连检查全部通过，返回 raw/disabled。未修改系统代理设置。下次真实浏览器必须继续使用受控直连/隧道并绕过代理，不能只凭 hosts 覆盖判断回调落点。

最终状态：

```text
SOURCEID_BASIC_ENCODING=raw
SOURCEID_PROTOCOL_ONLY=true
SOURCEID_CONTRACT_CONFIRMED=false
OIDC_AUTH_ENABLE=false
SOURCEID_DIAGNOSTICS_ENABLE=false
SOURCEID_LOGOUT_ENABLE=false
AUTO_MIGRATE=false
```

Mock 未运行，Mock CA/SSRF 白名单未注入。既有 Secret 文件权限为 0o600，未读取内容或重新生成凭据。公开配置 enabled=false。

## 数据、影响与回滚

- 只重建 app/frontend。PostgreSQL、Redis、DocReader 及其他既有运行容器 ID 均保持不变。
- 旧 app/frontend `3be5d3f92` 镜像及 rollback-before-raw 标签保留。未删除数据库、数据卷或历史文件。
- 备份目录：`/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/upgrade-backups/raw-basic-20261015T022248Z`，包含受保护 Compose/mode 配置备份与 PostgreSQL 自定义格式备份；pg_restore -l 验证通过。备份 354932 字节，SHA256 `c311390046e92415368e781301bc5223fa8e4b6c2d481a6d39fbde26308ecc01`。
- 数据库版本仍为 3024。users/knowledge_bases/knowledges/external_identities/source_id_sessions 升级前后计数完全一致；此次没有执行登录、绑定、业务数据写入或迁移。
- 回滚命令：`bash /home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/upgrade-backups/raw-basic-20261015T022248Z/rollback.sh`。只恢复旧 app/frontend 与原配置，OIDC/诊断仍保持关闭，不恢复或删除数据库。升级脚本内含失败自动回滚；本次成功，未实际切回旧版本，回滚演练 NOT_EXECUTED。
- 此次不再次创建本地用户、知识库或授权事务；已部署业务的本地登录/MCP/身份回归由本次 CI 和前一部署验收提供覆盖，不冒称本轮完成了新的交互登录。前端源码未变，失败回调测试通过；不能据此宣称 STATUS_BREAKPOINT 根因已解决。

## 下一次真实登录

当前可进入单次真实验证的准备阶段，但 OIDC/诊断关闭，尚未发起任何真实 SID 授权或兑换。需用户另行授权，再通过现有安全流程加载已轮换的有效凭据，并建立专用浏览器与打包机的受控路由。

下一笔保持其他协议参数不变，仅使用 raw。诊断会标记 raw_then_set_basic_auth_base64_once，记录三个阶段脱敏请求/网络/响应元数据。关注 Token 是否从 401 变为 200；仍 401 则停止，交 SID 核对凭据版本与网关日志，不自动重试。若 200，继续严格验证 ID Token、nonce 和 UserInfo；契约未确认则身份写入前停止，不开户、不绑定、不签发企业 JWT。

## 机器证据

- [CI 汇总](evidence/raw-basic-ci-summary-20261015.json)
- [原构建环境认证测试](evidence/raw-basic-build-tests-20261015.json)
- [实际升级及备份](evidence/raw-basic-upgrade-20261015.json)
- [运行版本和有效配置](evidence/raw-basic-final-audit-20261015.json)
- [Windows HTTP 验收](evidence/raw-basic-windows-http-20261015.json)
