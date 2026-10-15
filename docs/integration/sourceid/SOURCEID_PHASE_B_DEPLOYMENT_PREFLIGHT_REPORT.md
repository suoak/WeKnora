# OA/SID 阶段 B 打包机部署前核查与隔离预演

日期：2026-10-15。结论：**PREFLIGHT_PASS / LIVE_UPGRADE_AWAITING_APPROVAL**。

本轮只连接打包测试机 `172.30.232.42`（`aqyfzx`），完成只读盘点、候选镜像构建、受保护备份及隔离克隆库预演。未访问正式生产服务器，未替换默认 80/8080 容器，未迁移原数据库，未发起真实 SID 登录。候选版本具备提交部署审批的条件；不代表企业身份或本地会话已获准真实放行。

## 1. 当前运行现场

最终核查时间：北京时间 **2026-10-15 20:00:40**（UTC `12:00:40`）。[初始盘点](evidence/phaseb-deployment-inventory-20261015.json)、[最终只读复核](evidence/phaseb-preflight-final-20261015.json)。

| 项目 | 实际结果 |
| --- | --- |
| 当前代码 | `2fcbcaaedfa75fb955bd44dd7fe464ded64eec7c` |
| Compose 项目 | `knowhub-sourceid-business` |
| 运行目录 | `/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05` |
| app 容器 | `89f47e199a14…`，healthy，restart_count=0 |
| frontend 容器 | `dcbeecba4f42…`，running，restart_count=0；未配置 Docker healthcheck |
| 端口 | frontend `0.0.0.0:80`；app `0.0.0.0:8080` |
| 原库 | `schema_migrations=3026 / dirty=false`，约 32.4 MiB |
| 原库行数 | users=1、tenants=1、tenant_members=1、knowledge_bases=8 |
| 绑定与飞书数据 | external_identities / source_id_sessions / source_id_binding_reviews / feishu_connections / feishu_personal_imports 均为 0 |
| 最终 HTTP | 前端 `/`、app `/health`、经前端代理的 OIDC config 均 200 |

预演前后默认项目所有容器的 ID、镜像、创建/启动时间、重启次数、端口和挂载逐项比较一致。PostgreSQL、Redis、DocReader 和停止状态的 sid-mock 均未重建或重启。原 PostgreSQL、Redis、文件及 DocReader 共享卷均保留；未执行 prune、down -v 或数据库重建。

## 2. image-pin.yml 的实际影响

当前 app 实际使用两个 Compose 文件：

1. `compose.yml`
2. `private/basic-encoding-backups/20261015T112150Z/image-pin.yml`

第二个文件固定旧 app image ID，因此**只改基础 Compose 的 image 标签仍会运行旧版本**。已在独立预演目录生成以下 0600 文件，均未应用：

```text
/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml
/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/rollback-oauth-form-image-pin.yml
```

正式升级获批后，只使用基础 `compose.yml` 加新的 candidate override；从本次升级命令的 `-f` 列表中排除旧 pin，不删除旧 pin。新 override 同时按不可变 image ID 固定 app/frontend，`pull_policy: never`，显式保留 oauth_form、protocol_only=true、contract_confirmed=false、auto_binding=false。其余环境、卷、依赖服务不变。

回滚 override 固定当前旧 app/frontend image ID，并显式保留 **oauth_form**。不能使用此前会恢复 raw 的旧 Basic 切换回滚流程。

## 3. 候选来源、CI 与实际镜像

目标代码：`431226c3b1bd5338ec06c1bd0520e637d5fed398`。使用 `git archive` 导出该提交，未混入工作区未提交的 UI、MCP 或文档改动。代码归档 SHA256：`651b092c9228402ded1cc6d7562b6faac22c67aed604e7d56b768e4d9e0220c7`。

