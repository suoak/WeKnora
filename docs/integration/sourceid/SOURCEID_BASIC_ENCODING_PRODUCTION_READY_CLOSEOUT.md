# SID Basic 编码配置收口实测报告

日期：2026-10-15。状态：**CONFIG_SWITCH_COMPLETED / REAL_SID_OAUTH_FORM_LOGIN_NOT_TESTED**。

文件名遵循交付要求，不代表生产发布批准或真实 SID oauth_form 登录已经通过。本轮仅操作打包测试机 `172.30.232.42`，SSH 用户 `aqyfzx`，采用已有可信主机密钥校验；未连接正式生产服务器。

## 实际版本及变更范围

| 项目 | 实测结果 |
| --- | --- |
| 运行代码完整 SHA（镜像 revision 标签） | `2fcbcaaedfa75fb955bd44dd7fe464ded64eec7c` |
| Compose 项目 | `knowhub-sourceid-business` |
| 部署目录 | `/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05` |
| app 镜像 ID（前后完全相同） | `sha256:93f5c831b6e061832d71aa19fe74835c78e16b637d77e7ce18a4283e9ed62daa` |
| frontend 镜像 | `knowhub-sourceid-business/frontend:2fcbcaaed` |
| frontend 镜像 ID（未变化） | `sha256:4dc1ca5eacbb723000d069e947da4876e7a479f3e95ea5cea4c234a9c295dcba` |
| app 容器 | `03b06efdafe2…` → `89f47e199a14…`；只重建 app |
| frontend 容器 | `dcbeecba4f42…`，前后相同 |
| 发布端口 | frontend `0.0.0.0:80`，app `0.0.0.0:8080`，均不变 |
| Basic 配置 | **raw → oauth_form** |
| 数据库迁移状态 | 前后均 `3026 / dirty=false` |

配置事务开始于北京时间 **19:21:46**，app 新容器于 **19:21:51** 启动，最终只读复核于 **19:22:42** 完成。未构建或拉取镜像；未部署 `431226c3b` 或当前开发分支代码；未执行 003027 或任何迁移命令。

## 远端凭据门禁

1. 远端受控 Python 进程仅在内存中读取当前容器配置及现有受保护配置源，使用与 Go 对应的凭据规范化/表单编码，分别构造 raw 与 oauth_form Header 并比较；未将 Secret、完整 Header、Base64、长度或凭据指纹输出给 Agent。
2. 当前容器实际凭据：**EQUIVALENT=true**。配置源中将用于重建的 Client ID/Secret 与当前容器一致，且同样 **EQUIVALENT=true**。切换后再次检查仍为 **EQUIVALENT=true**。
3. 同一个旧 app 容器自 14:22 启动后未重启，其脱敏记录中存在 8 笔历史 Token HTTP 200，时间为北京时间 14:48–14:50，使用 raw。容器配置、待加载配置与历史成功实例可关联，满足使用历史已成功凭据版本的门禁。
4. **这 8 笔是历史记录，不是本轮新增请求。** 未向 SID 查询当前密钥登记状态，也未重新测试其当前有效性。本轮没有发起 authorize、真实员工登录或授权码兑换。

只保存了安全元数据与布尔结果，没有凭据值、哈希、原始请求/响应或原始日志文件。

## 实际配置修改与镜像固定

生效来源为 `compose.yml` 的 `services.app.environment.SOURCEID_BASIC_ENCODING`，会覆盖 env_file 中的同名值，因此只修改了这一处字面值。程序比较 YAML 结构，确认基础 Compose 的语义变更只有这一项。

未修改 `private/sourceid.env`、Client Secret、其他 env_file、SID 端点、Token Body、callback、scope、PKCE、state/nonce、Session 或身份绑定策略。重建前后 app 的**全部环境变量在受控进程内逐项比较，仅 Basic 编码值不同**；不输出环境转储或凭据比较指纹。

为严格固定现有镜像 ID，使用单独的 `image-pin.yml` 覆盖文件，仅包含 app 的原 image ID 和 `pull_policy: never`。基础 Compose 的 image 标签没有修改；容器的显示镜像引用因此成为同一个 SHA256 ID。这是固定镜像的操作元数据，不是版本升级。

实际重建只针对 app，使用 `--no-deps --no-build --pull never --force-recreate`。frontend、PostgreSQL、Redis、DocReader 容器 ID、启动时间、镜像、端口和挂载前后相同；停止状态的 sid-mock 也未被启动。app 挂载和端口不变，全部原数据卷保留。

`AUTO_MIGRATE=false`、`AUTO_RECOVER_DIRTY=false` 前后不变；只读查询确认 schema_migrations 前后均为 3026 且 clean。未进行迁移、数据库重建、数据卷操作或业务资源增删。

