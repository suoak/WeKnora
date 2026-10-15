# OA/SID 阶段 B 打包测试机实际部署报告

日期：2026-10-15。状态：**DEPLOYED_GATED / DB_3027_CLEAN / REAL_OA_LOGIN_NOT_TESTED**。

已按用户批准升级 `172.30.232.42` 的默认 app/frontend，并对现有测试库执行一次 003027 up。最终运行目标版本 `431226c3b`，80/8080 正常，身份门禁保留。过程中发生一次检查脚本误判并自动回滚，修正挂载比较后完成升级；没有重复执行迁移。未访问正式生产服务器，未真实 OA 登录、绑定员工或启用 local_approved。

## 1. 实际部署与版本核验

| 项目 | 升级前 | 最终运行 |
| --- | --- | --- |
| 代码完整 SHA | `2fcbcaaedfa75fb955bd44dd7fe464ded64eec7c` | `431226c3b1bd5338ec06c1bd0520e637d5fed398` |
| app 容器 | `89f47e199a14…` | `ceac394ee463…` |
| frontend 容器 | `dcbeecba4f42…` | `309f4982cf11…` |
| frontend | `0.0.0.0:80` | 不变，HTTP 200 |
| app | `0.0.0.0:8080` | 不变，Docker healthy、HTTP 200 |
| 数据库 | `3026 / dirty=false` | `3027 / dirty=false` |

Compose 项目仍为 `knowhub-sourceid-business`，部署目录仍为 `/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05`。

最终 app image ID：

```text
sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1
```

最终 frontend image ID：

```text
sha256:5aa70b6378d4fdbcb173d7a3bd4e4494b1218cafd148e9414684014ca90a3ac1
```

两张镜像 OCI revision 均为完整目标 SHA，app `/api/v1/system/info` 实际返回 `431226c3b`。采用预演已校验的现有候选镜像，没有重建或拉取其他版本。前端制品与目标构建产物逐文件一致的证据沿用[部署预检](SOURCEID_PHASE_B_DEPLOYMENT_PREFLIGHT_REPORT.md)，本轮复核实际运行 image ID 和 revision。