重新查询 [CI 38717758880](https://github.com/suoak/WeKnora/actions/runs/38717758880)，head SHA 精确为目标提交，frontend / postgres / linux / browser 四个 job 全部 SUCCESS。复用已有认证、MCP、飞书、迁移链及真实 Vue + Mock SID 浏览器证据，不因本次预演重复开发认证模块。[完整既有验收](SOURCEID_OA_LOGIN_E2E_ACCEPTANCE_REPORT.md)。

候选构建目录：`/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b`。沿用原 Go **1.26.7** 编译器、已有 AnyDoc/Rust/Node 与缓存、离线 SheetJS 包和原 runtime 基线；未升级系统库或更改既有构建镜像源。构建器固定 ID 为 `sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb`。CI 的 Go 版本与打包基线版本不同，不混称为同一构建环境。

打包机定向认证/配置/Handler 等 Go 测试、enrollment 测试、前端类型检查和构建通过，构建于 UTC `11:47:56` 完成。[构建状态](evidence/phaseb-build-progress-20261015.json)。

| 候选 | 实际 image ID | revision 与制品检查 |
| --- | --- | --- |
| `knowhub-sourceid-phaseb/app:431226c3b` | `sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1` | OCI revision 为完整目标 SHA；真实 `/system/info` 返回 `431226c3b`；镜像内二进制与构建制品字节摘要一致 |
| `knowhub-sourceid-phaseb/frontend:431226c3b` | `sha256:5aa70b6378d4fdbcb173d7a3bd4e4494b1218cafd148e9414684014ca90a3ac1` | OCI revision 为完整目标 SHA；镜像内 237 个前端文件逐一与该次构建 dist 摘要一致 |

app 二进制 SHA256：`eb0ae602ebab84182e8e4090e0e2a73c71ece7b50f60c0b9ae0443a199dee9a5`。这些是代码/制品摘要，不是任何凭据指纹。

与当前版本比较，目标的依赖锁文件未变；保留既有飞书、RJEMAIL/RJGH 关联能力，增加 local_approved 默认关闭的实现与 003027。无需为了该版本额外引入新的飞书业务提交或升级 PostgreSQL/Redis/DocReader。

## 4. 备份与迁移链

已有迁移链包含飞书 003025、审核绑定 003026；原库当前为 3026 且对应表存在。本次不跳过、不重跑 003025/003026。完整历史链由既有 CI 验证；现场克隆库仅执行 **3026 → 3027**。

受保护备份位于：

```text
/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/private-retry7/backups/
```

数据库 `test-db-3026.dump` 为 `pg_dump -Fc`，372,058 字节，已实际恢复到独立 PostgreSQL 并校验表/行数。配置备份包括基础 Compose、当前旧 pin、app.env、mode.env、sourceid.env；目录 0700、文件 0600。含敏感配置的备份仅留受保护远端目录，未下载、展示或提交 Git。此备份是核查时快照，正式执行升级前必须再制作最新备份。

003027 仅新增 `source_id_local_sessions` 元数据表，并外键关联已有 source_id_sessions；不迁移 users、租户、邮箱、成员或知识库权限。**已完成隔离库 up/down 实测**：

| 预演项目 | 结果 |
| --- | --- |
| 从现有 3026 备份恢复 | PASS |
| 3026 → 3027，dirty=false | PASS |
| 飞书表、审核映射表保留，核心行数不变 | PASS |
| SQL 事务撤销本地企业 Session 及派生 Token（合成夹具） | PASS |
| 3027 down 先撤销 Token/Session 再删除元数据 | PASS |
| 再次 up，启动新 app/frontend | PASS |
| 旧 app 使用保留 3027 的库启动并登录 | PASS |
| 最后回到 3026，核心行数不变 | PASS |

隔离 PostgreSQL 使用原 PostgreSQL 镜像、独立 tmpfs 数据、独立 Redis 和 Docker internal 网络。无宿主机发布端口、不连接 SID 或业务依赖、不挂载原数据卷；只使用合成 JWT 签名配置。保留的合成本地账号仅在克隆库登录，Token 只在进程内使用。

## 5. 应用兼容与清理实测

[最终预演原始脱敏收据](evidence/phaseb-local-rehearsal-20261015.json)，完成时间北京时间 **19:58:21**。

新 app 与回滚旧 app 分别通过：本地登录、auth/me、知识库列表、agents、MCP endpoints、飞书本人配置、system/info、退出。OIDC config 为关闭状态且编码 oauth_form；新 frontend 首页及到新 app 的 API 代理通过。

这些是克隆库上的实际容器与 HTTP 检查，不等于真实飞书授权、MCP 工具执行、文档处理、模型推理或完整 RAG 质量验收。普通员工自动关联、Portal、刷新/撤销等 Mock 业务断言复用目标 SHA 的既有 CI；本轮克隆本地登录使用已有合成账号，不冒充真实员工阶段 B 登录验收。STATUS_BREAKPOINT 未做真实员工复验，不宣称解决。

预演工具曾出现克隆恢复初始化时序、预装 schema 冲突、合成 DB DSN 空密码和 internal 网络不转发已发布端口的问题。仅修正隔离预演工具：等待最终 TCP 就绪、clean/if-exists 恢复到专用克隆库、使用非空合成 DB 密码、宿主机直连 internal bridge 地址；未改应用认证代码、TLS 或默认网络。早期失败收据保留，最终成功收据不代替或伪造早期结果。

临时新/旧 app、frontend、PostgreSQL、Redis 和 internal 网络已按明确名称逐项移除；原默认容器及原卷均未变化。候选镜像、代码和受保护备份保留待审批。临时克隆库已随 tmpfs 容器清理。

## 6. 安全门禁实际状态

| 配置 | 默认服务本轮前后 | 隔离新版本预演 |
| --- | --- | --- |
| SOURCEID_BASIC_ENCODING | oauth_form | oauth_form |
| SOURCEID_PROTOCOL_ONLY | true | true |
| SOURCEID_CONTRACT_CONFIRMED | false | false |
| SOURCEID_AUTO_BINDING_ENABLE | false | false |
| SOURCEID_EMAIL_BINDING_ENABLE | false | false |
| SOURCEID_EMAIL_CONTRACT_CONFIRMED | false | 未启用 |
| SOURCEID_SESSION_MODE | 未设置，默认 external | external |
| SOURCEID_LOGOUT_ENABLE | false | false |
| OIDC_AUTH_ENABLE | **true，沿用现场，未修改** | false |
| SOURCEID_DIAGNOSTICS_ENABLE | **true，沿用现场，未修改** | false |
| OIDC_AUTH_PKCE_ENABLE | false | 未启用 |
| AUTO_MIGRATE / AUTO_RECOVER_DIRTY | false / false | false / false |

未修改员工准入、会话审批或自动绑定配置，没有使用新的本地会话批准参数。没有向 Agent 输出 SID Secret、Token、code、Cookie 或员工值。

## 7. 获批后的部署与回滚顺序

以下为待执行步骤，本轮未对默认服务执行：

1. 复核版本、容器和 schema 仍等于本报告，创建最新数据库与配置备份，保留当前旧镜像与原卷。进入短维护窗口，停止 app/frontend 写入；不停止依赖服务。
2. 只用受控迁移进程，对原测试库执行已验证的 003027 up 一次；DSN 从受保护来源加载，不输出。必须核实版本 3027、dirty=false。迁移失败时先判断事务及 dirty 状态，不能强制跳版本或删库。
3. 使用基础 Compose 加 `candidate-image-pin.yml`，**排除旧 pin**；固定候选 app/frontend ID，`up -d --no-deps --no-build --pull never --force-recreate app frontend`。不启动 sid-mock、不重建其他服务。
4. 同时核对运行容器实际 image ID、完整 OCI revision、app system/info 和前端制品版本；不能只检查标签。核对 oauth_form 与全部身份门禁，验证 80/8080、代理、本地账号及核心 GET 接口，比较依赖容器 ID/挂载。
5. 如失败，保持企业入口不放行，先撤销 `source_id_local_sessions` 对应的派生 Token 和 source_id_sessions；采用明确事务并核对撤销结果。使用 `rollback-oauth-form-image-pin.yml` 重建仅 app/frontend，恢复旧镜像且 **不恢复 raw**。
6. 通常保留新增的 3027 表，旧 app 在克隆库上已验证兼容。但旧版不会实施新本地会话限制，因此回滚前的撤销不可省略。若未来确需 down，先归档受保护审计/元数据，再使用 003027 down；该脚本也先撤销，必须核实 dirty=false。无需恢复整个数据库或删除数据卷。

撤销事务的作用范围只包括本地企业会话：

```sql
BEGIN;
UPDATE auth_tokens SET is_revoked = TRUE
 WHERE session_id IN (SELECT id FROM source_id_local_sessions);
UPDATE source_id_sessions SET revoked = TRUE
 WHERE id IN (SELECT id FROM source_id_local_sessions);
COMMIT;
```

目前原库尚无 003027，也未创建这些企业会话；本轮上述事务仅在克隆库执行。绑定撤销/会话管理的受控工具与责任人还需在真实放行审批中落实。

## 8. 结论及尚未获准事项

**可以申请打包机代码升级**：候选镜像已构建、CI 通过、备份可恢复、3027 升降迁及新旧应用兼容已实测、旧 pin 风险已有明确替换方案。没有发现必须先开发其他依赖版本的阻塞。

**尚未执行**：默认版本升级、原库 003027 迁移、真实员工登录、真实自动绑定及 local_approved 启用。此项由用户本轮执行边界要求暂缓，需单独批准。

**真实业务放行仍受阻**：SID 停用传播、已有 SSO 时再次准入检查及未消费 code 的边界；本地会话时长/Refresh/风险窗口的正式安全批准；自动绑定策略批准和普通测试账号/权限基线准备。RJGH/RJEMAIL 的既有用户确认不再重复列作待确认项。SID 统一登出仍属第二期。

默认现场最终仍为 **2fcbcaaed / DB 3026 / oauth_form**。部署预检通过不等于阶段 B 业务放行或新的真实 SID Token 200 证据。
