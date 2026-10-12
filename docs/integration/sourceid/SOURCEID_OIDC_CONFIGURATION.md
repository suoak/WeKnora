# 配置与审核映射

复用既有 OIDC 环境变量；所有秘密仅注入服务端。以下为占位值，不能直接投入生产：

```dotenv
OIDC_AUTH_ENABLE=true
OIDC_AUTH_PROVIDER_DISPLAY_NAME=企业SID
OIDC_AUTH_CLIENT_ID=knowhub
OIDC_AUTH_CLIENT_SECRET=<secure-injection>
OIDC_AUTH_ISSUER_URL=<confirmed-issuer>
OIDC_AUTH_DISCOVERY_URL=<confirmed-discovery>
OIDC_AUTH_SCOPES=openid
OIDC_AUTH_REDIRECT_URI=http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback
OIDC_AUTH_CLIENT_AUTH_METHOD=client_secret_post
OIDC_AUTH_PKCE_ENABLE=false
SOURCEID_ENABLE=false
SOURCEID_LOGOUT_ENABLE=false
SOURCEID_CONTRACT_CONFIRMED=false
SOURCEID_CLAIMS_SOURCE=id_token
SOURCEID_EMPLOYEE_CLAIM=<confirmed-dot-path>
SOURCEID_SESSION_CLAIM=<confirmed-dot-path>
SOURCEID_STATUS_CLAIM=<confirmed-dot-path>
SOURCEID_ALLOWED_STATUS=<confirmed-exact-value>
SOURCEID_LOGOUT_ENCODING=xml
SOURCEID_LOGOUT_CIDRS=<trusted-ingress-peer-cidrs>
SOURCEID_LOGOUT_SECRET=<secure-random-at-least-32-characters>
```

Client authentication 仅支持 `client_secret_post` / `client_secret_basic`。PKCE 只在 SID 支持已验证后开启。
已有授权/Token/UserInfo/JWKS 显式端点变量可复用；Discovery issuer 不一致拒绝。不配置来源字段默认拒绝，而不是自动使用 OAuth2 的 attributes.GH/tgt。
业务 claims 来源为 `id_token`（默认）或 `userinfo`。状态可为字符串或 boolean 的精确 `true`/`false`；工号禁止数字强制转换，保留前导零。
SSRF 白名单只添加确认过的服务地址，绝不关闭 TLS 证书验证。

## 历史账号批量审核

先备份数据库。通过可信 HR/管理员渠道核对真实自然人工号与现有用户 ID，检查重复邮箱、禁用账号、多身份和权限归属。
CSV 内容严格为 `employee_no,user_id`，不使用邮箱自动匹配；reviewer 和 reference 记录独立审核凭据。

```csv
employee_no,user_id
00000001,00000000-0000-4000-8000-000000000001
```

编译 `go build -o sourceid-enroll ./cmd/sourceid-enroll`。
通过安全环境变量提供 `SOURCEID_ENROLL_DSN`，不要把 DSN 写入命令参数、报告或 Git。

```sh
./sourceid-enroll --input reviewed.csv --issuer '<confirmed-issuer>' --reviewer '<reviewer-id>' --reference '<approval-ticket>'
```

默认只预检、不写入；人工复核报告后增加 `--apply`，整批事务提交，任一冲突整批回滚。单个 CSV 最多 1 MiB / 10000 行，超限拒绝，不截断导入。
已有相同登记幂等跳过，不覆盖 reviewer、reference 或正确身份。唯一约束保护并发导入。
新员工先用现有系统管理员开户流程（沿用默认空间策略、普通权限），再登记审核映射。
本任务没有操作生产数据库。