## 原开关与安全门禁保持不变

| 配置 | 切换前后 |
| --- | --- |
| OIDC_AUTH_ENABLE | true |
| SOURCEID_DIAGNOSTICS_ENABLE | true |
| SOURCEID_ENABLE | true |
| SOURCEID_PROTOCOL_ONLY | true |
| SOURCEID_CONTRACT_CONFIRMED | false |
| SOURCEID_EMAIL_BINDING_ENABLE | false |
| SOURCEID_EMAIL_CONTRACT_CONFIRMED | false |
| SOURCEID_AUTO_BINDING_ENABLE | false |
| SOURCEID_LOGOUT_ENABLE | false |
| SOURCEID_SESSION_MODE | 未设置；未启用 local_approved |
| OIDC_AUTH_PKCE_ENABLE | false |

OIDC 和诊断原本已开启，按本次“保持原配置”的授权保留，没有额外打开或关闭。身份门禁继续阻止真实员工绑定及业务会话放行。

## 实际验收

| 检查 | 结果 |
| --- | --- |
| app 8080 `/health` | HTTP 200，Docker healthy |
| frontend 80 `/` | HTTP 200 |
| Windows 绕过系统代理，直连打包机 80/8080 | 前端、app health、OIDC 配置 API 均 HTTP 200，编码为 oauth_form |
| 经 frontend 代理的 OIDC 配置 API | HTTP 200，`sourceid_basic_encoding=oauth_form` |
| 直接 app 的 OIDC 配置 API | HTTP 200，`sourceid_basic_encoding=oauth_form` |
| callback 配置 | 仍为 `http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback` |
| 原有专用合成本地账号登录 | 切换前后均 HTTP 200 |
| 带本地业务令牌访问 `/api/v1/auth/me` | 切换前后均 HTTP 200 |
| 知识库、agents、models 基础 GET API | 切换前后均 HTTP 200 |
| MCP endpoints 及 tools 列表 GET API | 切换前后均 HTTP 200 |
| 飞书本人配置 GET API | 切换前后均 HTTP 200；未发起飞书授权 |
| 本地账号退出与令牌失效 | 退出 HTTP 200，随后 auth/me HTTP 401，符合预期 |

只使用既有专用合成账号，凭据及本地 JWT 仅在受控进程内使用、不输出或落盘。登录/退出按现有认证机制产生正常的认证记录；未创建真实员工账号、身份绑定、租户、知识库或 MCP 资源。上述退出后的业务 API 401 与 SID Token HTTP 状态无关。

基础 GET 验收不等同全文档处理、RAG 或真实飞书/OA 授权验收。本轮没有测试真实 SID oauth_form Token 200、ID Token、nonce 或 UserInfo，相关状态为 **NOT_TESTED**。

## 备份及回滚准备

受保护备份目录：

```text
/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/private/basic-encoding-backups/20261015T112150Z
```

- 目录权限 `0700`；`compose.yml.before`、`image-pin.yml` 均为 `0600`；当前 Compose 也保持 `0600`。
- 回滚脚本 `rollback.sh` 为 `0700`，`sh -n` 语法检查 PASS。备份对象为待修改的 Compose；没有另外导出 SID Client Secret。
- 脚本恢复原 raw Compose，然后使用相同 image-pin、相同镜像 ID，仅重建 app。不会回滚阶段 B/飞书代码、数据库迁移或数据卷。
- 本次切换和验收成功，**未实际执行回滚**，也未为演练而再次中断服务。
- 受批回滚命令：`sh <上述目录>/rollback.sh`。完成后仍须复核 app/front 健康、配置 API 的 raw、本地登录、原安全门禁和 schema 3026。
- 镜像覆盖文件已受保护保留，容器 Compose 标签记录了 base+pin 两个文件。后续获批版本升级时应明确处理这个固定镜像覆盖文件，不能误以为修改基础 image 标签就会覆盖 pin。

## 收口结论

**标准 oauth_form 配置已实际加载，业务基础验收通过，镜像和安全门禁未变。**

**真实 SID oauth_form 登录验证尚未执行。** 不能将本轮健康检查或历史 raw Token 200 当作新的 oauth_form Token 200 证据。若需要新的真实协议证据，仍须另行批准一笔新授权码事务，保留全部既有安全校验和身份写入门禁。

机器可读证据：[执行前核查](evidence/basic-closeout-remote-precheck-20261015.json)、[配置源与迁移开关](evidence/basic-closeout-startup-check-20261015.json)、[配置事务及验收](evidence/basic-closeout-apply-20261015.json)、[最终复核及回滚权限](evidence/basic-closeout-final-check-20261015.json)、[Windows 直接访问验收](evidence/basic-closeout-windows-http-20261015.json)。