目标代码的 [CI 38717758880](https://github.com/suoak/WeKnora/actions/runs/38717758880) 四个 job 已通过。本轮没有改变应用认证、Basic、前端、会话或身份绑定代码，仅执行部署与验收。

## 2. image-pin 处理及配置来源

最终两个服务的 Compose 文件列表均为：

```text
/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/compose.yml
/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml
```

旧 Basic 收口的 `private/basic-encoding-backups/20261015T112150Z/image-pin.yml` 已从本次升级命令中排除，但文件保留。candidate pin 按不可变 image ID 固定 app/frontend，pull_policy=never。已检查最终容器实际 image ID，排除了“标签变化但仍运行旧镜像”。

基础 Compose、三个既有 env_file、candidate pin 及 rollback pin 的内容在操作前后保持不变；仅改变运行容器采用的 override 文件。未输出完整环境或 Compose 展开配置。

## 3. 最新备份、迁移和数据影响

先停止测试 frontend/app 写入，再使用 PostgreSQL 本地受控进程制作最新 `pg_dump -Fc`。原依赖容器继续运行。

备份根目录：

```text
/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/private/phaseb-upgrade-backups/
```

| 备份 | 用途 | 状态 |
| --- | --- | --- |
| `20261015T121331Z/test-db-3026.dump` | 实际迁移前最新备份 | 372,058 字节；已再次恢复到独立临时 PostgreSQL，3026 clean、核心行数核对通过 |
| `20261015T121703Z/test-db-3027.dump` | 恢复执行应用升级前最新备份 | 374,667 字节，保留 |
| 两个目录各 7 份受保护配置副本 | 基础 Compose、当时使用的 pin、candidate/rollback pin、三个 env_file | 均保留，仅远端可访问 |

目录 0700、文件 0600；配置或数据库备份未下载、展示或加入 Git。备份恢复审计只输出计数，不读取员工字段或 Token 原值。临时审计 PostgreSQL 使用独立 internal 网络、tmpfs 和合成配置，不挂载业务卷，已清理。[实际新备份恢复证据](evidence/phaseb-fresh-backup-audit-20261015.json)。

003027 使用目标镜像内标准 `migrate` 工具执行 **up 1 一次**。一次性迁移进程从既有受保护 env_file 加载 DB 配置，在进程内编码 DSN；不经过会打印数据库密码的旧 migrate.sh，不输出 CLI 敏感参数或原始错误。迁移容器禁用 Docker 日志、无业务端口/数据卷、无重启策略，完成后移除。

迁移结果为 3027、dirty=false；只新增 source_id_local_sessions，初始行数 0。users、tenants、tenant_members、knowledge_bases、外部身份、SID Session、绑定审核和飞书表计数与迁移前一致。003025/003026 没有重跑或跳过，没有 force、down 或破坏性重建。

验收期间只新增并通过 API 删除本轮的一个合成知识库和一个合成 MCP endpoint；删除采用既有软删除机制。知识库总行数因此 **8→9**，原 8 行保留。本次实际迁移前备份恢复证明：原库有效知识库数为 **0**；验收清理后仍为 **0**，不能将原 8 行历史记录描述为 8 个可用知识库。未迁移或删除历史数据。

最终 users=1、tenants=1、tenant_members=1；外部身份、SID Session、审核绑定、飞书连接/导入及本地企业 Session 均为 0。未创建员工、修改成员关系或建立企业绑定。

## 4. 实际验收

[部署执行收据](evidence/phaseb-packaging-upgrade-20261015.json)、[独立收尾复核](evidence/phaseb-packaging-closeout-20261015.json)、[Windows 绕过系统代理直连检查](evidence/phaseb-packaging-windows-http-20261015.json)。

| 检查 | 实际结果 |
| --- | --- |
| frontend 80 `/`、`/login` | 服务器及 Windows 直连均 HTTP 200 |
| app 8080 `/health` | HTTP 200，healthy，restart_count=0 |
| app 实际二进制版本、frontend OCI revision | 目标版本，通过 |
| 前端代理 OIDC config | HTTP 200，oauth_form，批准 callback 未变 |
| 原专用合成本地账号登录、auth/me | 均 HTTP 200 |
| 知识库列表、agents、models、MCP 配置/工具目录 | 均 HTTP 200 |
| 合成知识库创建、授权读取 | HTTP 201 / 200，返回本次创建的 ID |
| 不携带凭据读取知识库 | HTTP 401，符合保护预期，与 SID Token 状态无关 |
| MCP initialize | JSON-RPC 成功，serverInfo / capabilities 存在 |
| MCP tools/list | 精确返回本轮允许的 3 个工具 |
| MCP list_knowledge_bases / list_documents / wiki_search | tools/call 成功、无 JSON-RPC error / isError；仅访问空的合成知识库 |
| 无效 MCP Token | HTTP 401，安全拒绝 |
| 飞书本人配置、合成知识库个人文档列表 | 均 HTTP 200，无真实飞书授权或导入 |
| 飞书管理员配置 GET | HTTP 403；现有合成 owner 不是 system admin，权限保护符合预期 |
| 缺 state 的业务 OIDC callback | HTTP 302 → oidc_error=invalid_state；未进入 Token 兑换 |
| 合成资源清理 | 新 MCP endpoint / 知识库删除均 HTTP 200，未触及原资源 |
| 本地退出及退出后 auth/me | 退出 HTTP 200，随后 401 |
| PostgreSQL / Redis / DocReader / sid-mock | 原容器 ID、启动时间、镜像、挂载及状态不变；sid-mock 仍停止 |

**验收限制：**飞书管理员配置的授权读取/保存未测试（当前没有可用于该操作的 system admin 测试夹具，不擅自提升权限）；已验证的是非管理员拒绝。真实飞书 OAuth、文档导入、非空知识库检索、向量/模型推理和完整 RAG 质量均 NOT_TESTED。MCP 的检索验收是空合成知识库的 wiki_search，不把它描述为向量检索质量验收。

前端失败回调防止提前加载 Portal 的合成回归沿用目标 SHA 的既有 CI，本轮检查实际业务 Handler 的 invalid_state 分支。没有重新进行真实 SID/浏览器 STATUS_BREAKPOINT 复验，不宣称解决该历史现象。

## 5. 日志与最终安全配置

新容器整个本轮验收日志在受控进程内扫描：app 4,925 字节、frontend 4,104 字节；未发现本次生成的本地 JWT、MCP Token、合成密码，也未发现完整 Basic/Bearer Header、JWT 或 SID code/access-token 模式。只保存扫描结果，不保存原始日志。此项不是对任意未知个人信息的无限保证；本轮未进行真实员工操作。

| 配置 | 最终现场 |
| --- | --- |
| SOURCEID_BASIC_ENCODING | **oauth_form** |
| SOURCEID_PROTOCOL_ONLY | **true** |
| SOURCEID_CONTRACT_CONFIRMED | **false** |
| SOURCEID_AUTO_BINDING_ENABLE | **false** |
| SOURCEID_EMAIL_BINDING_ENABLE | **false** |
| SOURCEID_EMAIL_CONTRACT_CONFIRMED | **false** |
| SOURCEID_LOGOUT_ENABLE | **false** |
| SOURCEID_SESSION_MODE | 未设置，代码默认 external；未启用 local_approved |
| OIDC_AUTH_ENABLE | **true**，维持升级前现场，不代表业务身份放行 |
| SOURCEID_DIAGNOSTICS_ENABLE | **true**，维持升级前现场 |
| OIDC_AUTH_PKCE_ENABLE | false，不变 |
| AUTO_MIGRATE / AUTO_RECOVER_DIRTY | false / false，不变 |

没有填入虚构审批编号、准入时效或会话期限；没有读取或输出真实 SID Secret、code、Token、Cookie 或员工值。线上生产环境未访问。

## 6. 失败、恢复及最终成功时间线

| 北京时间 | 事实 |
| --- | --- |
| 20:09 | 最终前置复核：2fcbcaaed、DB 3026 clean、原配置一致 |
| 20:13:31 | 第一次维护窗口开始，停止 app/frontend、制作最新 3026 备份 |
| 20:13–20:14 | 003027 up 成功；候选启动 healthy 后，检查脚本因无序 Mounts 列表的直接比较触发断言，自动撤销本地企业会话范围并回滚旧 app/frontend |
| 20:14:07 | 旧版本地登录、核心 GET、退出后失效、oauth_form 验证通过，库保留 3027 clean |
| 20:16 | 恢复执行的前置检查也受到列表顺序影响而停止，未进入维护或重复迁移 |
| 20:17:03 | 修正为按 Destination 比较完整挂载属性后，复核已知恢复现场，重新备份 3027，只恢复执行 app/frontend 替换 |
| 20:17:08 / 20:17:14 | 最终目标 app / frontend 启动 |
| 20:17:32 | 业务验收完成，通过 |
| 20:20:30 | 独立只读收尾复核通过，DB 3027 clean，依赖容器未变 |

首轮失败与回滚记录未覆盖：[首轮收据](evidence/phaseb-packaging-upgrade-attempt1-20261015.json)、[恢复盘点](evidence/phaseb-upgrade-recovery-inventory-20261015.json)、[前置停止记录](evidence/phaseb-packaging-upgrade-resume-precheck-failure-20261015.json)。后续只修正部署检查工具，不改变应用认证代码或镜像。本次原库的 003027 up 总执行次数仍为 **1**。最终不是停留在旧版回滚状态。

## 7. 保留的回滚能力

旧 app 镜像 `sha256:93f5c831b6e061832d71aa19fe74835c78e16b637d77e7ce18a4283e9ed62daa`、旧 frontend 镜像 `sha256:4dc1ca5eacbb723000d069e947da4876e7a479f3e95ea5cea4c234a9c295dcba` 均保留；受保护回滚配置：

```text
/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/rollback-oauth-form-image-pin.yml
```

本轮实际自动回滚已验证：先撤销 source_id_local_sessions 对应的 auth_tokens / source_id_sessions（当前为空），再用基础 Compose + 上述 rollback pin、--no-deps --no-build --pull never --force-recreate 仅恢复 app/frontend；保留 DB 3027。oauth_form 与身份门禁均保持。本地登录、auth/me、知识库/MCP/飞书本人列表和退出后令牌失效均通过。没有使用旧 Basic 回滚脚本，没有恢复 raw 或执行数据库 down。

未来若再次回滚，必须在单独批准的维护操作中先停止企业登录写入、归档受保护现场并撤销本地企业会话及派生 Token，再恢复旧应用。旧应用不能实施新本地会话限制，不能省略撤销。无需删除新增表或恢复整个数据库，更不能删除原卷与审核记录。

## 8. 下一阶段尚未完成

**代码升级与测试库迁移已完成。真实企业身份放行仍未批准。**

后续仍需完成 SID 授权准入时效/已有 SSO 重新检查边界、本地会话安全审批、普通员工自动绑定策略批准、受控测试邮箱账号和权限基线准备；之后才能单独批准配置放行与一次真实 OA 登录。本轮没有新的 SID Token 200、真实绑定、企业 Session 或 Portal 登录证据。
